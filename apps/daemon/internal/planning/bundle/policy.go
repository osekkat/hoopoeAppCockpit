// policy.go — model-context policy enforcement for the bundle
// assembly (hp-rsly twelfth slice — §5.5 privacy / secret-scan layer).
//
// `ApplyPolicy` filters a list of project-root-relative paths against
// the §5.5 model-context-policy rules:
//
//   - Static path-glob exclusions (.env, credentials, .ssh/, etc.).
//   - User-configured glob patterns from the project's manifest
//     (`.hoopoe/policy.yaml`).
//   - Secret-scan probes by basename + extension (a real content-
//     based secret scan is the redaction-layer's job — at this
//     layer we filter paths with secret-suggestive names so the
//     bundle never includes "secrets.json" or "private-key.pem").
//
// The function is purely path-based + side-effect-free. The next
// integration slice plugs ApplyPolicy into the assembly orchestrator
// between WalkProjectRoot and EnforceBudget so a path that would
// reach the model gets one final policy gate.
//
// What this slice does NOT do (separate hp-rsly residual):
//
//   - Content-based secret detection (the daemon's redaction layer
//     handles it; this slice only blocks suggestive *paths*).
//   - Reading `.hoopoe/policy.yaml` from disk (the orchestrator
//     parses the file and passes globs as `Policy.UserExcludes`).
//   - Surfacing the "manage what models see" UI link (renderer-
//     side, post-bundle).

package bundle

import (
	"errors"
	"path"
	"regexp"
	"strings"
)

// DefaultExcludePatterns lists the path-glob exclusions every
// bundle assembly applies regardless of project configuration.
// These are the §5.5 "always-blocked" surfaces: secrets,
// credentials, infrastructure keys, anything Hoopoe should not
// send to a model under any model-context policy.
//
// Patterns use go's `path.Match` glob syntax: `*` matches any
// sequence within a path component, `**` is NOT supported (use a
// secondary deepGlobMatch helper for that).
var DefaultExcludePatterns = []string{
	".env",
	".env.*",
	"*.env",
	".env.local",
	".env.production",
	".envrc",
	"credentials.json",
	"secrets.json",
	"secrets.yml",
	"secrets.yaml",
	"private-key.pem",
	"private_key.pem",
	"id_rsa",
	"id_ed25519",
	"id_dsa",
	"id_ecdsa",
	"*.pem",
	"*.key",
	"*.p12",
	"*.pfx",
}

// DefaultExcludeDirPrefixes lists path prefixes the policy
// excludes recursively. Stored as prefix strings (not globs)
// because directory-level exclusion is the natural shape: an
// `.ssh/` parent excludes everything under it.
var DefaultExcludeDirPrefixes = []string{
	".ssh/",
	".aws/",
	".gnupg/",
	".gcloud/",
	".kube/",
	".hoopoe/secrets/",
	"node_modules/",
	".git/",
	".terraform/",
	".vagrant/",
}

// secretSuggestiveBasename matches basenames whose name strongly
// implies the file holds a secret — even if the extension isn't on
// the static blocklist. Used as a final probe so a fixture-named
// file (`oauth-tokens.json`) gets caught.
// Match keyword as a substring (no word-boundary anchors) — paths
// like `oauth-tokens.json` or `my_secret.yml` should match. The
// rule is intentionally conservative: this layer is a path-name
// guard, not a content scanner; overmatches here just push more
// files through the explicit-allow review.
var secretSuggestiveBasename = regexp.MustCompile(`(?i)(secret|token|credential|password|apikey|api_key|access_key|private[-_ ]?key)`)

// ErrInvalidPolicy is returned when ApplyPolicy is called with a
// nil Policy struct — the call site should pass DefaultPolicy()
// when no per-project overrides exist.
var ErrInvalidPolicy = errors.New("planning/bundle: nil policy")

