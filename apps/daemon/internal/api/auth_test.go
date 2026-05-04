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

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/approvals"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/auth"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// fakePairing is a minimal PairingConsumer used by the auth route tests.
// It returns a stubbed PairingRecord on the first ConsumePairing call and
// the configured error thereafter (so tests can pin specific failure
// branches without booting the full BootstrapCredentialService).
type fakePairing struct {
	consumeErr error
	createErr  error
	listErr    error
	revokeErr  error
	record     auth.PairingRecord
	calls      int
	pairings   []auth.PairingRecord
	created    []auth.IssuedPairing
	revoked    []auth.RevokePairingRequest
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

func (f *fakePairing) CreatePairing(_ context.Context, req auth.CreatePairingRequest) (auth.IssuedPairing, error) {
	if f.createErr != nil {
		return auth.IssuedPairing{}, f.createErr
	}
	role := req.Role
	if role == "" {
		role = auth.PairingRoleClient
	}
	issued := auth.IssuedPairing{
		TokenID:      "pair_rotation_owner",
		DisplayToken: "ABCDEFGHJKM1",
		Role:         role,
		CreatedAt:    time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
	}
	f.created = append(f.created, issued)
	return issued, nil
}

func (f *fakePairing) ListPairings(_ context.Context) ([]auth.PairingRecord, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]auth.PairingRecord, len(f.pairings))
	copy(out, f.pairings)
	return out, nil
}

func (f *fakePairing) RevokePairing(_ context.Context, req auth.RevokePairingRequest) (auth.PairingRecord, error) {
	if f.revokeErr != nil {
		return auth.PairingRecord{}, f.revokeErr
	}
	f.revoked = append(f.revoked, req)
	for i, record := range f.pairings {
		if record.TokenID != req.TokenID {
			continue
		}
		now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
		record.RevokedAt = &now
		record.RevokedBy = req.Actor
		f.pairings[i] = record
		return record, nil
	}
	return auth.PairingRecord{}, auth.ErrPairingNotFound
}

// newAuthRouterTestRig wires a real SessionCredentialService + fakePairing
// behind NewRouter. Returns the rig handle for assertions.
type authRig struct {
	router    http.Handler
	svc       *auth.SessionCredentialService
	pairing   *fakePairing
	approvals *approvals.Queue
	events    *EventHub
	clock     func() time.Time
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
		pairings: []auth.PairingRecord{
			{
				TokenID:      "pair_open",
				DisplayToken: "ABCDEFGHJKM1",
				Role:         auth.PairingRoleOwner,
				CreatedAt:    now(),
			},
		},
	}
	approvalQueue := approvals.NewQueue(approvals.Config{Now: now})
	events := NewEventHub(EventHubConfig{Now: now})
	return &authRig{
		router: NewRouter(Config{
			Build:  BuildInfo{Version: "0.1.0", Commit: "test", BuildDate: "test", APIVersion: "v1"},
			Events: events,
			Auth: &AuthConfig{
				Service:   svc,
				Pairing:   pairing,
				Approvals: ApprovalQueueLookup{Queue: approvalQueue},
			},
			Now: now,
		}),
		svc:       svc,
		pairing:   pairing,
		approvals: approvalQueue,
		events:    events,
		clock:     now,
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

func TestBootstrapBearerRejectsOversizedBody(t *testing.T) {
	// hp-c5rb: /v1/auth/bootstrap/bearer is reachable pre-bearer, so an
	// attacker without credentials must not be able to drive the daemon out
	// of memory by streaming a huge body. The handler caps the decoded
	// request at authRequestBodyLimit (1 MB).
	rig := newAuthRouter(t)
	oversized := bytes.Repeat([]byte("A"), authRequestBodyLimit+1024)
	body := append([]byte(`{"pairingToken":"H0123456789AB","instanceId":"`), oversized...)
	body = append(body, []byte(`"}`)...)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/bootstrap/bearer", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	rig.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rr.Code, rr.Body.String())
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

func TestSessionRevokeRejectsOversizedBody(t *testing.T) {
	// hp-c5rb: even with a valid bearer, an oversized body must be capped
	// before the JSON decoder buffers GBs into memory.
	rig := newAuthRouter(t)
	bootRR := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "H0123456789AB",
		"instanceId":   "desktop-1",
	}, nil)
	bearer := decodeBearer(t, bootRR)

	oversized := bytes.Repeat([]byte("S"), authRequestBodyLimit+1024)
	body := append([]byte(`{"sid":"`), oversized...)
	body = append(body, []byte(`"}`)...)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/session/revoke", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+bearer.Token)
	rr := httptest.NewRecorder()
	rig.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rr.Code, rr.Body.String())
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

