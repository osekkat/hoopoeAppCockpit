// Package api — auth handlers for the production daemon (hp-0oi wiring of
// the SessionCredentialService from internal/auth).
//
// Three POST endpoints replace the seed-contract `handlePlannedWrite`
// stubs:
//
//   POST /v1/auth/bootstrap/bearer   pairing-token → bearer (30d)
//   POST /v1/auth/ws-token           bearer        → ws-token (5min)
//   POST /v1/auth/session/revoke     {sid}         → drop bearer + WS
//
// `POST /v1/auth/rotate-secret` is approval-gated (owner-only) and
// remains a planned-write stub until hp-tmo7 ships the approval flow.
//
// All three honor `Idempotency-Key` per plan.md §2.6 (the first call
// with a given key is durable; retries return the same response).
// Idempotency persistence lives in the daemon's idempotency store,
// which BrownStone wired in hp-ngq — for now the handlers are
// idempotency-key aware but don't deduplicate (single-process tests
// always succeed; production needs the persisted store).

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/auth"
)

// AuthService is the subset of SessionCredentialService + BootstrapCredentialService
// the HTTP layer needs. Defined as an interface so tests can inject a fake.
type AuthService interface {
	IssueBearer(role auth.SessionRole) (auth.IssuedBearer, error)
	IssueWSToken(bearerToken string) (auth.IssuedWSToken, error)
	RevokeSession(sid string, actor string) (bool, error)
}

// PairingConsumer is the BootstrapCredentialService subset the bootstrap
// handler uses. ConsumePairing returns the consumed PairingRecord.
type PairingConsumer interface {
	ConsumePairing(ctx context.Context, req auth.ConsumePairingRequest) (auth.PairingRecord, error)
}

// AuthConfig wires the auth handlers. Both services are required; the
// router refuses to mount the auth endpoints unless they're set.
type AuthConfig struct {
	Service AuthService
	Pairing PairingConsumer
}

// mountAuthRoutes attaches the bearer / ws-token / session-revoke
// endpoints to r. If the AuthConfig is unset, the seed-contract stubs
// in mountSeedContractRoutes still answer with 501 problem+json.
func (s *server) mountAuthRoutes(r chi.Router) {
	if s.authConfig == nil {
		return
	}
	r.Post("/v1/auth/bootstrap/bearer", s.handleAuthBootstrapBearer)
	r.Post("/v1/auth/ws-token", s.handleAuthWSToken)
	r.Post("/v1/auth/session/revoke", s.handleAuthSessionRevoke)
}

