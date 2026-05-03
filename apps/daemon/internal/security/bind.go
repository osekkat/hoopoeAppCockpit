// Package security centralizes daemon transport hardening policy.
package security

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	PublicBindWarningCode = "security.public_bind"
	PublicBindTokenTTL    = 10 * time.Minute
)

var (
	ErrInvalidBindAddress       = errors.New("security: invalid bind address")
	ErrPublicBindNotConfirmed   = errors.New("security: public bind requires runtime confirmation")
	ErrInvalidConfirmationToken = errors.New("security: invalid public-bind confirmation token")
	ErrConfirmationTokenUsed    = errors.New("security: public-bind confirmation token already used")
)

type PublicBindConfirmer interface {
	ConfirmPublicBind(ctx context.Context, token string, target BindTarget) error
}

type BindTarget struct {
	Address string `json:"address"`
	Host    string `json:"host"`
	Port    string `json:"port"`
}

type BindRequest struct {
	Address            string
	ConfigAllowsPublic bool
	ConfirmationToken  string
	Confirmer          PublicBindConfirmer
}

type BindDecision struct {
	RequestedAddress   string       `json:"requestedAddress"`
	EffectiveAddress   string       `json:"effectiveAddress"`
	Host               string       `json:"host"`
	Port               string       `json:"port"`
	ConfigAllowsPublic bool         `json:"configAllowsPublic"`
	RuntimeConfirmed   bool         `json:"runtimeConfirmed"`
	Loopback           bool         `json:"loopback"`
	Tailnet            bool         `json:"tailnet"`
	PublicExposure     bool         `json:"publicExposure"`
	Warning            *BindWarning `json:"warning,omitempty"`
}

type BindWarning struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	DismissalID string `json:"dismissalId"`
}

type BindReport struct {
	SchemaVersion int          `json:"schemaVersion"`
	GeneratedAt   time.Time    `json:"generatedAt"`
	Decision      BindDecision `json:"decision"`
}

type RejectPublicBindConfirmer struct{}

func (RejectPublicBindConfirmer) ConfirmPublicBind(context.Context, string, BindTarget) error {
	return ErrPublicBindNotConfirmed
}

func EvaluateBind(ctx context.Context, req BindRequest) (BindDecision, error) {
	if req.Address == "" {
		req.Address = "127.0.0.1:0"
	}
	target, err := ParseBindTarget(req.Address)
	if err != nil {
		return BindDecision{}, err
	}

	loopback := IsLoopbackHost(target.Host)
	tailnet := IsTailnetHost(target.Host)
	decision := BindDecision{
		RequestedAddress:   target.Address,
		EffectiveAddress:   target.Address,
		Host:               target.Host,
		Port:               target.Port,
		ConfigAllowsPublic: req.ConfigAllowsPublic,
		Loopback:           loopback,
		Tailnet:            tailnet,
		PublicExposure:     !loopback && !tailnet,
	}
	if !decision.PublicExposure {
		return decision, nil
	}

	if req.ConfigAllowsPublic {
		confirmer := req.Confirmer
		if confirmer == nil {
			confirmer = RejectPublicBindConfirmer{}
		}
		if err := confirmer.ConfirmPublicBind(ctx, req.ConfirmationToken, target); err == nil {
			decision.RuntimeConfirmed = true
			decision.Warning = publicBindAllowedWarning(target.Address)
			return decision, nil
		} else if !errors.Is(err, ErrPublicBindNotConfirmed) && !errors.Is(err, ErrInvalidConfirmationToken) && !errors.Is(err, ErrConfirmationTokenUsed) {
			return BindDecision{}, err
		}
	}

	decision.EffectiveAddress = loopbackFallbackAddress(target)
	decision.Warning = publicBindFallbackWarning(target.Address, decision.EffectiveAddress)
	return decision, nil
}

func NewBindReport(decision BindDecision, now time.Time) BindReport {
	return BindReport{
		SchemaVersion: 1,
		GeneratedAt:   now.UTC(),
		Decision:      decision,
	}
}

func ParseBindTarget(addr string) (BindTarget, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return BindTarget{}, fmt.Errorf("%w: %v", ErrInvalidBindAddress, err)
	}
	if port == "" {
		return BindTarget{}, fmt.Errorf("%w: empty port", ErrInvalidBindAddress)
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return BindTarget{}, fmt.Errorf("%w: invalid port %q", ErrInvalidBindAddress, port)
	}
	if host == "" {
		host = "0.0.0.0"
	}
	return BindTarget{
		Address: net.JoinHostPort(host, port),
		Host:    host,
		Port:    port,
	}, nil
}

