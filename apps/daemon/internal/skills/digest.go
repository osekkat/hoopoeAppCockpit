package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const sha256Prefix = "sha256:"

func DigestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return sha256Prefix + hex.EncodeToString(sum[:])
}

func NormalizeDigest(digest string) (string, error) {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return "", fmt.Errorf("%w: empty digest", ErrInvalidRequest)
	}
	if strings.HasPrefix(digest, sha256Prefix) {
		digest = strings.TrimPrefix(digest, sha256Prefix)
	}
	if len(digest) != sha256.Size*2 {
		return "", fmt.Errorf("%w: sha256 digest has %d hex chars", ErrInvalidRequest, len(digest))
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("%w: invalid sha256 digest: %v", ErrInvalidRequest, err)
	}
	return sha256Prefix + strings.ToLower(digest), nil
}

func sameDigest(a string, b string) bool {
	normalizedA, errA := NormalizeDigest(a)
	normalizedB, errB := NormalizeDigest(b)
	return errA == nil && errB == nil && normalizedA == normalizedB
}
