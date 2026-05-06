package redaction

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"testing"
)

type redactionFuzzCase struct {
	id    string
	build func(string) (string, []string)
}

func FuzzRedactPatterns(f *testing.F) {
	cases := redactionFuzzCases()
	for _, testCase := range cases {
		f.Add(testCase.id, "seed")
	}

	f.Fuzz(func(t *testing.T, patternID string, entropy string) {
		testCase := fuzzCaseFor(patternID, cases)
		payload, leaks := testCase.build(entropy)
		out, traces := NewDefault().RedactText(SurfaceAudit, "fuzz."+testCase.id, payload)

		if len(traces) == 0 {
			t.Fatalf("%s: no redaction traces for payload %q", testCase.id, payload)
		}
		if !traceContains(traces, testCase.id) {
			t.Fatalf("%s: expected trace id missing from %#v", testCase.id, traces)
		}
		for _, leak := range leaks {
			if leak != "" && strings.Contains(out, leak) {
				t.Fatalf("%s: redacted output leaked %q in %q", testCase.id, leak, out)
			}
		}
	})
}

// FuzzRedactValueNested exercises the recursive RedactValue path, which
// FuzzRedactPatterns does not — RedactText only walks a single string,
// but real audit/event payloads are nested maps and arrays. The wrapper
// also covers RedactAdapterOutput transitively (it is RedactValue with
// a "adapter:<name>" surface label).
func FuzzRedactValueNested(f *testing.F) {
	cases := redactionFuzzCases()
	for _, testCase := range cases {
		f.Add(testCase.id, "seed")
	}

	f.Fuzz(func(t *testing.T, patternID string, entropy string) {
		testCase := fuzzCaseFor(patternID, cases)
		payload, leaks := testCase.build(entropy)
		nested := map[string]any{
			"outer": map[string]any{
				"inner": payload,
				"arr":   []any{"safe", payload},
			},
		}
		out, traces := NewDefault().RedactValue(SurfaceEvents, "event", nested)
		if len(traces) == 0 {
			t.Fatalf("%s: no redaction traces for nested payload", testCase.id)
		}
		if !traceContains(traces, testCase.id) {
			t.Fatalf("%s: expected trace id missing from nested %#v", testCase.id, traces)
		}
		rendered := flattenForFuzz(out)
		for _, leak := range leaks {
			if leak != "" && strings.Contains(rendered, leak) {
				t.Fatalf("%s: nested redacted output leaked %q in %q", testCase.id, leak, rendered)
			}
		}
	})
}

func flattenForFuzz(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any:
		var b strings.Builder
		for key, child := range v {
			b.WriteString(key)
			b.WriteString("=")
			b.WriteString(flattenForFuzz(child))
			b.WriteString(";")
		}
		return b.String()
	case []any:
		var b strings.Builder
		for _, child := range v {
			b.WriteString(flattenForFuzz(child))
			b.WriteString(";")
		}
		return b.String()
	default:
		return ""
	}
}

