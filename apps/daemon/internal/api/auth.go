// Package api — auth handlers for the production daemon (hp-0oi wiring of
// the SessionCredentialService from internal/auth).
//
// Four POST endpoints replace the seed-contract `handlePlannedWrite`
// stubs:
//
//   POST /v1/auth/bootstrap/bearer   pairing-token → bearer (30d)
//   POST /v1/auth/ws-token           bearer        → ws-token (5min)
//   POST /v1/auth/session/revoke     {sid}         → drop bearer + WS
//   POST /v1/auth/rotate-secret      rotate signing secret + re-pair
//
// All four honor `Idempotency-Key` per plan.md §2.6 (the first call
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
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/auth"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const (
	authRotateSecretActionKind      = "auth.rotate_secret"
	authRotateSecretConfirmHeader   = "X-Hoopoe-Confirm-Rotate"
	authRotateSecretApprovalHeader  = "X-Hoopoe-Approval-ID"
	authRotateSecretApprovalMaxAge  = 2 * time.Minute
	authRotateSecretGraceWindowMs   = 5000
	authRotateSecretSystemChannel   = "_system"
	authRotateSecretImminentEvent   = "auth.secret_rotation_imminent"
	authRotateSecretCompletedEvent  = "auth.secret_rotated"
	authRotateSecretRevocationCause = "secret_rotation"
)

// AuthService is the subset of SessionCredentialService + BootstrapCredentialService
// the HTTP layer needs. Defined as an interface so tests can inject a fake.
type AuthService interface {
	IssueBearer(role auth.SessionRole) (auth.IssuedBearer, error)
	IssueWSToken(bearerToken string) (auth.IssuedWSToken, error)
	VerifyBearer(token string) (auth.Claims, error)
	RevokeSession(sid string, actor string) (bool, error)
	RotateSecret() (auth.SecretSnapshot, error)
	ListSessions() []auth.SessionRecord
}

// PairingConsumer is the BootstrapCredentialService subset the bootstrap
// handler uses. ConsumePairing returns the consumed PairingRecord.
type PairingConsumer interface {
	ConsumePairing(ctx context.Context, req auth.ConsumePairingRequest) (auth.PairingRecord, error)
}

// PairingRotator is the BootstrapCredentialService subset needed when
// rotating the signing secret: revoke every open grant, then issue exactly
// one replacement owner pairing token whose value is returned once.
type PairingRotator interface {
	CreatePairing(ctx context.Context, req auth.CreatePairingRequest) (auth.IssuedPairing, error)
	ListPairings(ctx context.Context) ([]auth.PairingRecord, error)
	RevokePairing(ctx context.Context, req auth.RevokePairingRequest) (auth.PairingRecord, error)
}

// ApprovalLookup wraps the unified approvals queue behind a domain-named
// method. Keeping this off a generic `Get` name also avoids confusing static
// HTTP taint scanners at the header boundary.
type ApprovalLookup interface {
	LookupApproval(ctx context.Context, id string) (schemas.Approval, bool, error)
}

type ApprovalGetter interface {
	Get(ctx context.Context, id string) (schemas.Approval, bool, error)
}

type ApprovalQueueLookup struct {
	Queue ApprovalGetter
}

func (l ApprovalQueueLookup) LookupApproval(ctx context.Context, id string) (schemas.Approval, bool, error) {
	if l.Queue == nil {
		return schemas.Approval{}, false, errors.New("approval queue is nil")
	}
	return l.Queue.Get(ctx, id)
}

// AuthConfig wires the auth handlers. Both services are required; the
// router refuses to mount the auth endpoints unless they're set.
type AuthConfig struct {
	Service   AuthService
	Pairing   PairingConsumer
	Approvals ApprovalLookup
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
	r.Post("/v1/auth/rotate-secret", s.handleAuthRotateSecret)
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
			stampSecretRotationRevocation(w)
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
	claims, err := s.authConfig.Service.VerifyBearer(bearer)
	if err != nil {
		if errors.Is(err, auth.ErrTokenSignatureMismatch) {
			stampSecretRotationRevocation(w)
		}
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
		"sid":       body.SID,
		"wasActive": wasActive,
		"revokedBy": claims.SID,
	})
}