func TestRotateSecretHappyPathInvalidatesBearerAndReturnsReplacementPairing(t *testing.T) {
	rig := newAuthRouter(t)
	bootRR := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "H0123456789AB",
		"instanceId":   "desktop-1",
	}, nil)
	if bootRR.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", bootRR.Code, bootRR.Body.String())
	}
	bearer := decodeBearer(t, bootRR)
	ws, err := rig.svc.IssueWSToken(bearer.Token)
	if err != nil {
		t.Fatalf("issue ws before rotation: %v", err)
	}
	approvalID := approveRotateSecret(t, rig)

	rr := postJSON(t, rig.router, "/v1/auth/rotate-secret", nil, map[string]string{
		"Authorization":                "Bearer " + bearer.Token,
		authRotateSecretConfirmHeader:  "yes",
		authRotateSecretApprovalHeader: approvalID,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		FlowID          string `json:"flowId"`
		NewPairingToken struct {
			Value   string           `json:"value"`
			TokenID string           `json:"tokenId"`
			Role    auth.PairingRole `json:"role"`
		} `json:"newPairingToken"`
		Revoked struct {
			Bearers       int `json:"bearers"`
			PairingGrants int `json:"pairingGrants"`
		} `json:"revoked"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	if !strings.HasPrefix(resp.FlowID, "rot_") {
		t.Fatalf("flowId=%q", resp.FlowID)
	}
	if resp.NewPairingToken.Value != "ABCDEFGHJKM1" || resp.NewPairingToken.Role != auth.PairingRoleOwner {
		t.Fatalf("new pairing = %+v", resp.NewPairingToken)
	}
	if resp.Revoked.Bearers != 1 || resp.Revoked.PairingGrants != 1 {
		t.Fatalf("revoked = %+v", resp.Revoked)
	}
	consumed, ok, err := rig.approvals.Get(context.Background(), approvalID)
	if err != nil || !ok {
		t.Fatalf("approval lookup after rotation: approval=%+v ok=%v err=%v", consumed, ok, err)
	}
	if consumed.State != schemas.Revoked {
		t.Fatalf("approval state = %s, want revoked", consumed.State)
	}
	if len(rig.pairing.revoked) != 1 || rig.pairing.revoked[0].TokenID != "pair_open" {
		t.Fatalf("pairing revocations = %+v", rig.pairing.revoked)
	}
	staleRR := postJSON(t, rig.router, "/v1/auth/ws-token", nil, map[string]string{
		"Authorization": "Bearer " + bearer.Token,
	})
	if staleRR.Code != http.StatusUnauthorized {
		t.Fatalf("stale bearer status=%d body=%s", staleRR.Code, staleRR.Body.String())
	}
	if staleRR.Header().Get("X-Hoopoe-Revocation-Cause") != authRotateSecretRevocationCause {
		t.Fatalf("revocation header=%q", staleRR.Header().Get("X-Hoopoe-Revocation-Cause"))
	}
	if _, err := rig.svc.VerifyBearer(bearer.Token); !errors.Is(err, auth.ErrTokenSignatureMismatch) {
		t.Fatalf("old bearer verify err=%v", err)
	}
	if _, err := rig.svc.VerifyWSToken(ws.Token); !errors.Is(err, auth.ErrTokenSignatureMismatch) {
		t.Fatalf("old ws verify err=%v", err)
	}

	events, gap := rig.events.Replay("_system", 0)
	if gap {
		t.Fatal("unexpected replay gap")
	}
	if len(events) != 2 || events[0].Type != authRotateSecretImminentEvent || events[1].Type != authRotateSecretCompletedEvent {
		t.Fatalf("events = %+v", events)
	}
	rawEvents, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawEvents), resp.NewPairingToken.Value) {
		t.Fatalf("event stream leaked replacement pairing token: %s", rawEvents)
	}
}

func TestRotateSecretRejectsReusedOnceApproval(t *testing.T) {
	rig := newAuthRouter(t)
	bootRR := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "H0123456789AB",
		"instanceId":   "desktop-1",
	}, nil)
	if bootRR.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", bootRR.Code, bootRR.Body.String())
	}
	bearer := decodeBearer(t, bootRR)
	approvalID := approveRotateSecret(t, rig)

	first := postJSON(t, rig.router, "/v1/auth/rotate-secret", nil, map[string]string{
		"Authorization":                "Bearer " + bearer.Token,
		authRotateSecretConfirmHeader:  "yes",
		authRotateSecretApprovalHeader: approvalID,
	})
	if first.Code != http.StatusOK {
		t.Fatalf("first rotate status=%d body=%s", first.Code, first.Body.String())
	}
	var firstBody struct {
		NewPairingToken struct {
			Value string `json:"value"`
		} `json:"newPairingToken"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatalf("decode first rotate: %v body=%s", err, first.Body.String())
	}

	repairedBoot := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": firstBody.NewPairingToken.Value,
		"instanceId":   "desktop-2",
	}, nil)
	if repairedBoot.Code != http.StatusOK {
		t.Fatalf("replacement bootstrap status=%d body=%s", repairedBoot.Code, repairedBoot.Body.String())
	}
	repairedBearer := decodeBearer(t, repairedBoot)
	reuse := postJSON(t, rig.router, "/v1/auth/rotate-secret", nil, map[string]string{
		"Authorization":                "Bearer " + repairedBearer.Token,
		authRotateSecretConfirmHeader:  "yes",
		authRotateSecretApprovalHeader: approvalID,
	})
	if reuse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("reuse status=%d body=%s", reuse.Code, reuse.Body.String())
	}
	if !strings.Contains(reuse.Body.String(), "auth.rotate_secret_approval_consumed") {
		t.Fatalf("reuse body=%s, want consumed problem code", reuse.Body.String())
	}
	events, gap := rig.events.Replay("_system", 0)
	if gap {
		t.Fatal("unexpected replay gap")
	}
	if len(events) != 2 {
		t.Fatalf("events after rejected reuse = %+v, want original rotation events only", events)
	}
}

