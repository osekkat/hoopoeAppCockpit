package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/auth"
)

// fakePairing is a minimal PairingConsumer used by the auth route tests.
// It returns a stubbed PairingRecord on the first ConsumePairing call and
// the configured error thereafter (so tests can pin specific failure
// branches without booting the full BootstrapCredentialService).
type fakePairing struct {
	consumeErr error
	record     auth.PairingRecord
	calls      int
}

func (f *fakePairing) ConsumePairing(_ context.Context, req auth.ConsumePairingRequest) (auth.PairingRecord, error) {
	f.calls++
	if f.consumeErr != nil {
		return auth.PairingRecord{}, f.consumeErr
	}
	r := f.record
	if r.TokenID == "" {
		r = auth.PairingRecord{
			TokenID:      "pair_test",
			DisplayToken: req.PairingToken,
			Role:         auth.PairingRoleOwner,
			CreatedAt:    time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		}
	}
	return r, nil
}

// newAuthRouterTestRig wires a real SessionCredentialService + fakePairing
// behind NewRouter. Returns the rig handle for assertions.
type authRig struct {
	router  http.Handler
	svc     *auth.SessionCredentialService
	pairing *fakePairing
	clock   func() time.Time
}

func newAuthRouter(t *testing.T) *authRig {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) }
	store, err := auth.NewServerSecretStore(auth.ServerSecretStoreConfig{
		Path: filepath.Join(t.TempDir(), "secret.json"),
		Now:  now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureInitialized(); err != nil {
		t.Fatal(err)
	}
	svc, err := auth.NewSessionCredentialService(auth.SessionCredentialConfig{
		Secrets: store,
		Now:     now,
	})
	if err != nil {
		t.Fatal(err)
	}
	pairing := &fakePairing{
		record: auth.PairingRecord{
			TokenID:      "pair_test",
			DisplayToken: "H0123456789AB",
			Role:         auth.PairingRoleOwner,
			CreatedAt:    now(),
		},
	}
	return &authRig{
		router: NewRouter(Config{
			Build: BuildInfo{Version: "0.1.0", Commit: "test", BuildDate: "test", APIVersion: "v1"},
			Auth: &AuthConfig{Service: svc, Pairing: pairing},
			Now:  now,
		}),
		svc:     svc,
		pairing: pairing,
		clock:   now,
	}
}

func postJSON(t *testing.T, router http.Handler, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func decodeBearer(t *testing.T, rr *httptest.ResponseRecorder) auth.IssuedBearer {
	t.Helper()
	var bearer auth.IssuedBearer
	if err := json.Unmarshal(rr.Body.Bytes(), &bearer); err != nil {
		t.Fatalf("decode bearer: %v body=%s", err, rr.Body.String())
	}
	return bearer
}

func TestBootstrapBearerHappyPath(t *testing.T) {
	rig := newAuthRouter(t)
	rr := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "H0123456789AB",
		"instanceId":   "desktop-1",
	}, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	bearer := decodeBearer(t, rr)
	if bearer.Token == "" {
		t.Error("empty token")
	}
	if bearer.SID == "" {
		t.Error("empty sid")
	}
	if bearer.Role != auth.PairingRoleOwner {
		t.Errorf("role=%s", bearer.Role)
	}
}

func TestBootstrapBearerRejectsMissingFields(t *testing.T) {
	rig := newAuthRouter(t)
	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing pairingToken", map[string]any{"instanceId": "desktop-1"}},
		{"missing instanceId", map[string]any{"pairingToken": "H0123456789AB"}},
		{"empty body", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", tc.body, nil)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("status=%d", rr.Code)
			}
		})
	}
}

func TestBootstrapBearerInvalidJSON(t *testing.T) {
	rig := newAuthRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/bootstrap/bearer", strings.NewReader("{not json"))
	rr := httptest.NewRecorder()
	rig.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d", rr.Code)
	}
}

func TestBootstrapBearerPairingNotFound(t *testing.T) {
	rig := newAuthRouter(t)
	rig.pairing.consumeErr = auth.ErrPairingNotFound
	rr := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "Hxxxxxxxxxxx",
		"instanceId":   "desktop-1",
	}, nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestBootstrapBearerPairingConsumed(t *testing.T) {
	rig := newAuthRouter(t)
	rig.pairing.consumeErr = auth.ErrPairingConsumed
	rr := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "Hxxxxxxxxxxx",
		"instanceId":   "desktop-1",
	}, nil)
	if rr.Code != http.StatusGone {
		t.Errorf("status=%d", rr.Code)
	}
}

