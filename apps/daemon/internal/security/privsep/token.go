package privsep

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	ErrApprovalTokenMissing   = errors.New("privsep: approval token missing")
	ErrApprovalExpired        = errors.New("privsep: approval expired")
	ErrApprovalInvalid        = errors.New("privsep: approval signature invalid")
	ErrApprovalAlreadyUsed    = errors.New("privsep: approval already used")
	ErrApprovalActionMismatch = errors.New("privsep: approval action mismatch")
)

type TokenClaims struct {
	SchemaVersion   int       `json:"schemaVersion"`
	ApprovalID      string    `json:"approvalId"`
	Actor           string    `json:"actor"`
	Action          string    `json:"action"`
	Nonce           string    `json:"nonce"`
	ExpiresAt       time.Time `json:"expiresAt"`
	AllowlistDigest string    `json:"allowlistDigest"`
}

type TokenRequest struct {
	ApprovalID      string
	Actor           string
	Action          string
	ExpiresAt       time.Time
	AllowlistDigest string
}

type TokenValidator interface {
	ValidateApprovalToken(ctx context.Context, token string, req TokenValidationRequest) (TokenClaims, error)
}

type TokenValidationRequest struct {
	Action          string
	AllowlistDigest string
	Now             time.Time
}

type TokenStore interface {
	Consume(ctx context.Context, approvalID, nonce string, expiresAt time.Time) (bool, error)
}

type HMACTokenAuthority struct {
	Secret []byte
	Store  TokenStore
	Now    func() time.Time
}

func (a HMACTokenAuthority) Mint(req TokenRequest) (string, error) {
	if len(a.Secret) < 16 {
		return "", fmt.Errorf("%w: HMAC secret must be at least 16 bytes", ErrApprovalInvalid)
	}
	nonce, err := randomNonce()
	if err != nil {
		return "", err
	}
	claims := TokenClaims{
		SchemaVersion:   SchemaVersion,
		ApprovalID:      strings.TrimSpace(req.ApprovalID),
		Actor:           strings.TrimSpace(req.Actor),
		Action:          strings.TrimSpace(req.Action),
		Nonce:           nonce,
		ExpiresAt:       req.ExpiresAt.UTC(),
		AllowlistDigest: normalizeDigest(req.AllowlistDigest),
	}
	if claims.ApprovalID == "" || claims.Actor == "" || claims.Action == "" || claims.AllowlistDigest == "" || claims.ExpiresAt.IsZero() {
		return "", fmt.Errorf("%w: incomplete token request", ErrApprovalInvalid)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, a.Secret)
	mac.Write([]byte(encodedPayload))
	signature := mac.Sum(nil)
	return encodedPayload + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (a HMACTokenAuthority) ValidateApprovalToken(ctx context.Context, token string, req TokenValidationRequest) (TokenClaims, error) {
	if strings.TrimSpace(token) == "" {
		return TokenClaims{}, ErrApprovalTokenMissing
	}
	if len(a.Secret) < 16 {
		return TokenClaims{}, fmt.Errorf("%w: HMAC secret must be at least 16 bytes", ErrApprovalInvalid)
	}
	payloadText, signatureText, ok := strings.Cut(token, ".")
	if !ok {
		return TokenClaims{}, ErrApprovalInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(signatureText)
	if err != nil {
		return TokenClaims{}, ErrApprovalInvalid
	}
	mac := hmac.New(sha256.New, a.Secret)
	mac.Write([]byte(payloadText))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return TokenClaims{}, ErrApprovalInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadText)
	if err != nil {
		return TokenClaims{}, ErrApprovalInvalid
	}
	var claims TokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return TokenClaims{}, ErrApprovalInvalid
	}
	if claims.SchemaVersion != SchemaVersion || claims.ApprovalID == "" || claims.Nonce == "" || claims.Action == "" {
		return TokenClaims{}, ErrApprovalInvalid
	}
	now := req.Now
	if now.IsZero() {
		now = a.now()
	}
	if !claims.ExpiresAt.After(now) {
		return TokenClaims{}, ErrApprovalExpired
	}
	if req.Action != "" && claims.Action != req.Action {
		return TokenClaims{}, ErrApprovalActionMismatch
	}
	if normalizeDigest(req.AllowlistDigest) != "" && normalizeDigest(claims.AllowlistDigest) != normalizeDigest(req.AllowlistDigest) {
		return TokenClaims{}, ErrAllowlistChecksumMismatch
	}
	store := a.Store
	if store == nil {
		return TokenClaims{}, fmt.Errorf("%w: token replay store is required", ErrApprovalInvalid)
	}
	ok, err = store.Consume(ctx, claims.ApprovalID, claims.Nonce, claims.ExpiresAt)
	if err != nil {
		return TokenClaims{}, err
	}
	if !ok {
		return TokenClaims{}, ErrApprovalAlreadyUsed
	}
	return claims, nil
}