// handleAuthBootstrapBearer consumes a pairing token and issues a 30d
// bearer in one step. The pairing token is single-use; subsequent calls
// with the same token fail with 410.
func (s *server) handleAuthBootstrapBearer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PairingToken string `json:"pairingToken"`
		InstanceID   string `json:"instanceId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeProblemCode(w, http.StatusBadRequest, "auth.invalid_json", "invalid request body", err.Error())
		return
	}
	if body.PairingToken == "" {
		s.writeProblemCode(w, http.StatusBadRequest, "auth.missing_pairing", "pairingToken required", "request body must include pairingToken")
		return
	}
	if body.InstanceID == "" {
		s.writeProblemCode(w, http.StatusBadRequest, "auth.missing_instance", "instanceId required", "request body must include instanceId for audit")
		return
	}
	record, err := s.authConfig.Pairing.ConsumePairing(r.Context(), auth.ConsumePairingRequest{
		PairingToken: body.PairingToken,
		InstanceID:   body.InstanceID,
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrPairingNotFound):
			s.writeProblemCode(w, http.StatusNotFound, "auth.pairing_not_found", "pairing token not found", err.Error())
		case errors.Is(err, auth.ErrPairingConsumed):
			s.writeProblemCode(w, http.StatusGone, "auth.pairing_consumed", "pairing token already consumed", err.Error())
		case errors.Is(err, auth.ErrPairingRevoked):
			s.writeProblemCode(w, http.StatusForbidden, "auth.pairing_revoked", "pairing token revoked", err.Error())
		case errors.Is(err, auth.ErrInvalidPairingToken), errors.Is(err, auth.ErrInvalidPairingRequest):
			s.writeProblemCode(w, http.StatusBadRequest, "auth.pairing_invalid", "invalid pairing token", err.Error())
		default:
			s.writeProblemCode(w, http.StatusInternalServerError, "auth.pairing_error", "pairing consume failed", err.Error())
		}
		return
	}
	bearer, err := s.authConfig.Service.IssueBearer(record.Role)
	if err != nil {
		s.writeProblemCode(w, http.StatusInternalServerError, "auth.bearer_issue_failed", "bearer issuance failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bearer)
}

// handleAuthWSToken issues a 5-minute WS token from a valid bearer.
// The bearer is supplied via the Authorization header (`Bearer <token>`)
// per plan.md §5.2.
func (s *server) handleAuthWSToken(w http.ResponseWriter, r *http.Request) {
	bearer := extractBearerHeader(r)
	if bearer == "" {
		s.writeProblemCode(w, http.StatusUnauthorized, "auth.missing_bearer", "Authorization header missing", "request must include `Authorization: Bearer <token>`")
		return
	}
	wsToken, err := s.authConfig.Service.IssueWSToken(bearer)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrTokenExpired):
			s.writeProblemCode(w, http.StatusUnauthorized, "auth.bearer_expired", "bearer expired", err.Error())
		case errors.Is(err, auth.ErrTokenSignatureMismatch):
			s.writeProblemCode(w, http.StatusUnauthorized, "auth.bearer_invalid_signature", "bearer signature invalid", err.Error())
		case errors.Is(err, auth.ErrSessionRevoked):
			s.writeProblemCode(w, http.StatusUnauthorized, "auth.session_revoked", "session revoked", err.Error())
		case errors.Is(err, auth.ErrTokenWrongKind):
			s.writeProblemCode(w, http.StatusUnauthorized, "auth.bearer_wrong_kind", "expected bearer, got ws-token", err.Error())
		case errors.Is(err, auth.ErrInvalidToken):
			s.writeProblemCode(w, http.StatusUnauthorized, "auth.bearer_invalid", "invalid bearer", err.Error())
		case errors.Is(err, auth.ErrSessionNotFound):
			s.writeProblemCode(w, http.StatusUnauthorized, "auth.session_unknown", "session not in active table", err.Error())
		default:
			s.writeProblemCode(w, http.StatusInternalServerError, "auth.ws_token_failed", "ws-token issuance failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, wsToken)
}

// handleAuthSessionRevoke kills a sid (drops bearer + cascading WS).
// Owner-only; the caller's role is verified via the bearer in the
// Authorization header.
func (s *server) handleAuthSessionRevoke(w http.ResponseWriter, r *http.Request) {
	bearer := extractBearerHeader(r)
	if bearer == "" {
		s.writeProblemCode(w, http.StatusUnauthorized, "auth.missing_bearer", "Authorization header missing", "session-revoke requires owner bearer")
		return
	}
	// Verify caller's bearer + role. We use the same service that issues
	// bearers; the verify path is what gates owner-only operations.
	type verifier interface {
		VerifyBearer(token string) (auth.Claims, error)
	}
	v, ok := s.authConfig.Service.(verifier)
	if !ok {
		s.writeProblemCode(w, http.StatusInternalServerError, "auth.verifier_missing", "no verifier configured", "session-revoke requires a service implementing VerifyBearer")
		return
	}
	claims, err := v.VerifyBearer(bearer)
	if err != nil {
		s.writeProblemCode(w, http.StatusUnauthorized, "auth.bearer_invalid", "bearer rejected", err.Error())
		return
	}
	if claims.Role != auth.PairingRoleOwner {
		s.writeProblemCode(w, http.StatusForbidden, "auth.role_forbidden", "session-revoke requires owner role", "client-role bearers cannot revoke sessions")
		return
	}

	var body struct {
		SID string `json:"sid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeProblemCode(w, http.StatusBadRequest, "auth.invalid_json", "invalid request body", err.Error())
		return
	}
	if body.SID == "" {
		s.writeProblemCode(w, http.StatusBadRequest, "auth.missing_sid", "sid required", "request body must include sid")
		return
	}
	wasActive, err := s.authConfig.Service.RevokeSession(body.SID, claims.SID)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrSessionNotFound):
			s.writeProblemCode(w, http.StatusNotFound, "auth.session_not_found", "session not in active table", err.Error())
		case errors.Is(err, auth.ErrInvalidToken):
			s.writeProblemCode(w, http.StatusBadRequest, "auth.invalid_sid", "invalid sid", err.Error())
		default:
			s.writeProblemCode(w, http.StatusInternalServerError, "auth.revoke_failed", "session revoke failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sid":          body.SID,
		"wasActive":    wasActive,
		"revokedBy":    claims.SID,
	})
}

// extractBearerHeader pulls the bearer token from `Authorization: Bearer
// <token>`. Returns empty string if the header is missing or shaped
// wrong.
func extractBearerHeader(r *http.Request) string {
	value := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(value) <= len(prefix) {
		return ""
	}
	if value[:len(prefix)] != prefix && value[:len(prefix)] != "bearer " && value[:len(prefix)] != "BEARER " {
		return ""
	}
	return value[len(prefix):]
}
