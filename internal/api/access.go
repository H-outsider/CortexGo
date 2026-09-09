package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

type tenantContextKey struct{}

func withTenant(ctx context.Context, t Tenant) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, t)
}
func TenantFromContext(ctx context.Context) (Tenant, bool) {
	t, ok := ctx.Value(tenantContextKey{}).(Tenant)
	return t, ok
}

type Tenant struct {
	ID    string
	Roles []string
}
type TenantResolver func(*http.Request) (Tenant, bool)
type Authorizer interface{ Allow(Tenant, string) bool }

// RBAC maps roles to permissions. Permissions are checked on every protected request.
type RBAC struct {
	mu    sync.RWMutex
	roles map[string]map[string]struct{}
}

func NewRBAC() *RBAC { return &RBAC{roles: make(map[string]map[string]struct{})} }
func (r *RBAC) Grant(role string, permissions ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.roles == nil {
		r.roles = make(map[string]map[string]struct{})
	}
	if r.roles[role] == nil {
		r.roles[role] = make(map[string]struct{})
	}
	for _, p := range permissions {
		r.roles[role][p] = struct{}{}
	}
}
func (r *RBAC) Allow(t Tenant, permission string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, role := range t.Roles {
		if _, ok := r.roles[role][permission]; ok {
			return true
		}
	}
	return false
}

// SlidingWindowLimiter limits each tenant independently.
type SlidingWindowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
}

func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	if limit < 1 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	return &SlidingWindowLimiter{limit: limit, window: window, hits: make(map[string][]time.Time)}
}
func (l *SlidingWindowLimiter) Allow(key string) (bool, time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	old := l.hits[key]
	i := 0
	for i < len(old) && old[i].Before(cutoff) {
		i++
	}
	old = old[i:]
	if len(old) >= l.limit {
		return false, old[0].Add(l.window).Sub(now)
	}
	l.hits[key] = append(old, now)
	return true, 0
}

func HeaderTenantResolver(r *http.Request) (Tenant, bool) {
	id := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if id == "" {
		return Tenant{}, false
	}
	return Tenant{ID: id}, true
}
