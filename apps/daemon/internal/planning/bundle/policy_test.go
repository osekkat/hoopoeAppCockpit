package bundle

import (
	"errors"
	"strings"
	"testing"
)

func TestApplyPolicyNilRejected(t *testing.T) {
	_, err := ApplyPolicy([]string{"a.ts"}, nil)
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Errorf("err = %v, want ErrInvalidPolicy", err)
	}
}

func TestApplyPolicyAdmitsBenignPath(t *testing.T) {
	p := DefaultPolicy()
	d, err := ApplyPolicy([]string{"src/foo.ts", "README.md", "docs/architecture/01.md"}, &p)
	if err != nil {
		t.Fatalf("ApplyPolicy: %v", err)
	}
	for _, dec := range d {
		if !dec.Admitted {
			t.Errorf("benign path %q rejected: %s", dec.Path, dec.Reason)
		}
	}
}

func TestApplyPolicyBlocksDotEnv(t *testing.T) {
	p := DefaultPolicy()
	cases := []string{".env", ".env.local", ".env.production"}
	for _, c := range cases {
		d, err := ApplyPolicy([]string{c}, &p)
		if err != nil {
			t.Fatalf("ApplyPolicy(%q): %v", c, err)
		}
		if d[0].Admitted {
			t.Errorf("%q admitted; want blocked", c)
		}
	}
}

func TestApplyPolicyBlocksPemAndKey(t *testing.T) {
	p := DefaultPolicy()
	cases := []string{"server.pem", "deploy.key", "id_rsa", "id_ed25519"}
	for _, c := range cases {
		d, err := ApplyPolicy([]string{c}, &p)
		if err != nil {
			t.Fatalf("ApplyPolicy(%q): %v", c, err)
		}
		if d[0].Admitted {
			t.Errorf("%q admitted; want blocked", c)
		}
	}
}

func TestApplyPolicyBlocksDirPrefix(t *testing.T) {
	p := DefaultPolicy()
	cases := []string{".ssh/id_rsa", ".aws/credentials", "node_modules/foo/index.js", ".git/HEAD"}
	for _, c := range cases {
		d, err := ApplyPolicy([]string{c}, &p)
		if err != nil {
			t.Fatalf("ApplyPolicy(%q): %v", c, err)
		}
		if d[0].Admitted {
			t.Errorf("%q admitted; want blocked", c)
		}
		if !strings.HasPrefix(d[0].Reason, "dir-prefix:") {
			t.Errorf("%q reason = %q, want dir-prefix:*", c, d[0].Reason)
		}
	}
}

func TestApplyPolicyBlocksDirPrefixAtAnyDepth(t *testing.T) {
	p := DefaultPolicy()
	cases := []string{"vendor/foo/.git/HEAD", "subproject/.aws/credentials"}
	for _, c := range cases {
		d, err := ApplyPolicy([]string{c}, &p)
		if err != nil {
			t.Fatalf("ApplyPolicy(%q): %v", c, err)
		}
		if d[0].Admitted {
			t.Errorf("%q admitted; want blocked at any depth", c)
		}
	}
}

func TestApplyPolicyBlocksSecretSuggestiveBasename(t *testing.T) {
	p := DefaultPolicy()
	cases := []string{
		"oauth-tokens.json",
		"api_key.json",
		"my-secret-config.yml",
		"PRIVATE_KEY.pub",
	}
	for _, c := range cases {
		d, err := ApplyPolicy([]string{c}, &p)
		if err != nil {
			t.Fatalf("ApplyPolicy(%q): %v", c, err)
		}
		if d[0].Admitted {
			t.Errorf("%q admitted; want blocked by secret-suggestive name", c)
		}
	}
}

func TestApplyPolicyAllowSecretSuggestiveBasenamesEscapeHatch(t *testing.T) {
	p := DefaultPolicy()
	p.AllowSecretSuggestiveBasenames = true
	d, err := ApplyPolicy([]string{"docs/api-secrets.md"}, &p)
	if err != nil {
		t.Fatalf("ApplyPolicy: %v", err)
	}
	if !d[0].Admitted {
		t.Error("AllowSecretSuggestiveBasenames=true should bypass the regex probe")
	}
}

