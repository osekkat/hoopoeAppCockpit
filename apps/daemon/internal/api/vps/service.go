package vps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	gitadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/git"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/projects"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/search"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const (
	DefaultDiffLimit = 500
	MaxDiffLimit     = 5000
	DefaultCacheTTL  = 5 * time.Second
)

var (
	ErrProjectResolverMissing = errors.New("vps: project resolver is not configured")
	ErrProjectRepoMissing     = errors.New("vps: project has no VPS clone path")
	ErrInvalidDiffKind        = errors.New("vps: invalid diff kind")
)

type ProjectResolver interface {
	Project(ctx context.Context, id string) (schemas.Project, error)
}

type GitClient interface {
	Status(ctx context.Context) (*gitadapter.Status, error)
	DiffStaged(ctx context.Context) ([]byte, error)
	DiffUnstaged(ctx context.Context) ([]byte, error)
	UnpushedCommits(ctx context.Context, branch string) (*gitadapter.CommitDelta, error)
	RevParse(ctx context.Context, ref string) (string, error)
}

type GitClientFactory func(repoPath string) GitClient

type Searcher interface {
	Search(context.Context, search.Request) (search.Response, error)
}

type Logger interface {
	Info(ctx context.Context, message string, fields map[string]any)
	Error(ctx context.Context, message string, fields map[string]any)
}

type Config struct {
	Projects         ProjectResolver
	GitClientFactory GitClientFactory
	Cache            *DiffCache
	Searcher         Searcher
	Logger           Logger
	Now              func() time.Time
}

type Service struct {
	projects ProjectResolver
	newGit   GitClientFactory
	cache    *DiffCache
	searcher Searcher
	logger   Logger
	now      func() time.Time
}