// Policy captures the inputs ApplyPolicy needs. Defaults live in
// `DefaultPolicy()`; per-project overrides come from the project's
// `.hoopoe/policy.yaml` (parsed by the orchestrator before calling
// this layer).
type Policy struct {
	// ExcludePatterns extends DefaultExcludePatterns. Patterns use
	// the same `path.Match` glob syntax. Match is performed against
	// the basename only — for prefix matches, use ExcludeDirPrefixes.
	ExcludePatterns []string

	// ExcludeDirPrefixes extends DefaultExcludeDirPrefixes. Each
	// entry is a project-root-relative directory prefix; any path
	// that has this as a prefix (after POSIX normalization) is
	// excluded.
	ExcludeDirPrefixes []string

	// AllowSecretSuggestiveBasenames disables the
	// secretSuggestiveBasename regex probe. Defaults to false; set
	// to true only when the project explicitly opts out (rare —
	// the redaction-layer is a stronger control).
	AllowSecretSuggestiveBasenames bool
}

// DefaultPolicy returns the §5.5 baseline. The returned struct is
// safe to mutate — every field is freshly allocated.
func DefaultPolicy() Policy {
	patterns := make([]string, len(DefaultExcludePatterns))
	copy(patterns, DefaultExcludePatterns)
	prefixes := make([]string, len(DefaultExcludeDirPrefixes))
	copy(prefixes, DefaultExcludeDirPrefixes)
	return Policy{
		ExcludePatterns:    patterns,
		ExcludeDirPrefixes: prefixes,
	}
}

// PolicyDecision records why a path was admitted or excluded. The
// orchestrator surfaces excluded paths in the bundle's `Excluded`
// field; the reason string is stable + human-readable so the §7.1
// UI artifact rail can render "blocked: matches `*.pem`" instead of
// a generic "filtered."
type PolicyDecision struct {
	Path       string
	Admitted   bool
	Reason     string
}

// ApplyPolicy filters `paths` through the policy rules and returns
// per-path decisions in the same order. The orchestrator partitions
// the result: admitted paths reach the bundle; excluded paths feed
// the bundle's `Excluded` field with a stable reason.
//
// `paths` should already be POSIX-normalized (the upstream walk
// guarantees this); ApplyPolicy applies an extra normalization for
// safety.
func ApplyPolicy(paths []string, policy *Policy) ([]PolicyDecision, error) {
	if policy == nil {
		return nil, ErrInvalidPolicy
	}
	out := make([]PolicyDecision, 0, len(paths))
	for _, p := range paths {
		out = append(out, decideOne(p, policy))
	}
	return out, nil
}

func decideOne(p string, policy *Policy) PolicyDecision {
	posix := strictPosixPath(p)

	for _, prefix := range policy.ExcludeDirPrefixes {
		if strings.HasPrefix(posix, prefix) {
			return PolicyDecision{Path: posix, Admitted: false, Reason: "dir-prefix:" + prefix}
		}
		// Also match where the directory is at any depth — only when
		// the prefix doesn't already start with "./" or "/" so we
		// don't double-match.
		needle := "/" + prefix
		if strings.Contains(posix, needle) {
			return PolicyDecision{Path: posix, Admitted: false, Reason: "dir-prefix:" + prefix}
		}
	}

	base := path.Base(posix)
	for _, pat := range policy.ExcludePatterns {
		matched, err := path.Match(pat, base)
		if err == nil && matched {
			return PolicyDecision{Path: posix, Admitted: false, Reason: "pattern:" + pat}
		}
	}

	if !policy.AllowSecretSuggestiveBasenames && secretSuggestiveBasename.MatchString(base) {
		return PolicyDecision{Path: posix, Admitted: false, Reason: "secret-suggestive-basename"}
	}

	return PolicyDecision{Path: posix, Admitted: true, Reason: ""}
}

// PartitionDecisions splits decisions into admitted-paths and a
// `[]string` of `path:reason` excluded-markers. Convenience for
// the orchestrator wiring.
func PartitionDecisions(decisions []PolicyDecision) (admitted []string, excluded []string) {
	admitted = []string{}
	excluded = []string{}
	for _, d := range decisions {
		if d.Admitted {
			admitted = append(admitted, d.Path)
		} else {
			excluded = append(excluded, d.Path+" ["+d.Reason+"]")
		}
	}
	return admitted, excluded
}