func (a HMACTokenAuthority) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

type MemoryTokenStore struct {
	mu       sync.Mutex
	consumed map[string]time.Time
	now      func() time.Time
}

func NewMemoryTokenStore() *MemoryTokenStore {
	return &MemoryTokenStore{
		consumed: make(map[string]time.Time),
		now:      time.Now,
	}
}

func (s *MemoryTokenStore) Consume(_ context.Context, approvalID, nonce string, expiresAt time.Time) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("%w: nil token store", ErrApprovalInvalid)
	}
	key := approvalID + ":" + nonce
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consumed == nil {
		s.consumed = make(map[string]time.Time)
	}
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	for existing, expiry := range s.consumed {
		if expiry.Before(now()) {
			delete(s.consumed, existing)
		}
	}
	if _, exists := s.consumed[key]; exists {
		return false, nil
	}
	s.consumed[key] = expiresAt
	return true, nil
}

type DaemonSocketTokenValidator struct {
	SocketPath string
	Client     *http.Client
}

func (v DaemonSocketTokenValidator) ValidateApprovalToken(ctx context.Context, token string, req TokenValidationRequest) (TokenClaims, error) {
	if strings.TrimSpace(v.SocketPath) == "" {
		return TokenClaims{}, fmt.Errorf("%w: daemon socket path is required", ErrApprovalInvalid)
	}
	client := v.Client
	if client == nil {
		transport := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", v.SocketPath)
			},
		}
		client = &http.Client{Transport: transport, Timeout: 5 * time.Second}
	}
	bodyPayload, err := json.Marshal(struct {
		Token           string `json:"token"`
		Action          string `json:"action"`
		AllowlistDigest string `json:"allowlistDigest"`
	}{
		Token:           token,
		Action:          req.Action,
		AllowlistDigest: normalizeDigest(req.AllowlistDigest),
	})
	if err != nil {
		return TokenClaims{}, err
	}
	body := bytes.NewReader(bodyPayload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/internal/setup-helper/approval-token/validate", body)
	if err != nil {
		return TokenClaims{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return TokenClaims{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return TokenClaims{}, fmt.Errorf("%w: daemon validator returned %d", ErrApprovalInvalid, resp.StatusCode)
	}
	var out TokenClaims
	dec := json.NewDecoder(resp.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return TokenClaims{}, err
	}
	if out.SchemaVersion != SchemaVersion || out.ApprovalID == "" || out.Action == "" {
		return TokenClaims{}, ErrApprovalInvalid
	}
	if req.Action != "" && out.Action != req.Action {
		return TokenClaims{}, ErrApprovalActionMismatch
	}
	if normalizeDigest(req.AllowlistDigest) != "" && normalizeDigest(out.AllowlistDigest) != normalizeDigest(req.AllowlistDigest) {
		return TokenClaims{}, ErrAllowlistChecksumMismatch
	}
	return out, nil
}

func randomNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