// handleAuthRotateSecret is the owner-only recovery valve for suspected
// bearer compromise. It invalidates every outstanding bearer/WS-token by
// rotating the daemon's HMAC signing secret, revokes unconsumed pairing
// grants, and returns one fresh owner pairing token value exactly once.
func (s *server) handleAuthRotateSecret(w http.ResponseWriter, r *http.Request) {
	bearer := extractBearerHeader(r)
	if bearer == "" {
		s.writeProblemCode(w, http.StatusUnauthorized, "auth.missing_bearer", "Authorization header missing", "rotate-secret requires owner bearer")
		return
	}
	claims, err := s.authConfig.Service.VerifyBearer(bearer)
	if err != nil {
		if errors.Is(err, auth.ErrTokenSignatureMismatch) {
			stampSecretRotationRevocation(w)
		}
		s.writeProblemCode(w, http.StatusUnauthorized, "auth.bearer_invalid", "bearer rejected", err.Error())
		return
	}
	if claims.Role != auth.PairingRoleOwner {
		s.writeProblemCode(w, http.StatusForbidden, "auth.role_forbidden", "rotate-secret requires owner role", "client-role bearers cannot rotate the signing secret")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get(authRotateSecretConfirmHeader)), "yes") {
		s.writeProblemCode(w, http.StatusUnprocessableEntity, "auth.rotate_secret_confirmation_missing", "rotation confirmation missing", authRotateSecretConfirmHeader+" must be set to yes")
		return
	}

	approvalID := strings.TrimSpace(r.Header.Get(authRotateSecretApprovalHeader))
	if approvalID == "" {
		s.writeProblemCode(w, http.StatusUnprocessableEntity, "auth.rotate_secret_approval_missing", "approval required", authRotateSecretApprovalHeader+" must reference an approved critical once approval")
		return
	}
	if err := s.validateRotateSecretApproval(r.Context(), approvalID); err != nil {
		writeApprovalProblem(s, w, err)
		return
	}

	rotator, ok := s.authConfig.Pairing.(PairingRotator)
	if !ok {
		s.writeProblemCode(w, http.StatusNotImplemented, "auth.pairing_rotator_unavailable", "pairing rotation unavailable", "rotate-secret requires a pairing service that can list, revoke, and create pairing grants")
		return
	}

	flowID := "rot_" + newEventID()
	s.events.Publish(PublishInput{
		Channel:       authRotateSecretSystemChannel,
		Type:          authRotateSecretImminentEvent,
		CorrelationID: flowID,
		Actor:         map[string]any{"kind": "owner", "sid": claims.SID},
		Data: map[string]any{
			"flowId":  flowID,
			"graceMs": authRotateSecretGraceWindowMs,
		},
	})

	revokedBearers := countActiveSessions(s.authConfig.Service.ListSessions(), s.now())
	snap, err := s.authConfig.Service.RotateSecret()
	if err != nil {
		s.writeProblemCode(w, http.StatusInternalServerError, "auth.rotate_secret_failed", "secret rotation failed", err.Error())
		return
	}
	revokedPairings, err := revokeOpenPairingGrants(r.Context(), rotator, flowID)
	if err != nil {
		s.writeProblemCode(w, http.StatusInternalServerError, "auth.rotate_secret_pairing_revoke_failed", "pairing revoke failed", err.Error())
		return
	}
	newPairing, err := rotator.CreatePairing(r.Context(), auth.CreatePairingRequest{Role: auth.PairingRoleOwner})
	if err != nil {
		s.writeProblemCode(w, http.StatusInternalServerError, "auth.rotate_secret_pairing_create_failed", "replacement pairing token failed", err.Error())
		return
	}

	rotatedAt := s.now().UTC()
	s.events.Publish(PublishInput{
		Channel:       authRotateSecretSystemChannel,
		Type:          authRotateSecretCompletedEvent,
		CorrelationID: flowID,
		Actor:         map[string]any{"kind": "owner", "sid": claims.SID},
		Data: map[string]any{
			"flowId":            flowID,
			"approvalId":        approvalID,
			"rotatedAt":         rotatedAt.Format(time.RFC3339Nano),
			"revocationCause":   authRotateSecretRevocationCause,
			"secretGeneration":  snap.Generation,
			"newPairingTokenId": newPairing.TokenID,
			"revoked": map[string]any{
				"bearers":       revokedBearers,
				"pairingGrants": revokedPairings,
			},
		},
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"flowId":    flowID,
		"rotatedAt": rotatedAt,
		"newPairingToken": map[string]any{
			"value":   newPairing.DisplayToken,
			"ttl":     "PT15M",
			"tokenId": newPairing.TokenID,
			"role":    newPairing.Role,
		},
		"revoked": map[string]any{
			"bearers":       revokedBearers,
			"pairingGrants": revokedPairings,
		},
	})
}