func TestWSTokenHappyPath(t *testing.T) {
	rig := newAuthRouter(t)
	bootRR := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "H0123456789AB",
		"instanceId":   "desktop-1",
	}, nil)
	if bootRR.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d", bootRR.Code)
	}
	bearer := decodeBearer(t, bootRR)

	rr := postJSON(t, rig.router, "/v1/auth/ws-token", nil, map[string]string{
		"Authorization": "Bearer " + bearer.Token,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("ws-token status=%d body=%s", rr.Code, rr.Body.String())
	}
	var ws auth.IssuedWSToken
	if err := json.Unmarshal(rr.Body.Bytes(), &ws); err != nil {
		t.Fatal(err)
	}
	if ws.SID != bearer.SID {
		t.Errorf("ws sid=%s, bearer sid=%s", ws.SID, bearer.SID)
	}
	if ws.ExpiresAt.Sub(ws.IssuedAt) != auth.WSTokenTTL {
		t.Errorf("ws ttl=%v", ws.ExpiresAt.Sub(ws.IssuedAt))
	}
}

func TestWSTokenMissingBearer(t *testing.T) {
	rig := newAuthRouter(t)
	rr := postJSON(t, rig.router, "/v1/auth/ws-token", nil, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status=%d", rr.Code)
	}
}

func TestWSTokenRejectsForgedBearer(t *testing.T) {
	rig := newAuthRouter(t)
	rr := postJSON(t, rig.router, "/v1/auth/ws-token", nil, map[string]string{
		"Authorization": "Bearer not-even-base64.fake",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status=%d", rr.Code)
	}
}

func TestSessionRevokeHappyPath(t *testing.T) {
	rig := newAuthRouter(t)
	bootRR := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "H0123456789AB",
		"instanceId":   "desktop-1",
	}, nil)
	bearer := decodeBearer(t, bootRR)

	rr := postJSON(t, rig.router, "/v1/auth/session/revoke", map[string]any{
		"sid": bearer.SID,
	}, map[string]string{
		"Authorization": "Bearer " + bearer.Token,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["sid"] != bearer.SID {
		t.Errorf("sid=%v", resp["sid"])
	}
	if resp["wasActive"] != true {
		t.Errorf("wasActive=%v", resp["wasActive"])
	}
	// Subsequent verify should now reject the bearer.
	if _, err := rig.svc.VerifyBearer(bearer.Token); !errors.Is(err, auth.ErrSessionRevoked) {
		t.Errorf("post-revoke verify: %v", err)
	}
}

func TestSessionRevokeRequiresOwnerRole(t *testing.T) {
	rig := newAuthRouter(t)
	rig.pairing.record.Role = auth.PairingRoleClient // pairing yields client bearer
	bootRR := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "H0123456789AB",
		"instanceId":   "desktop-1",
	}, nil)
	if bootRR.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d", bootRR.Code)
	}
	bearer := decodeBearer(t, bootRR)

	rr := postJSON(t, rig.router, "/v1/auth/session/revoke", map[string]any{
		"sid": bearer.SID,
	}, map[string]string{
		"Authorization": "Bearer " + bearer.Token,
	})
	if rr.Code != http.StatusForbidden {
		t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSessionRevokeMissingBearer(t *testing.T) {
	rig := newAuthRouter(t)
	rr := postJSON(t, rig.router, "/v1/auth/session/revoke", map[string]any{
		"sid": "sid_test",
	}, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status=%d", rr.Code)
	}
}

func TestSessionRevokeMissingSID(t *testing.T) {
	rig := newAuthRouter(t)
	bootRR := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "H0123456789AB",
		"instanceId":   "desktop-1",
	}, nil)
	bearer := decodeBearer(t, bootRR)
	rr := postJSON(t, rig.router, "/v1/auth/session/revoke", map[string]any{}, map[string]string{
		"Authorization": "Bearer " + bearer.Token,
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d", rr.Code)
	}
}

func TestAuthRoutesNotMountedWithoutConfig(t *testing.T) {
	// Without AuthConfig, the seed-contract `handlePlannedWrite` stubs
	// answer with 501 — not 401/200. Pin this so a future bootstrap
	// regression that drops the AuthConfig surfaces clearly.
	router := NewRouter(Config{
		Build: BuildInfo{Version: "0.1.0", Commit: "test"},
		Now:   func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) },
		// Auth: nil — intentional.
	})
	rr := postJSON(t, router, "/v1/auth/bootstrap/bearer", map[string]any{}, nil)
	if rr.Code != http.StatusNotImplemented {
		t.Errorf("expected 501 from stub when AuthConfig is nil, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// extractBearerHeader edge cases.
func TestExtractBearerHeader(t *testing.T) {
	cases := []struct {
		name, header, want string
	}{
		{"standard bearer", "Bearer abc123", "abc123"},
		{"lowercase bearer", "bearer abc123", "abc123"},
		{"uppercase bearer", "BEARER abc123", "abc123"},
		{"missing prefix", "abc123", ""},
		{"empty", "", ""},
		{"too short", "Bear", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tc.header)
			got := extractBearerHeader(req)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
