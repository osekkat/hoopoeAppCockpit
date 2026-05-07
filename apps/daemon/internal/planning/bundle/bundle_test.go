package bundle

import (
	"context"
	"errors"
	"testing"
)

const validSHA = "0123456789abcdef0123456789abcdef01234567"

func TestSchemaVersionMatchesOpenAPI(t *testing.T) {
	// hp-rsly: this constant must mirror the openapi.yaml
	// `ExistingCodebaseContextBundle.schemaVersion` enum (currently
	// [1]). Bump both together when the contract changes.
	if SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1 (mirror openapi.yaml)", SchemaVersion)
	}
}

func TestNewBuilderReturnsNotImplemented(t *testing.T) {
	b := NewBuilder()
	bundle, err := b.Build(context.Background(), BuildOpts{
		ProjectID:   "demo",
		ProjectRoot: "/tmp/demo",
		CommitSHA:   validSHA,
	})
	if bundle != nil {
		t.Fatalf("Build returned a non-nil bundle while assembly is unimplemented")
	}
	if !errors.Is(err, ErrAssemblyNotImplemented) {
		t.Fatalf("err = %v, want ErrAssemblyNotImplemented", err)
	}
}

func TestBuildHonoursContextCancellation(t *testing.T) {
	b := NewBuilder()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.Build(ctx, BuildOpts{
		ProjectID:   "demo",
		ProjectRoot: "/tmp/demo",
		CommitSHA:   validSHA,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestBuildOptsValidate(t *testing.T) {
	cases := []struct {
		name string
		opts BuildOpts
		want error
	}{
		{
			name: "missing project id",
			opts: BuildOpts{ProjectRoot: "/tmp/x", CommitSHA: validSHA},
			want: ErrInvalidOpts,
		},
		{
			name: "missing project root",
			opts: BuildOpts{ProjectID: "demo", CommitSHA: validSHA},
			want: ErrInvalidOpts,
		},
		{
			name: "missing commit sha",
			opts: BuildOpts{ProjectID: "demo", ProjectRoot: "/tmp/x"},
			want: ErrInvalidOpts,
		},
		{
			name: "short commit sha",
			opts: BuildOpts{ProjectID: "demo", ProjectRoot: "/tmp/x", CommitSHA: "deadbeef"},
			want: ErrInvalidOpts,
		},
		{
			name: "negative token budget",
			opts: BuildOpts{ProjectID: "demo", ProjectRoot: "/tmp/x", CommitSHA: validSHA, TokenBudget: -1},
			want: ErrInvalidOpts,
		},
		{
			name: "valid",
			opts: BuildOpts{ProjectID: "demo", ProjectRoot: "/tmp/x", CommitSHA: validSHA, TokenBudget: 0},
			want: nil,
		},
		{
			name: "valid with budget",
			opts: BuildOpts{ProjectID: "demo", ProjectRoot: "/tmp/x", CommitSHA: validSHA, TokenBudget: 4096},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.validate()
			if tc.want == nil {
				if err != nil {
					t.Fatalf("validate err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("validate err = %v, want errors.Is(%v)", err, tc.want)
			}
		})
	}
}

func TestInvalidOptsErrorIsAndUnwrap(t *testing.T) {
	err := errInvalidOpts("test message")
	if !errors.Is(err, ErrInvalidOpts) {
		t.Fatal("errors.Is(err, ErrInvalidOpts) = false")
	}
	unwrapped := errors.Unwrap(err)
	if unwrapped != ErrInvalidOpts {
		t.Fatalf("Unwrap = %v, want ErrInvalidOpts", unwrapped)
	}
	if !contains(err.Error(), "test message") {
		t.Fatalf("error string = %q, missing custom message", err.Error())
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && haystack[len(haystack)-len(needle):] == needle ||
		indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
