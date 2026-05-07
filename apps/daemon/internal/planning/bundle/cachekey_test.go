package bundle

import (
	"errors"
	"strings"
	"testing"
)

// validOpts returns a BuildOpts that passes opts.validate(). Tests
// mutate the returned struct rather than re-list the fields; keeps
// the "what makes opts valid" definition co-located.
func validOpts() BuildOpts {
	return BuildOpts{
		ProjectID:   "demo",
		ProjectRoot: "/tmp/proj",
		CommitSHA:   strings.Repeat("a", 40),
		TokenBudget: 8000,
	}
}

func TestComputeCacheKeyDeterministic(t *testing.T) {
	opts := validOpts()
	hash := strings.Repeat("0", 64)

	first, err := ComputeCacheKey(opts, hash)
	if err != nil {
		t.Fatalf("ComputeCacheKey first: %v", err)
	}
	second, err := ComputeCacheKey(opts, hash)
	if err != nil {
		t.Fatalf("ComputeCacheKey second: %v", err)
	}
	if first != second {
		t.Errorf("ComputeCacheKey not deterministic: %q vs %q", first, second)
	}
}

func TestComputeCacheKeyShape(t *testing.T) {
	key, err := ComputeCacheKey(validOpts(), strings.Repeat("0", 64))
	if err != nil {
		t.Fatalf("ComputeCacheKey: %v", err)
	}
	if !strings.HasPrefix(key, CacheKeyPrefix) {
		t.Errorf("missing prefix: %q", key)
	}
	hash := strings.TrimPrefix(key, CacheKeyPrefix)
	if len(hash) != cacheKeyHashLen {
		t.Errorf("hash len = %d, want %d", len(hash), cacheKeyHashLen)
	}
}

func TestComputeCacheKeyDifferentProjectID(t *testing.T) {
	a, _ := ComputeCacheKey(validOpts(), "h")
	o := validOpts()
	o.ProjectID = "other"
	b, _ := ComputeCacheKey(o, "h")
	if a == b {
		t.Error("different ProjectID should produce different cache key")
	}
}

func TestComputeCacheKeyDifferentCommitSHA(t *testing.T) {
	a, _ := ComputeCacheKey(validOpts(), "h")
	o := validOpts()
	o.CommitSHA = strings.Repeat("b", 40)
	b, _ := ComputeCacheKey(o, "h")
	if a == b {
		t.Error("different CommitSHA should produce different cache key")
	}
}

func TestComputeCacheKeyDifferentTokenBudget(t *testing.T) {
	a, _ := ComputeCacheKey(validOpts(), "h")
	o := validOpts()
	o.TokenBudget = 4000
	b, _ := ComputeCacheKey(o, "h")
	if a == b {
		t.Error("different TokenBudget should produce different cache key")
	}
}

func TestComputeCacheKeyDifferentContentHash(t *testing.T) {
	a, _ := ComputeCacheKey(validOpts(), "hash-A")
	b, _ := ComputeCacheKey(validOpts(), "hash-B")
	if a == b {
		t.Error("different contentHash should produce different cache key")
	}
}

func TestComputeCacheKeyEmptyContentHash(t *testing.T) {
	_, err := ComputeCacheKey(validOpts(), "")
	if !errors.Is(err, ErrInvalidContentHash) {
		t.Errorf("err = %v, want ErrInvalidContentHash", err)
	}
}

func TestComputeCacheKeyInvalidOpts(t *testing.T) {
	o := validOpts()
	o.ProjectID = ""
	_, err := ComputeCacheKey(o, "h")
	if !errors.Is(err, ErrInvalidOpts) {
		t.Errorf("err = %v, want ErrInvalidOpts (ProjectID missing)", err)
	}

	o = validOpts()
	o.CommitSHA = "short"
	_, err = ComputeCacheKey(o, "h")
	if !errors.Is(err, ErrInvalidOpts) {
		t.Errorf("err = %v, want ErrInvalidOpts (CommitSHA too short)", err)
	}
}

func TestMustComputeCacheKeyValid(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MustComputeCacheKey panicked on valid input: %v", r)
		}
	}()
	_ = MustComputeCacheKey(validOpts(), "h")
}

func TestMustComputeCacheKeyPanicsOnInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustComputeCacheKey should panic on invalid opts")
		}
	}()
	_ = MustComputeCacheKey(BuildOpts{}, "h")
}

func TestParseCacheKeyRoundTrip(t *testing.T) {
	key, err := ComputeCacheKey(validOpts(), "h")
	if err != nil {
		t.Fatalf("ComputeCacheKey: %v", err)
	}
	hash, err := ParseCacheKey(key)
	if err != nil {
		t.Fatalf("ParseCacheKey: %v", err)
	}
	if len(hash) != cacheKeyHashLen {
		t.Errorf("ParseCacheKey returned %d-char hash, want %d", len(hash), cacheKeyHashLen)
	}
	if !strings.HasPrefix(key, CacheKeyPrefix+hash) {
		t.Errorf("round-trip mismatch: key=%q hash=%q", key, hash)
	}
}

func TestParseCacheKeyMissingPrefix(t *testing.T) {
	_, err := ParseCacheKey(strings.Repeat("a", 64))
	if !errors.Is(err, ErrInvalidCacheKey) {
		t.Errorf("err = %v, want ErrInvalidCacheKey", err)
	}
}

func TestParseCacheKeyBadHexLen(t *testing.T) {
	_, err := ParseCacheKey(CacheKeyPrefix + "abc")
	if !errors.Is(err, ErrInvalidCacheKey) {
		t.Errorf("err = %v, want ErrInvalidCacheKey (short hex)", err)
	}
}

func TestParseCacheKeyNonHex(t *testing.T) {
	_, err := ParseCacheKey(CacheKeyPrefix + strings.Repeat("z", cacheKeyHashLen))
	if !errors.Is(err, ErrInvalidCacheKey) {
		t.Errorf("err = %v, want ErrInvalidCacheKey (non-hex)", err)
	}
}

func TestComputeCacheKeyNullSeparatorAvoidsAmbiguity(t *testing.T) {
	// If the payload joined fields with `-` or `=`, "budget=10" + "" could
	// alias with "budget=" + "10" + "" → same payload bytes. The NUL
	// separator makes that impossible. Guard the invariant: two opts
	// that "look like" they could collide under a fragile separator
	// produce different keys here.
	a := validOpts()
	a.ProjectID = "x"
	a.CommitSHA = strings.Repeat("0", 40)

	b := validOpts()
	// Smuggle a budget-like substring into ProjectID.
	b.ProjectID = "x\x00" + strings.Repeat("0", 40)
	b.CommitSHA = strings.Repeat("a", 40)

	keyA, errA := ComputeCacheKey(a, "h")
	keyB, errB := ComputeCacheKey(b, "h")
	if errA != nil || errB != nil {
		t.Fatalf("compute err: a=%v b=%v", errA, errB)
	}
	if keyA == keyB {
		t.Error("crafted aliasing inputs collided — separator failed")
	}
}