func TestRotateSecretDoesNotInvalidateBearerWhenPairingRevokeFails(t *testing.T) {
	rig := newAuthRouter(t)
	bootRR := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "H0123456789AB",
		"instanceId":   "desktop-1",
	}, nil)
	if bootRR.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", bootRR.Code, bootRR.Body.String())
	}
	bearer := decodeBearer(t, bootRR)
	approvalID := approveRotateSecret(t, rig)
	rig.pairing.revokeErr = errors.New("pairing store unavailable")

	rr := postJSON(t, rig.router, "/v1/auth/rotate-secret", nil, map[string]string{
		"Authorization":                "Bearer " + bearer.Token,
		authRotateSecretConfirmHeader:  "yes",
		authRotateSecretApprovalHeader: approvalID,
	})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("rotate status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := rig.svc.VerifyBearer(bearer.Token); err != nil {
		t.Fatalf("bearer was invalidated before revoke succeeded: %v", err)
	}
	events, gap := rig.events.Replay("_system", 0)
	if gap {
		t.Fatal("unexpected replay gap")
	}
	if len(events) != 0 {
		t.Fatalf("rotation events published before revoke succeeded: %+v", events)
	}
}

func TestRotateSecretDoesNotInvalidateBearerWhenReplacementPairingFails(t *testing.T) {
	rig := newAuthRouter(t)
	bootRR := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "H0123456789AB",
		"instanceId":   "desktop-1",
	}, nil)
	if bootRR.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", bootRR.Code, bootRR.Body.String())
	}
	bearer := decodeBearer(t, bootRR)
	approvalID := approveRotateSecret(t, rig)
	rig.pairing.createErr = errors.New("entropy unavailable")

	rr := postJSON(t, rig.router, "/v1/auth/rotate-secret", nil, map[string]string{
		"Authorization":                "Bearer " + bearer.Token,
		authRotateSecretConfirmHeader:  "yes",
		authRotateSecretApprovalHeader: approvalID,
	})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("rotate status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := rig.svc.VerifyBearer(bearer.Token); err != nil {
		t.Fatalf("bearer was invalidated before replacement pairing existed: %v", err)
	}
	events, gap := rig.events.Replay("_system", 0)
	if gap {
		t.Fatal("unexpected replay gap")
	}
	if len(events) != 0 {
		t.Fatalf("rotation events published before replacement pairing existed: %+v", events)
	}
}