func NewService(cfg Config) *Service {
	newGit := cfg.GitClientFactory
	if newGit == nil {
		newGit = func(repoPath string) GitClient { return gitadapter.New(repoPath) }
	}
	cache := cfg.Cache
	if cache == nil {
		cache = NewDiffCache(DefaultCacheTTL)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	searcher := cfg.Searcher
	if searcher == nil {
		searcher = search.NewService(search.Config{Now: now})
	}
	return &Service{
		projects: cfg.Projects,
		newGit:   newGit,
		cache:    cache,
		searcher: searcher,
		logger:   cfg.Logger,
		now:      now,
	}
}

func (s *Service) WorkingTreeStatus(ctx context.Context, projectID string) (WorkingTreeStatusResponse, error) {
	project, repoPath, client, err := s.projectClient(ctx, projectID)
	if err != nil {
		return WorkingTreeStatusResponse{}, err
	}
	status, head, err := s.statusHead(ctx, client)
	if err != nil {
		return WorkingTreeStatusResponse{}, err
	}
	files, counts := statusFiles(status)
	s.auditRead(ctx, project.Id, "git.status")
	return WorkingTreeStatusResponse{
		SchemaVersion: SchemaVersion,
		ProjectID:     project.Id,
		RepoPath:      repoPath,
		Branch:        status.Branch,
		Upstream:      status.Upstream,
		HeadSHA:       head,
		AheadBy:       status.AheadBy,
		BehindBy:      status.BehindBy,
		Detached:      status.Detached,
		Clean:         status.Clean,
		DirtyCounts:   counts,
		Files:         files,
		CheckedAt:     s.now().UTC(),
	}, nil
}

func (s *Service) Diff(ctx context.Context, projectID string, kind DiffKind, page DiffPage) (DiffResponse, error) {
	if kind != DiffKindStaged && kind != DiffKindUnstaged {
		return DiffResponse{}, ErrInvalidDiffKind
	}
	project, repoPath, client, err := s.projectClient(ctx, projectID)
	if err != nil {
		return DiffResponse{}, err
	}
	status, head, err := s.statusHead(ctx, client)
	if err != nil {
		return DiffResponse{}, err
	}
	key := s.diffCacheKey(repoPath, project.Id, kind, head, status)
	diff, cached, err := s.cachedDiff(ctx, client, kind, key)
	if err != nil {
		return DiffResponse{}, err
	}
	startLine, limit := normalizePage(page)
	paged, total, hasMore := paginateLines(string(diff), startLine, limit)
	s.auditRead(ctx, project.Id, "git."+string(kind)+"_diff")
	return DiffResponse{
		SchemaVersion: SchemaVersion,
		ProjectID:     project.Id,
		RepoPath:      repoPath,
		Kind:          kind,
		Branch:        status.Branch,
		HeadSHA:       head,
		CacheKey:      key,
		Cached:        cached,
		StartLine:     startLine,
		Limit:         limit,
		TotalLines:    total,
		HasMore:       hasMore,
		Diff:          paged,
		CheckedAt:     s.now().UTC(),
	}, nil
}

func (s *Service) UnpushedCommits(ctx context.Context, projectID string) (UnpushedCommitsResponse, error) {
	project, repoPath, client, err := s.projectClient(ctx, projectID)
	if err != nil {
		return UnpushedCommitsResponse{}, err
	}
	status, _, err := s.statusHead(ctx, client)
	if err != nil {
		return UnpushedCommitsResponse{}, err
	}
	branch := status.Branch
	if branch == "" {
		branch = "HEAD"
	}
	delta, err := client.UnpushedCommits(ctx, branch)
	if err != nil {
		return UnpushedCommitsResponse{}, err
	}
	commits := make([]CommitRef, 0, len(delta.Commits))
	for _, commit := range delta.Commits {
		commits = append(commits, CommitRef{SHA: commit.SHA, ShortSHA: commit.ShortSHA})
	}
	s.auditRead(ctx, project.Id, "git.unpushed_commits")
	return UnpushedCommitsResponse{
		SchemaVersion: SchemaVersion,
		ProjectID:     project.Id,
		RepoPath:      repoPath,
		Branch:        branch,
		FromRef:       delta.From,
		ToRef:         delta.To,
		Commits:       commits,
		CheckedAt:     s.now().UTC(),
	}, nil
}

func (s *Service) OpenFiles(ctx context.Context, projectID string) (OpenFilesResponse, error) {
	project, repoPath, client, err := s.projectClient(ctx, projectID)
	if err != nil {
		return OpenFilesResponse{}, err
	}
	status, head, err := s.statusHead(ctx, client)
	if err != nil {
		return OpenFilesResponse{}, err
	}
	files, _ := statusFiles(status)
	s.auditRead(ctx, project.Id, "git.open_files")
	return OpenFilesResponse{
		SchemaVersion: SchemaVersion,
		ProjectID:     project.Id,
		RepoPath:      repoPath,
		HeadSHA:       head,
		Files:         files,
		CheckedAt:     s.now().UTC(),
	}, nil
}

func (s *Service) Grep(ctx context.Context, projectID string, req search.Request) (search.Response, error) {
	project, repoPath, err := s.projectRepo(ctx, projectID)
	if err != nil {
		return search.Response{}, err
	}
	req.ProjectID = project.Id
	req.RepoPath = repoPath
	response, err := s.searcher.Search(ctx, req)
	if err != nil {
		return search.Response{}, err
	}
	s.auditRead(ctx, project.Id, "git.grep")
	return response, nil
}

func (s *Service) projectClient(ctx context.Context, projectID string) (schemas.Project, string, GitClient, error) {
	project, repoPath, err := s.projectRepo(ctx, projectID)
	if err != nil {
		return schemas.Project{}, "", nil, err
	}
	return project, repoPath, s.newGit(repoPath), nil
}

func (s *Service) projectRepo(ctx context.Context, projectID string) (schemas.Project, string, error) {
	if s.projects == nil {
		return schemas.Project{}, "", ErrProjectResolverMissing
	}
	project, err := s.projects.Project(ctx, strings.TrimSpace(projectID))
	if err != nil {
		return schemas.Project{}, "", err
	}
	repoPath := ""
	if project.Repo.VpsClonePath != nil {
		repoPath = strings.TrimSpace(*project.Repo.VpsClonePath)
	}
	if repoPath == "" {
		return schemas.Project{}, "", ErrProjectRepoMissing
	}
	return project, repoPath, nil
}

func (s *Service) statusHead(ctx context.Context, client GitClient) (*gitadapter.Status, string, error) {
	status, err := client.Status(ctx)
	if err != nil {
		return nil, "", err
	}
	head, err := client.RevParse(ctx, "HEAD")
	if err != nil {
		return nil, "", err
	}
	return status, head, nil
}

func (s *Service) cachedDiff(ctx context.Context, client GitClient, kind DiffKind, key string) ([]byte, bool, error) {
	if s.cache != nil {
		if diff, ok := s.cache.Get(key, s.now()); ok {
			return diff, true, nil
		}
	}
	var (
		diff []byte
		err  error
	)
	switch kind {
	case DiffKindStaged:
		diff, err = client.DiffStaged(ctx)
	case DiffKindUnstaged:
		diff, err = client.DiffUnstaged(ctx)
	default:
		return nil, false, ErrInvalidDiffKind
	}
	if err != nil {
		return nil, false, err
	}
	if s.cache != nil {
		s.cache.Set(key, diff, s.now())
	}
	return diff, false, nil
}

func (s *Service) diffCacheKey(repoPath string, projectID string, kind DiffKind, head string, status *gitadapter.Status) string {
	hash := sha256.New()
	writeHash(hash, projectID)
	writeHash(hash, string(kind))
	writeHash(hash, head)
	if status != nil {
		writeHash(hash, status.Branch)
		writeHash(hash, status.Upstream)
		for _, entry := range sortedStatusEntries(status.Entries) {
			writeHash(hash, entry.XY)
			writeHash(hash, entry.Path)
			writeHash(hash, entry.OldPath)
			statPath(hash, filepath.Join(repoPath, entry.Path))
			if entry.OldPath != "" {
				statPath(hash, filepath.Join(repoPath, entry.OldPath))
			}
		}
	}
	statPath(hash, filepath.Join(repoPath, ".git", "index"))
	return hex.EncodeToString(hash.Sum(nil))
}

func sortedStatusEntries(entries []gitadapter.StatusEntry) []gitadapter.StatusEntry {
	out := append([]gitadapter.StatusEntry(nil), entries...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].OldPath < out[j].OldPath
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func writeHash(hash interface{ Write([]byte) (int, error) }, value string) {
	_, _ = hash.Write([]byte(value))
	_, _ = hash.Write([]byte{0})
}

func statPath(hash interface{ Write([]byte) (int, error) }, path string) {
	info, err := os.Stat(path)
	if err != nil {
		writeHash(hash, "missing:"+path)
		return
	}
	writeHash(hash, fmt.Sprintf("%s:%d:%d", path, info.Size(), info.ModTime().UnixNano()))
}

func normalizePage(page DiffPage) (int, int) {
	startLine := page.StartLine
	if startLine <= 0 {
		startLine = 1
	}
	limit := page.Limit
	if limit <= 0 {
		limit = DefaultDiffLimit
	}
	if limit > MaxDiffLimit {
		limit = MaxDiffLimit
	}
	return startLine, limit
}

func paginateLines(body string, startLine int, limit int) (string, int, bool) {
	lines := splitLines(body)
	total := len(lines)
	start := startLine - 1
	if start >= total {
		return "", total, false
	}
	end := start + limit
	if end > total {
		end = total
	}
	return strings.Join(lines[start:end], ""), total, end < total
}

func splitLines(body string) []string {
	if body == "" {
		return nil
	}
	lines := strings.SplitAfter(body, "\n")
	if lines[len(lines)-1] == "" {
		return lines[:len(lines)-1]
	}
	return lines
}

func (s *Service) auditRead(ctx context.Context, projectID string, action string) {
	if s.logger == nil {
		return
	}
	s.logger.Info(ctx, "vps_wip_read", map[string]any{
		"projectId": projectID,
		"action":    action,
	})
}

func mapProjectError(err error) (int, string, string) {
	switch {
	case errors.Is(err, ErrProjectResolverMissing):
		return 501, "projects.unavailable", "project registry unavailable"
	case errors.Is(err, projects.ErrNotFound):
		return 404, "project.not_found", "project not found"
	case errors.Is(err, ErrProjectRepoMissing):
		return 422, "project.vps_clone_missing", "project has no VPS clone path"
	case errors.Is(err, ErrInvalidDiffKind):
		return 400, "git.diff.invalid_kind", "invalid diff kind"
	case errors.Is(err, search.ErrInvalidRequest):
		return 400, "search.invalid_request", "invalid search request"
	case errors.Is(err, search.ErrPathOutsideRoot):
		return 400, "search.path_outside_root", "search path outside project root"
	case errors.Is(err, search.ErrMalformedOutput):
		return 502, "search.malformed_output", "malformed ripgrep output"
	case errors.Is(err, search.ErrCommandFailed):
		return 502, "search.command_failed", "search command failed"
	default:
		return 502, "git.command_failed", "git command failed"
	}
}

type DiffCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]diffCacheEntry
}

type diffCacheEntry struct {
	diff      []byte
	expiresAt time.Time
}

func NewDiffCache(ttl time.Duration) *DiffCache {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &DiffCache{
		ttl:     ttl,
		entries: make(map[string]diffCacheEntry),
	}
}

func (c *DiffCache) Get(key string, now time.Time) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !entry.expiresAt.After(now) {
		if ok {
			delete(c.entries, key)
		}
		return nil, false
	}
	return append([]byte(nil), entry.diff...), true
}

func (c *DiffCache) Set(key string, diff []byte, now time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]diffCacheEntry)
	}
	c.entries[key] = diffCacheEntry{
		diff:      append([]byte(nil), diff...),
		expiresAt: now.Add(c.ttl),
	}
}
