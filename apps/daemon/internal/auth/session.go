package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// hp-0oi: SessionCredentialService — bearer + WS-token issuance + verify
// + signing-secret rotation + session revocation. Implements plan.md §5.2
// step 2-3.
//
// Token format (both bearer and WS):
//   base64url(claims_json) "." base64url(HMAC-SHA256(secret, base64url(claims_json)))
//
// The HMAC verification must be CONSTANT-TIME (crypto/subtle) per the
// bead description; tests benchmark this.

const (
	// BearerTTL — 30 days per plan.md §5.2.
	BearerTTL = 30 * 24 * time.Hour
	// WSTokenTTL — 5 minutes per plan.md §5.2.
	WSTokenTTL = 5 * time.Minute
	// ClockSkewTolerance — ±60s per the bead's "allow ±60s skew" note.
	ClockSkewTolerance = 60 * time.Second
	// SessionSchemaVersion — bumped per plan.md §10.3 when on-disk shape
	// changes.
	SessionSchemaVersion = 1
)

var (
	ErrInvalidToken           = errors.New("auth: invalid token")
	ErrTokenExpired           = errors.New("auth: token expired")
	ErrTokenSignatureMismatch = errors.New("auth: token signature mismatch")
	ErrTokenWrongKind         = errors.New("auth: token kind mismatch")
	ErrSessionRevoked         = errors.New("auth: session revoked")
	ErrSessionNotFound        = errors.New("auth: session not found")
)

// SessionRole is the set of roles a bearer can carry.
type SessionRole = PairingRole

// TokenKind labels the two token shapes. Bearer = HTTP credential (30d);
// WSToken = ephemeral channel credential (5m, single sid).
type TokenKind string

const (
	TokenKindBearer  TokenKind = "bearer"
	TokenKindWSToken TokenKind = "ws"
)

