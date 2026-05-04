// Package vps exposes read-only daemon RPCs over the VPS working tree.
// These reads are the WIP overlay for the desktop's origin-synced local clone.
package vps

import (
	"time"

	gitadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/git"
)

const SchemaVersion = 1

type DirtyCounts struct {
	Files     int `json:"files"`
	Staged    int `json:"staged"`
	Unstaged  int `json:"unstaged"`
	Untracked int `json:"untracked"`
	Deleted   int `json:"deleted"`
	Renamed   int `json:"renamed"`
}

type WorkingTreeFile struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitempty"`
	XY        string `json:"xy"`
	Staged    bool   `json:"staged"`
	Unstaged  bool   `json:"unstaged"`
	Untracked bool   `json:"untracked"`
	Deleted   bool   `json:"deleted"`
	Renamed   bool   `json:"renamed"`
}

type WorkingTreeStatusResponse struct {
	SchemaVersion int               `json:"schemaVersion"`
	ProjectID     string            `json:"projectId"`
	RepoPath      string            `json:"repoPath"`
	Branch        string            `json:"branch"`
	Upstream      string            `json:"upstream,omitempty"`
	HeadSHA       string            `json:"headSha"`
	AheadBy       int               `json:"aheadBy"`
	BehindBy      int               `json:"behindBy"`
	Detached      bool              `json:"detached"`
	Clean         bool              `json:"clean"`
	DirtyCounts   DirtyCounts       `json:"dirtyCounts"`
	Files         []WorkingTreeFile `json:"files"`
	CheckedAt     time.Time         `json:"checkedAt"`
}

type DiffKind string

const (
	DiffKindStaged   DiffKind = "staged"
	DiffKindUnstaged DiffKind = "unstaged"
)

type DiffPage struct {
	StartLine int
	Limit     int
}

type DiffResponse struct {
	SchemaVersion int       `json:"schemaVersion"`
	ProjectID     string    `json:"projectId"`
	RepoPath      string    `json:"repoPath"`
	Kind          DiffKind  `json:"kind"`
	Branch        string    `json:"branch"`
	HeadSHA       string    `json:"headSha"`
	CacheKey      string    `json:"cacheKey"`
	Cached        bool      `json:"cached"`
	StartLine     int       `json:"startLine"`
	Limit         int       `json:"limit"`
	TotalLines    int       `json:"totalLines"`
	HasMore       bool      `json:"hasMore"`
	Diff          string    `json:"diff"`
	CheckedAt     time.Time `json:"checkedAt"`
}

type CommitRef struct {
	SHA      string `json:"sha"`
	ShortSHA string `json:"shortSha,omitempty"`
}

type UnpushedCommitsResponse struct {
	SchemaVersion int         `json:"schemaVersion"`
	ProjectID     string      `json:"projectId"`
	RepoPath      string      `json:"repoPath"`
	Branch        string      `json:"branch"`
	FromRef       string      `json:"fromRef"`
	ToRef         string      `json:"toRef"`
	Commits       []CommitRef `json:"commits"`
	CheckedAt     time.Time   `json:"checkedAt"`
}

type OpenFilesResponse struct {
	SchemaVersion int               `json:"schemaVersion"`
	ProjectID     string            `json:"projectId"`
	RepoPath      string            `json:"repoPath"`
	HeadSHA       string            `json:"headSha"`
	Files         []WorkingTreeFile `json:"files"`
	CheckedAt     time.Time         `json:"checkedAt"`
}

func statusFiles(status *gitadapter.Status) ([]WorkingTreeFile, DirtyCounts) {
	if status == nil {
		return []WorkingTreeFile{}, DirtyCounts{}
	}
	files := make([]WorkingTreeFile, 0, len(status.Entries))
	counts := DirtyCounts{Files: len(status.Entries)}
	for _, entry := range status.Entries {
		file := workingTreeFile(entry)
		if file.Staged {
			counts.Staged++
		}
		if file.Unstaged {
			counts.Unstaged++
		}
		if file.Untracked {
			counts.Untracked++
		}
		if file.Deleted {
			counts.Deleted++
		}
		if file.Renamed {
			counts.Renamed++
		}
		files = append(files, file)
	}
	return files, counts
}

func workingTreeFile(entry gitadapter.StatusEntry) WorkingTreeFile {
	x := statusByte(entry.XY, 0)
	y := statusByte(entry.XY, 1)
	untracked := x == '?' && y == '?'
	return WorkingTreeFile{
		Path:      entry.Path,
		OldPath:   entry.OldPath,
		XY:        entry.XY,
		Staged:    !untracked && x != ' ' && x != 0,
		Unstaged:  untracked || y != ' ' && y != 0,
		Untracked: untracked,
		Deleted:   x == 'D' || y == 'D',
		Renamed:   x == 'R' || y == 'R',
	}
}

func statusByte(xy string, idx int) byte {
	if len(xy) <= idx {
		return 0
	}
	return xy[idx]
}