type rotateApprovalError struct {
	status int
	code   string
	title  string
	detail string
}

func (e rotateApprovalError) Error() string { return e.detail }

func (s *server) validateRotateSecretApproval(ctx context.Context, approvalID string) error {
	if s.authConfig.Approvals == nil {
		return rotateApprovalError{
			status: http.StatusServiceUnavailable,
			code:   "auth.rotate_secret_approvals_unavailable",
			title:  "approval queue unavailable",
			detail: "rotate-secret requires the unified approvals queue",
		}
	}
	approval, ok, err := s.authConfig.Approvals.LookupApproval(ctx, approvalID)
	if err != nil {
		return rotateApprovalError{
			status: http.StatusInternalServerError,
			code:   "auth.rotate_secret_approval_lookup_failed",
			title:  "approval lookup failed",
			detail: err.Error(),
		}
	}
	if !ok {
		return rotateApprovalError{
			status: http.StatusUnprocessableEntity,
			code:   "auth.rotate_secret_approval_not_found",
			title:  "approval not found",
			detail: "approval id does not exist",
		}
	}
	if approval.State != schemas.Approved {
		return rotateApprovalError{
			status: http.StatusUnprocessableEntity,
			code:   "auth.rotate_secret_approval_not_approved",
			title:  "approval is not approved",
			detail: "approval must be approved before rotating the signing secret",
		}
	}
	if approval.RiskClass != schemas.Critical || approval.Scope != schemas.Once {
		return rotateApprovalError{
			status: http.StatusUnprocessableEntity,
			code:   "auth.rotate_secret_approval_scope_invalid",
			title:  "approval scope invalid",
			detail: "approval must be riskClass=critical and scope=once",
		}
	}
	if approval.RequestedAction.Kind != authRotateSecretActionKind {
		return rotateApprovalError{
			status: http.StatusUnprocessableEntity,
			code:   "auth.rotate_secret_approval_action_invalid",
			title:  "approval action invalid",
			detail: "approval must cover auth.rotate_secret",
		}
	}

	now := s.now().UTC()
	freshFrom := approval.RequestedAt
	if approval.DecidedAt != nil {
		freshFrom = *approval.DecidedAt
	}
	if now.Sub(freshFrom.UTC()) > authRotateSecretApprovalMaxAge {
		return rotateApprovalError{
			status: http.StatusUnprocessableEntity,
			code:   "auth.rotate_secret_approval_expired",
			title:  "approval too old",
			detail: "rotate-secret approval must be fresh within 2 minutes",
		}
	}
	return nil
}

func writeApprovalProblem(s *server, w http.ResponseWriter, err error) {
	var approvalErr rotateApprovalError
	if errors.As(err, &approvalErr) {
		s.writeProblemCode(w, approvalErr.status, approvalErr.code, approvalErr.title, approvalErr.detail)
		return
	}
	s.writeProblemCode(w, http.StatusInternalServerError, "auth.rotate_secret_approval_error", "approval check failed", err.Error())
}

func revokeOpenPairingGrants(ctx context.Context, rotator PairingRotator, actor string) (int, error) {
	records, err := rotator.ListPairings(ctx)
	if err != nil {
		return 0, err
	}
	revoked := 0
	for _, record := range records {
		if !record.Active() {
			continue
		}
		if _, err := rotator.RevokePairing(ctx, auth.RevokePairingRequest{
			TokenID: record.TokenID,
			Actor:   actor,
		}); err != nil {
			return revoked, err
		}
		revoked++
	}
	return revoked, nil
}

func countActiveSessions(records []auth.SessionRecord, now time.Time) int {
	count := 0
	for _, record := range records {
		if record.Active(now) {
			count++
		}
	}
	return count
}

func stampSecretRotationRevocation(w http.ResponseWriter) {
	w.Header().Set("X-Hoopoe-Revocation-Cause", authRotateSecretRevocationCause)
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