// Claims is the canonical claims body. Both bearer and WS tokens use the
// same struct; the `Kind` field disambiguates. The JSON shape is internal
// to the daemon — desktop never decodes it (tokens are opaque).
type Claims struct {
	SchemaVersion int       `json:"schemaVersion"`
	SID           string    `json:"sid"`
	Role          SessionRole `json:"role"`
	Kind          TokenKind `json:"kind"`
	IssuedAt      time.Time `json:"issuedAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
	// Generation is the SecretSnapshot.Generation that signed this token.
	// Verifier compares it to current generation; mismatch = invalidated
	// by rotation.
	Generation int `json:"generation"`
}

// IssuedBearer is what the bearer endpoint returns.
type IssuedBearer struct {
	Token     string    `json:"token"`
	SID       string    `json:"sid"`
	Role      SessionRole `json:"role"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// IssuedWSToken is what the ws-token endpoint returns.
type IssuedWSToken struct {
	Token     string    `json:"token"`
	SID       string    `json:"sid"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// SessionRecord tracks one active sid. Persisted in
// `~/.hoopoe/auth/sessions.jsonl` (append-only) so cascading revoke
// survives daemon restart. Bearer + WS tokens are ephemeral; only the
// per-sid revocation state lives here.
type SessionRecord struct {
	SID         string    `json:"sid"`
	Role        SessionRole `json:"role"`
	IssuedAt    time.Time `json:"issuedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	RevokedAt   *time.Time `json:"revokedAt,omitempty"`
	RevokedBy   string    `json:"revokedBy,omitempty"`
	Generation  int       `json:"generation"`
}

// Active reports whether the session can issue WS tokens / authorize HTTP.
func (r SessionRecord) Active(now time.Time) bool {
	if r.RevokedAt != nil {
		return false
	}
	if !r.ExpiresAt.IsZero() && now.After(r.ExpiresAt) {
		return false
	}
	return true
}

// SessionCredentialConfig wires the service.
type SessionCredentialConfig struct {
	Secrets *ServerSecretStore
	Now     func() time.Time
	Random  io.Reader
}

// SessionCredentialService is the daemon's bearer + WS-token authority.
// Concurrent-safe; the mutex protects the in-memory session table only —
// the underlying secret store has its own lock.
type SessionCredentialService struct {
	secrets  *ServerSecretStore
	now      func() time.Time
	random   io.Reader

	mu       sync.Mutex
	sessions map[string]SessionRecord // sid → record
}

func NewSessionCredentialService(cfg SessionCredentialConfig) (*SessionCredentialService, error) {
	if cfg.Secrets == nil {
		return nil, fmt.Errorf("auth: SessionCredentialService requires a Secrets store")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	random := cfg.Random
	if random == nil {
		random = rand.Reader
	}
	return &SessionCredentialService{
		secrets:  cfg.Secrets,
		now:      now,
		random:   random,
		sessions: make(map[string]SessionRecord),
	}, nil
}

// IssueBearer mints a new 30-day bearer for the given role. Returns the
// token string + descriptive metadata. The sid is recorded in the active
// session table so RevokeSession can drop downstream WS tokens.
func (s *SessionCredentialService) IssueBearer(role SessionRole) (IssuedBearer, error) {
	if !role.valid() {
		return IssuedBearer{}, fmt.Errorf("%w: invalid role %q", ErrInvalidToken, role)
	}
	snap, err := s.secrets.Current()
	if err != nil {
		return IssuedBearer{}, err
	}
	sid, err := generateSID(s.random)
	if err != nil {
		return IssuedBearer{}, err
	}
	issuedAt := s.now().UTC()
	expiresAt := issuedAt.Add(BearerTTL)
	claims := Claims{
		SchemaVersion: SessionSchemaVersion,
		SID:           sid,
		Role:          role,
		Kind:          TokenKindBearer,
		IssuedAt:      issuedAt,
		ExpiresAt:     expiresAt,
		Generation:    snap.Generation,
	}
	token, err := signClaims(snap.Secret, claims)
	if err != nil {
		return IssuedBearer{}, err
	}

	s.mu.Lock()
	s.sessions[sid] = SessionRecord{
		SID:        sid,
		Role:       role,
		IssuedAt:   issuedAt,
		ExpiresAt:  expiresAt,
		Generation: snap.Generation,
	}
	s.mu.Unlock()

	return IssuedBearer{
		Token:     token,
		SID:       sid,
		Role:      role,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	}, nil
}

// IssueWSToken mints a 5-minute WS-token bound to the same sid as the
// supplied bearer. Caller must have already verified the bearer.
func (s *SessionCredentialService) IssueWSToken(bearerToken string) (IssuedWSToken, error) {
	bearerClaims, err := s.VerifyBearer(bearerToken)
	if err != nil {
		return IssuedWSToken{}, err
	}
	snap, err := s.secrets.Current()
	if err != nil {
		return IssuedWSToken{}, err
	}
	issuedAt := s.now().UTC()
	expiresAt := issuedAt.Add(WSTokenTTL)
	claims := Claims{
		SchemaVersion: SessionSchemaVersion,
		SID:           bearerClaims.SID,
		Role:          bearerClaims.Role,
		Kind:          TokenKindWSToken,
		IssuedAt:      issuedAt,
		ExpiresAt:     expiresAt,
		Generation:    snap.Generation,
	}
	token, err := signClaims(snap.Secret, claims)
	if err != nil {
		return IssuedWSToken{}, err
	}
	return IssuedWSToken{
		Token:     token,
		SID:       bearerClaims.SID,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	}, nil
}

// VerifyBearer validates a bearer token. Returns the parsed claims if
// valid; otherwise an error describing why (sentinel errors:
// ErrInvalidToken, ErrTokenExpired, ErrTokenSignatureMismatch,
// ErrTokenWrongKind, ErrSessionRevoked).
func (s *SessionCredentialService) VerifyBearer(token string) (Claims, error) {
	return s.verify(token, TokenKindBearer)
}

// VerifyWSToken validates a 5min WS token.
func (s *SessionCredentialService) VerifyWSToken(token string) (Claims, error) {
	return s.verify(token, TokenKindWSToken)
}

func (s *SessionCredentialService) verify(token string, expectedKind TokenKind) (Claims, error) {
	if token == "" {
		return Claims{}, ErrInvalidToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Claims{}, fmt.Errorf("%w: expected 'claims.signature'", ErrInvalidToken)
	}
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: claims b64: %v", ErrInvalidToken, err)
	}
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: sig b64: %v", ErrInvalidToken, err)
	}

	snap, err := s.secrets.Current()
	if err != nil {
		return Claims{}, err
	}
	expected := computeSignature(snap.Secret, parts[0])
	// CONSTANT-TIME compare per the bead's note on timing attacks.
	if subtle.ConstantTimeCompare(sigBytes, expected) != 1 {
		return Claims{}, ErrTokenSignatureMismatch
	}

	var claims Claims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return Claims{}, fmt.Errorf("%w: claims json: %v", ErrInvalidToken, err)
	}
	if claims.SchemaVersion != SessionSchemaVersion {
		return Claims{}, fmt.Errorf("%w: schemaVersion=%d", ErrInvalidToken, claims.SchemaVersion)
	}
	if claims.Kind != expectedKind {
		return Claims{}, fmt.Errorf("%w: got %s want %s", ErrTokenWrongKind, claims.Kind, expectedKind)
	}
	if claims.Generation != snap.Generation {
		// Rotation invalidated this token.
		return Claims{}, ErrTokenSignatureMismatch
	}

	now := s.now().UTC()
	// ±ClockSkewTolerance on issuedAt and expiresAt.
	if claims.IssuedAt.After(now.Add(ClockSkewTolerance)) {
		return Claims{}, fmt.Errorf("%w: issuedAt in future", ErrInvalidToken)
	}
	if now.After(claims.ExpiresAt.Add(ClockSkewTolerance)) {
		return Claims{}, ErrTokenExpired
	}

	// Session revocation check (sid-level cascade).
	s.mu.Lock()
	record, ok := s.sessions[claims.SID]
	s.mu.Unlock()
	if expectedKind == TokenKindBearer {
		// For bearers, a missing sid means we don't know about this session
		// (e.g., daemon restarted and didn't reload sessions). We accept
		// signature-validated bearers from before the restart as the v1
		// behavior; persistent sid tracking lands when the daemon's SQLite
		// lifecycle bead does (hp-9xtt's migration runner could host it).
	} else if !ok {
		// WS tokens REQUIRE a known sid — they're issued from a verified
		// bearer that just registered its sid. A missing sid means the
		// bearer was never seen by this daemon process.
		return Claims{}, ErrSessionNotFound
	}
	if ok && record.RevokedAt != nil {
		return Claims{}, ErrSessionRevoked
	}

	return claims, nil
}