func TestApplyPolicyUserExcludePatterns(t *testing.T) {
	p := DefaultPolicy()
	p.ExcludePatterns = append(p.ExcludePatterns, "*.proto")
	d, err := ApplyPolicy([]string{"api/types.proto", "api/types.go"}, &p)
	if err != nil {
		t.Fatalf("ApplyPolicy: %v", err)
	}
	if d[0].Admitted {
		t.Error("user pattern *.proto did not block types.proto")
	}
	if !d[1].Admitted {
		t.Error("user pattern *.proto over-blocked types.go")
	}
}

func TestApplyPolicyUserExcludeDirPrefix(t *testing.T) {
	p := DefaultPolicy()
	p.ExcludeDirPrefixes = append(p.ExcludeDirPrefixes, "internal-only/")
	d, err := ApplyPolicy([]string{"internal-only/secrets.json", "public/foo.ts"}, &p)
	if err != nil {
		t.Fatalf("ApplyPolicy: %v", err)
	}
	if d[0].Admitted {
		t.Error("user dir prefix did not block internal-only/")
	}
	if !d[1].Admitted {
		t.Error("user dir prefix over-blocked public/")
	}
}

func TestApplyPolicyDecisionOrderPreserved(t *testing.T) {
	p := DefaultPolicy()
	in := []string{"a.ts", ".env", "b.ts", ".aws/c"}
	d, err := ApplyPolicy(in, &p)
	if err != nil {
		t.Fatalf("ApplyPolicy: %v", err)
	}
	wantPaths := []string{"a.ts", ".env", "b.ts", ".aws/c"}
	for i, w := range wantPaths {
		if d[i].Path != w {
			t.Errorf("d[%d].Path = %q, want %q", i, d[i].Path, w)
		}
	}
}

func TestApplyPolicyPosixNormalizes(t *testing.T) {
	p := DefaultPolicy()
	// Backslash input gets normalized; .env is still blocked.
	d, err := ApplyPolicy([]string{"sub\\dir\\.env"}, &p)
	if err != nil {
		t.Fatalf("ApplyPolicy: %v", err)
	}
	if d[0].Admitted {
		t.Errorf(".env on backslash path admitted: %v", d[0])
	}
	if strings.Contains(d[0].Path, "\\") {
		t.Errorf("Path not POSIX-normalized: %q", d[0].Path)
	}
}

func TestPartitionDecisions(t *testing.T) {
	decisions := []PolicyDecision{
		{Path: "a.ts", Admitted: true},
		{Path: ".env", Admitted: false, Reason: "pattern:.env"},
		{Path: "b.ts", Admitted: true},
	}
	admitted, excluded := PartitionDecisions(decisions)
	if len(admitted) != 2 || admitted[0] != "a.ts" || admitted[1] != "b.ts" {
		t.Errorf("admitted = %v", admitted)
	}
	if len(excluded) != 1 || excluded[0] != ".env [pattern:.env]" {
		t.Errorf("excluded = %v", excluded)
	}
}

func TestPartitionDecisionsEmpty(t *testing.T) {
	admitted, excluded := PartitionDecisions(nil)
	if admitted == nil || excluded == nil {
		t.Errorf("admitted=%v excluded=%v should be non-nil", admitted, excluded)
	}
	if len(admitted) != 0 || len(excluded) != 0 {
		t.Errorf("non-empty: admitted=%v excluded=%v", admitted, excluded)
	}
}

func TestDefaultPolicyIsolation(t *testing.T) {
	a := DefaultPolicy()
	b := DefaultPolicy()
	a.ExcludePatterns = append(a.ExcludePatterns, "extra-pattern")
	if len(a.ExcludePatterns) == len(b.ExcludePatterns) {
		t.Error("DefaultPolicy returned a shared slice; mutating a affects b")
	}
}