func TestRedactionFuzzCasesCoverDefaultPatterns(t *testing.T) {
	got := make([]string, 0, len(redactionFuzzCases()))
	for _, testCase := range redactionFuzzCases() {
		got = append(got, testCase.id)
	}
	sort.Strings(got)

	want := make([]string, 0, len(defaultPatterns()))
	for _, pattern := range defaultPatterns() {
		want = append(want, pattern.id)
	}
	sort.Strings(want)

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("fuzz cases do not cover default patterns\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func redactionFuzzCases() []redactionFuzzCase {
	return []redactionFuzzCase{
		{
			id: "http-header-authorization",
			build: func(entropy string) (string, []string) {
				token := alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._~+/-", 36)
				return "GET /v1/jobs\nAuthorization: Bearer " + token + "\n", []string{token}
			},
		},
		{
			id: "http-header-cookie",
			build: func(entropy string) (string, []string) {
				secret := alphabet(entropy, "abcdefghijklmnopqrstuvwxyz0123456789", 28)
				return "Cookie: session=" + secret + "; theme=dark", []string{secret}
			},
		},
		{
			id: "http-header-set-cookie",
			build: func(entropy string) (string, []string) {
				secret := alphabet(entropy, "abcdefghijklmnopqrstuvwxyz0123456789", 28)
				return "Set-Cookie: session=" + secret + "; Path=/", []string{secret}
			},
		},
		{
			id: "private-key-block",
			build: func(entropy string) (string, []string) {
				secret := "fuzz-private-key-" + alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", 32)
				return "-----BEGIN OPENSSH PRIVATE KEY-----\n" + secret + "\n-----END OPENSSH PRIVATE KEY-----", []string{secret}
			},
		},
		{
			id: "bearer-hmac",
			build: func(entropy string) (string, []string) {
				token := "eyJ" + alphabet(entropy, base64URLAlphabet, 20) + "." + alphabet(entropy+"payload", base64URLAlphabet, 24) + "." + trailingWord(alphabet(entropy+"sig", base64URLAlphabet, 24))
				return "token=" + token, []string{token}
			},
		},
		{
			id: "bearer-token",
			build: func(entropy string) (string, []string) {
				token := trailingWord(alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._~+/-", 36))
				return "Bearer " + token, []string{token}
			},
		},
		{
			id: "provider-key-anthropic",
			build: func(entropy string) (string, []string) {
				key := "sk-ant-" + trailingWord(alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-", 32))
				return "ANTHROPIC_SESSION=" + key, []string{key}
			},
		},
		{
			id: "provider-key-sk",
			build: func(entropy string) (string, []string) {
				key := "sk-" + trailingWord(alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-", 32))
				return "compat_provider_token=" + key, []string{key}
			},
		},
		{
			id: "provider-key-google",
			build: func(entropy string) (string, []string) {
				key := "AIza" + trailingWord(alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-", 35))
				return "google=" + key, []string{key}
			},
		},
		{
			id: "provider-key-aws",
			build: func(entropy string) (string, []string) {
				key := "AKIA" + alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", 16)
				return "aws_access_key_id=" + key, []string{key}
			},
		},
		{
			id: "provider-key-github",
			build: func(entropy string) (string, []string) {
				key := "ghp_" + alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", 40)
				return "github_token=" + key, []string{key}
			},
		},
		{
			id: "provider-key-slack",
			build: func(entropy string) (string, []string) {
				key := "xoxb-" + trailingWord(alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-", 36))
				return "slack=" + key, []string{key}
			},
		},
		{
			id: "pairing-token",
			build: func(entropy string) (string, []string) {
				token := "H-" + alphabet(entropy, "ABCDEFGHJKMNPQRSTVWXYZ0123456789", 11)
				return "pairing=" + token, []string{token}
			},
		},
		{
			id: "ssh-passphrase",
			build: func(entropy string) (string, []string) {
				secret := "pw-" + alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-", 24)
				return "passphrase=" + secret, []string{secret}
			},
		},
		{
			id: "browser-cookie-chatgpt",
			build: func(entropy string) (string, []string) {
				value := alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-", 32)
				return "__Secure-next-auth.session-token=" + value, []string{value}
			},
		},
		{
			id: "browser-cookie-next-auth-csrf",
			build: func(entropy string) (string, []string) {
				value := alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._%-", 32)
				return "__Host-next-auth.csrf-token=" + value, []string{value}
			},
		},
		{
			id: "browser-cookie-claude",
			build: func(entropy string) (string, []string) {
				value := alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-", 32)
				return "claude_session=" + value, []string{value}
			},
		},
		{
			id: "browser-cookie-claude-sessionkey",
			build: func(entropy string) (string, []string) {
				value := "ck." + alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._%+/-", 24)
				return "sessionKey=" + value, []string{value}
			},
		},
		{
			id: "browser-cookie-oai",
			build: func(entropy string) (string, []string) {
				value := alphabet(entropy, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-", 32)
				return "oai_session=" + value, []string{value}
			},
		},
		{
			id: "telegram-bot-token",
			build: func(entropy string) (string, []string) {
				token := alphabet(entropy, "0123456789", 9) + ":" + trailingWord(alphabet(entropy+"bot", "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-", 36))
				return "telegram=" + token, []string{token}
			},
		},
		{
			id: "email-address",
			build: func(entropy string) (string, []string) {
				local := alphabet(entropy, "abcdefghijklmnopqrstuvwxyz0123456789", 12)
				email := local + "@example.com"
				return "notify " + email, []string{email}
			},
		},
		{
			id: "ssh-key-path",
			build: func(entropy string) (string, []string) {
				path := "~/.ssh/id_" + alphabet(entropy, "abcdefghijklmnopqrstuvwxyz0123456789", 12)
				return "ssh key at " + path, []string{path, ".ssh/id_"}
			},
		},
		{
			id: "shadow-file-path",
			build: func(string) (string, []string) {
				return "read /etc/shadow before diagnostics", []string{"/etc/shadow"}
			},
		},
		{
			id: "macos-private-db-path",
			build: func(entropy string) (string, []string) {
				path := "/private/var/db/" + alphabet(entropy, "abcdefghijklmnopqrstuvwxyz0123456789", 12) + "/secret.db"
				return path, []string{path, "/private/var/db/"}
			},
		},
		{
			id: "oracle-profile-path",
			build: func(entropy string) (string, []string) {
				user := alphabet(entropy, "abcdefghijklmnopqrstuvwxyz", 8)
				path := "/home/" + user + "/.config/oracle/profile.json"
				return path, []string{path, ".config/oracle"}
			},
		},
		{
			id: "user-home-path",
			build: func(entropy string) (string, []string) {
				user := alphabet(entropy, "abcdefghijklmnopqrstuvwxyz", 8)
				path := "/home/" + user + "/Projects/" + alphabet(entropy+"repo", "abcdefghijklmnopqrstuvwxyz0123456789", 10) + "/.env"
				return path, []string{path, "/home/" + user + "/Projects"}
			},
		},
		{
			id: "vps-project-path",
			build: func(entropy string) (string, []string) {
				path := "/data/projects/" + alphabet(entropy, "abcdefghijklmnopqrstuvwxyz0123456789", 12) + "/.env"
				return path, []string{path, "/data/projects/"}
			},
		},
	}
}

func fuzzCaseFor(patternID string, cases []redactionFuzzCase) redactionFuzzCase {
	for _, testCase := range cases {
		if testCase.id == patternID {
			return testCase
		}
	}
	sum := sha256.Sum256([]byte(patternID))
	return cases[int(sum[0])%len(cases)]
}

func traceContains(traces []TraceEvent, patternID string) bool {
	for _, trace := range traces {
		if trace.PatternID == patternID {
			return true
		}
	}
	return false
}

const base64URLAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"

func alphabet(entropy string, chars string, n int) string {
	if entropy == "" {
		entropy = "seed"
	}
	sum := sha256.Sum256([]byte(entropy))
	hexed := hex.EncodeToString(sum[:])
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		source := entropy[i%len(entropy)] + hexed[i%len(hexed)] + byte(i*31)
		b.WriteByte(chars[int(source)%len(chars)])
	}
	return b.String()
}

func trailingWord(s string) string {
	if s == "" {
		return "A"
	}
	last := s[len(s)-1]
	if (last >= 'A' && last <= 'Z') || (last >= 'a' && last <= 'z') || (last >= '0' && last <= '9') || last == '_' {
		return s
	}
	return s[:len(s)-1] + "A"
}