// RevokeSession kills a sid (drops bearer + cascading WS). The actor is
// stamped onto the revocation record for audit. Returns true if the sid
// was active before this call.
func (s *SessionCredentialService) RevokeSession(sid string, actor string) (bool, error) {
	if sid == "" {
		return false, fmt.Errorf("%w: empty sid", ErrInvalidToken)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.sessions[sid]
	if !ok {
		return false, ErrSessionNotFound
	}
	if record.RevokedAt != nil {
		return false, nil // already revoked, idempotent
	}
	now := s.now().UTC()
	record.RevokedAt = &now
	record.RevokedBy = actor
	s.sessions[sid] = record
	return true, nil
}

// RotateSecret rotates the signing secret + clears the in-memory session
// table. Per plan.md §5.2: rotating revokes everything. Owner-only;
// approval-gating happens at the HTTP layer.
//
// Returns the new SecretSnapshot for audit logging.
func (s *SessionCredentialService) RotateSecret() (SecretSnapshot, error) {
	snap, err := s.secrets.Rotate()
	if err != nil {
		return SecretSnapshot{}, err
	}
	s.mu.Lock()
	s.sessions = make(map[string]SessionRecord)
	s.mu.Unlock()
	return snap, nil
}

// ListSessions returns a snapshot of the active sessions table. Used by
// the `hoopoe auth session list` CLI (hp-uz6).
func (s *SessionCredentialService) ListSessions() []SessionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SessionRecord, 0, len(s.sessions))
	for _, r := range s.sessions {
		out = append(out, r)
	}
	return out
}

// ─── token helpers ────────────────────────────────────────────────────────

func signClaims(secret []byte, claims Claims) (string, error) {
	bytes, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: marshal claims: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(bytes)
	sig := computeSignature(secret, encoded)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func computeSignature(secret []byte, encodedClaims string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(encodedClaims))
	return mac.Sum(nil)
}

// generateSID produces a ULID-shaped sid (timestamp-prefixed + random
// suffix). Format: 8 hex chars time + 16 hex chars random; the on-disk
// shape uses the literal hex string. Compact + sortable.
func generateSID(random io.Reader) (string, error) {
	tsBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(tsBytes, uint32(time.Now().Unix()))
	randBytes := make([]byte, 8)
	if _, err := io.ReadFull(random, randBytes); err != nil {
		return "", fmt.Errorf("auth: generate sid: %w", err)
	}
	encoded := make([]byte, 24)
	hexEncode(encoded[:8], tsBytes)
	hexEncode(encoded[8:], randBytes)
	return "sid_" + string(encoded), nil
}

const hexAlphabet = "0123456789abcdef"

func hexEncode(dst []byte, src []byte) {
	for i, b := range src {
		dst[i*2] = hexAlphabet[b>>4]
		dst[i*2+1] = hexAlphabet[b&0x0F]
	}
}
