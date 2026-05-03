package jobs

import (
	"context"
	"fmt"
	"sync"
)

type Resource string

const (
	ResourceLLMCalls             Resource = "llm_calls"
	ResourceGitOpsPerProject     Resource = "git_ops_per_project"
	ResourceBROpsPerProject      Resource = "br_ops_per_project"
	ResourceHealthRunsPerProject Resource = "health_runs_per_project"
	ResourceSwarmSpawnsGlobal    Resource = "swarm_spawns_global"
	ResourceTerminalStreams      Resource = "terminal_streams_per_client"
)

type ResourceLimiter struct {
	mu     sync.RWMutex
	sems   map[Resource]chan struct{}
	limits map[Resource]int
}

type ResourceLease struct {
	once sync.Once
	ch   chan struct{}
}

func NewResourceLimiter(limits map[Resource]int) (*ResourceLimiter, error) {
	if len(limits) == 0 {
		return nil, fmt.Errorf("%w: no resource limits", ErrInvalidRequest)
	}
	sems := make(map[Resource]chan struct{}, len(limits))
	copied := make(map[Resource]int, len(limits))
	for resource, limit := range limits {
		if resource == "" || limit <= 0 {
			return nil, fmt.Errorf("%w: invalid resource limit for %q", ErrInvalidRequest, resource)
		}
		sems[resource] = make(chan struct{}, limit)
		copied[resource] = limit
	}
	return &ResourceLimiter{sems: sems, limits: copied}, nil
}

func (l *ResourceLimiter) Acquire(ctx context.Context, resource Resource) (*ResourceLease, error) {
	l.mu.RLock()
	ch, ok := l.sems[resource]
	l.mu.RUnlock()
	if !ok {
		return nil, ErrResourceNotConfigured
	}
	select {
	case ch <- struct{}{}:
		return &ResourceLease{ch: ch}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *ResourceLimiter) Limit(resource Resource) (int, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	limit, ok := l.limits[resource]
	return limit, ok
}

func (l *ResourceLimiter) InUse(resource Resource) int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	ch, ok := l.sems[resource]
	if !ok {
		return 0
	}
	return len(ch)
}

func (lease *ResourceLease) Release() {
	if lease == nil || lease.ch == nil {
		return
	}
	lease.once.Do(func() {
		<-lease.ch
	})
}