func TestRotateSecretRejectsMissingApprovalAndConfirmation(t *testing.T) {
	rig := newAuthRouter(t)
	bootRR := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "H0123456789AB",
		"instanceId":   "desktop-1",
	}, nil)
	bearer := decodeBearer(t, bootRR)

	missingConfirm := postJSON(t, rig.router, "/v1/auth/rotate-secret", nil, map[string]string{
		"Authorization":                "Bearer " + bearer.Token,
		authRotateSecretApprovalHeader: approveRotateSecret(t, rig),
	})
	if missingConfirm.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing confirm status=%d body=%s", missingConfirm.Code, missingConfirm.Body.String())
	}

	missingApproval := postJSON(t, rig.router, "/v1/auth/rotate-secret", nil, map[string]string{
		"Authorization":               "Bearer " + bearer.Token,
		authRotateSecretConfirmHeader: "yes",
	})
	if missingApproval.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing approval status=%d body=%s", missingApproval.Code, missingApproval.Body.String())
	}
}

func TestRotateSecretRequiresOwnerRole(t *testing.T) {
	rig := newAuthRouter(t)
	rig.pairing.record.Role = auth.PairingRoleClient
	bootRR := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "H0123456789AB",
		"instanceId":   "desktop-1",
	}, nil)
	bearer := decodeBearer(t, bootRR)

	rr := postJSON(t, rig.router, "/v1/auth/rotate-secret", nil, map[string]string{
		"Authorization":                "Bearer " + bearer.Token,
		authRotateSecretConfirmHeader:  "yes",
		authRotateSecretApprovalHeader: approveRotateSecret(t, rig),
	})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRotateSecretRejectsWrongApprovalShape(t *testing.T) {
	rig := newAuthRouter(t)
	bootRR := postJSON(t, rig.router, "/v1/auth/bootstrap/bearer", map[string]any{
		"pairingToken": "H0123456789AB",
		"instanceId":   "desktop-1",
	}, nil)
	bearer := decodeBearer(t, bootRR)
	approval, _, err := rig.approvals.Request(context.Background(), approvals.Request{
		RequestedAction: schemas.CommandSpec{Kind: "git.push_branch", Target: map[string]interface{}{}},
		RequestActor:    schemas.Actor{Kind: schemas.ActorKindUser},
		RiskClass:       schemas.Critical,
		Scope:           schemas.Once,
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := rig.approvals.Approve(context.Background(), approval.Id, schemas.ApprovalDecisionRequest{
		DecisionActor: schemas.Actor{Kind: schemas.ActorKindUser},
	})
	if err != nil {
		t.Fatal(err)
	}

	rr := postJSON(t, rig.router, "/v1/auth/rotate-secret", nil, map[string]string{
		"Authorization":                "Bearer " + bearer.Token,
		authRotateSecretConfirmHeader:  "yes",
		authRotateSecretApprovalHeader: approved.Id,
	})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
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

func approveRotateSecret(t *testing.T, rig *authRig) string {
	t.Helper()
	approval, _, err := rig.approvals.Request(context.Background(), approvals.Request{
		RequestedAction: schemas.CommandSpec{
			Kind:   authRotateSecretActionKind,
			Target: map[string]interface{}{"daemon": "local"},
		},
		RequestActor: schemas.Actor{Kind: schemas.ActorKindUser},
		RiskClass:    schemas.Critical,
		Scope:        schemas.Once,
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := rig.approvals.Approve(context.Background(), approval.Id, schemas.ApprovalDecisionRequest{
		DecisionActor: schemas.Actor{Kind: schemas.ActorKindUser},
	})
	if err != nil {
		t.Fatal(err)
	}
	return approved.Id
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