func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func IsTailnetHost(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
	}
	tailscalePrefix := net.ParseIP("fd7a:115c:a1e0::")
	mask := net.CIDRMask(48, 128)
	return ip.Mask(mask).Equal(tailscalePrefix.Mask(mask))
}

func loopbackFallbackAddress(target BindTarget) string {
	if strings.Contains(target.Host, ":") && net.ParseIP(target.Host).To4() == nil {
		return net.JoinHostPort("::1", target.Port)
	}
	return net.JoinHostPort("127.0.0.1", target.Port)
}

func publicBindAllowedWarning(address string) *BindWarning {
	return &BindWarning{
		Code:        PublicBindWarningCode,
		Severity:    "critical",
		Message:     fmt.Sprintf("Daemon is bound to %s. Public exposure is high-risk. Verify mTLS is configured, firewall rules restrict access, and that this is intentional.", address),
		DismissalID: "bind:" + address,
	}
}

func publicBindFallbackWarning(requestedAddress string, effectiveAddress string) *BindWarning {
	return &BindWarning{
		Code:        PublicBindWarningCode,
		Severity:    "warning",
		Message:     fmt.Sprintf("Public bind to %s was requested without both required confirmations. The daemon is bound to %s instead.", requestedAddress, effectiveAddress),
		DismissalID: "bind-refused:" + requestedAddress,
	}
}

type HMACPublicBindConfirmer struct {
	Secret []byte
	Now    func() time.Time

	mu       sync.Mutex
	consumed map[string]time.Time
}

type publicBindTokenClaims struct {
	TokenID   string    `json:"tokenId"`
	Operation string    `json:"operation"`
	Address   string    `json:"address"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (c *HMACPublicBindConfirmer) ConfirmPublicBind(ctx context.Context, token string, target BindTarget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	claims, err := c.parse(token)
	if err != nil {
		return err
	}
	if claims.Operation != "daemon.public_bind" || claims.Address != target.Address {
		return ErrInvalidConfirmationToken
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	if !claims.ExpiresAt.After(now().UTC()) {
		return ErrInvalidConfirmationToken
	}
	if claims.TokenID == "" {
		return ErrInvalidConfirmationToken
	}
	if err := c.consume(claims.TokenID, claims.ExpiresAt, now().UTC()); err != nil {
		return err
	}
	return nil
}

func (c *HMACPublicBindConfirmer) Mint(target BindTarget, expiresAt time.Time) (string, error) {
	if len(c.Secret) < 32 {
		return "", fmt.Errorf("%w: confirmation secret must be at least 32 bytes", ErrInvalidConfirmationToken)
	}
	tokenID, err := newTokenID()
	if err != nil {
		return "", err
	}
	claims := publicBindTokenClaims{
		TokenID:   tokenID,
		Operation: "daemon.public_bind",
		Address:   target.Address,
		ExpiresAt: expiresAt.UTC(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	sig := sign(c.Secret, payload)
	return "bindv1." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (c *HMACPublicBindConfirmer) parse(token string) (publicBindTokenClaims, error) {
	if len(c.Secret) < 32 {
		return publicBindTokenClaims{}, fmt.Errorf("%w: confirmation secret must be at least 32 bytes", ErrInvalidConfirmationToken)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "bindv1" {
		return publicBindTokenClaims{}, ErrInvalidConfirmationToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return publicBindTokenClaims{}, ErrInvalidConfirmationToken
	}
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return publicBindTokenClaims{}, ErrInvalidConfirmationToken
	}
	wantSig := sign(c.Secret, payload)
	if !hmac.Equal(gotSig, wantSig) {
		return publicBindTokenClaims{}, ErrInvalidConfirmationToken
	}
	var claims publicBindTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return publicBindTokenClaims{}, ErrInvalidConfirmationToken
	}
	return claims, nil
}

func (c *HMACPublicBindConfirmer) consume(tokenID string, expiresAt time.Time, now time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.consumed == nil {
		c.consumed = make(map[string]time.Time)
	}
	for id, expiry := range c.consumed {
		if !expiry.After(now) {
			delete(c.consumed, id)
		}
	}
	if _, ok := c.consumed[tokenID]; ok {
		return ErrConfirmationTokenUsed
	}
	c.consumed[tokenID] = expiresAt
	return nil
}

func newTokenID() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func sign(secret []byte, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return mac.Sum(nil)
}
