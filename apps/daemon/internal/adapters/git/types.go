// Package git is the daemon-side adapter for per-project git plumbing per
// plan.md §2.3 + §5.3 (every project-level git op runs on the VPS via the
// daemon; the desktop never invokes project-level git directly).
//
// Wraps 13 commands:
//
//   - status            (porcelain v1)
//   - diff              (unstaged)
//   - diff --staged     (staged)
//   - log               (commit history)
//   - show              (single-commit detail)
//   - push              (audited; force-push gated through approvals queue)
//   - fetch             (read-only network sync)
//   - rev-list          (commits between refs — for unpushed-list)
//   - rev-parse         (resolve refs to SHAs)
//   - ls-tree           (file listing at a ref)
//   - blame             (per-line authorship)
//   - remote            (configured remotes)
//   - branch            (local + tracking branches)
//
// Output parsing follows the porcelain conventions:
//
//   - status uses `--porcelain=v1` (stable; v2 would be more
//     ergonomic but v1 is universally supported back to git 1.7).
//   - log uses `--pretty=format:` with NUL-separated fields so
//     commit messages with newlines parse cleanly.
//   - diff returns the unparsed unified-diff text (renderers + LLMs
//     consume diffs as text, not as a structured AST).
//
// Output may include user names + emails (commit authors). DO NOT
// redact those — they're public Git metadata. Secret-shaped strings
// inside commit messages (rare) flow through the daemon's hp-lxs
// log redactor at the audit boundary, not in this package.
package git

import "time"

// StatusEntry is one line of `git status --porcelain=v1`.
type StatusEntry struct {
	// XY is the two-character XY code from porcelain v1.
	// X = staged change; Y = unstaged change. e.g., "M ", " M",
	// "??" (untracked), "AM" (added then modified), "R " (renamed
	// staged), "DD" (both deleted), etc.
	XY string

	// Path is the working-tree path of the change.
	Path string

	// OldPath is set on rename / copy entries (XY[0] in {'R','C'}).
	OldPath string
}

// Status is the parsed result of `git status --porcelain=v1`.
type Status struct {
	// Branch is the current branch name (from the porcelain v1
	// `## branch...origin/branch [ahead 1, behind 2]` header line).
	Branch string

	// Upstream is the tracking branch (e.g., `origin/main`) when set.
	Upstream string

	// AheadBy / BehindBy reflect the porcelain header counts; -1 when
	// not present (no upstream).
	AheadBy  int
	BehindBy int

	// Detached is true when the working tree is detached HEAD.
	Detached bool

	// Entries holds one StatusEntry per modified/untracked file.
	Entries []StatusEntry

	// Clean reports whether Entries is empty and (AheadBy <= 0).
	// Convenience flag for the renderer.
	Clean bool
}

// Commit is the parsed result of one entry in `git log`.
type Commit struct {
	SHA       string
	ShortSHA  string
	AuthorName  string
	AuthorEmail string
	AuthoredAt  time.Time
	CommitterName  string
	CommitterEmail string
	CommittedAt    time.Time
	Subject     string
	Body        string
	ParentSHAs  []string
}

// Branch is one entry from `git branch -v --no-abbrev`.
type Branch struct {
	Name      string
	HeadSHA   string
	IsCurrent bool
	Subject   string // first line of the branch's HEAD commit subject
}

// Remote is one entry from `git remote -v`.
type Remote struct {
	Name     string
	FetchURL string
	PushURL  string
}

// PushResult is the parsed outcome of `git push`. The git binary's exit
// code is the source of truth for success/failure; this struct surfaces
// the human-readable summary lines for audit + UI display.
type PushResult struct {
	OK             bool
	RemoteRef      string
	OldSHA         string
	NewSHA         string
	Forced         bool
	Summary        string
	UpdatedRefs    []PushedRefUpdate
}

// PushedRefUpdate is one ref the push touched (typical case: 1 entry).
type PushedRefUpdate struct {
	Status     string // ' ' (fast-forward) | '+' (forced) | '*' (new) | '!' (rejected) | '-' (deleted)
	Summary    string // e.g., "abc1234..def5678"
	Source     string
	Destination string
	Reason     string // populated when Status == '!'
}

// FileBlame is the parsed result of `git blame --porcelain` for one file.
type FileBlame struct {
	Path  string
	Lines []BlameLine
}

// BlameLine is one line of attribution.
type BlameLine struct {
	SHA         string
	AuthorName  string
	AuthorEmail string
	AuthoredAt  time.Time
	LineNum     int
	Content     string
}

// LsTreeEntry is one entry from `git ls-tree -r <ref>`.
type LsTreeEntry struct {
	Mode string
	Type string // "blob" | "tree" | "commit"
	SHA  string
	Path string
}

// CommitDelta is the result of `git rev-list <local>..<remote>`-shaped
// queries — the commits that exist in the second ref but not the first.
type CommitDelta struct {
	From    string
	To      string
	Commits []Commit
}
