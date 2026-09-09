// Package reliability defines storage and admission-control contracts for production deployments.
package reliability

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cortexgo/cortexgo/internal/provider"
)

// SQLStore and RedisStore are deliberately small adapters; applications provide their drivers.
type SQLStore interface {
	ExecContext(context.Context, string, ...any) error
	QueryContext(context.Context, string, ...any) (Rows, error)
}
type Rows interface {
	Close() error
	Next() bool
	Scan(...any) error
}
type RedisStore interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string, time.Duration) error
	Incr(context.Context, string) (int64, error)
}

type Quota struct {
	RequestsPerMinute int
	MaxTokens         int
	MaxCostUSD        float64
}
type Usage struct {
	Requests int
	Tokens   int
	CostUSD  float64
}

type Budget struct {
	mu     sync.Mutex
	limits map[string]Quota
	usage  map[string]Usage
	window time.Time
}

func NewBudget(limits map[string]Quota) *Budget {
	return &Budget{limits: limits, usage: make(map[string]Usage), window: time.Now().UTC()}
}
func (b *Budget) Allow(tenant string, tokens int, cost float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now().UTC()
	if now.Sub(b.window) >= time.Minute {
		b.usage = make(map[string]Usage)
		b.window = now
	}
	q, ok := b.limits[tenant]
	if !ok {
		return nil
	}
	u := b.usage[tenant]
	if q.RequestsPerMinute > 0 && u.Requests >= q.RequestsPerMinute {
		return fmt.Errorf("reliability: tenant %q request quota exceeded", tenant)
	}
	if q.MaxTokens > 0 && u.Tokens+tokens > q.MaxTokens {
		return fmt.Errorf("reliability: tenant %q token budget exceeded", tenant)
	}
	if q.MaxCostUSD > 0 && u.CostUSD+cost > q.MaxCostUSD {
		return fmt.Errorf("reliability: tenant %q cost budget exceeded", tenant)
	}
	b.usage[tenant] = Usage{Requests: u.Requests + 1, Tokens: u.Tokens + tokens, CostUSD: u.CostUSD + cost}
	return nil
}

type Concurrency struct{ sem chan struct{} }

func NewConcurrency(limit int) *Concurrency {
	if limit < 1 {
		limit = 1
	}
	return &Concurrency{sem: make(chan struct{}, limit)}
}
func (c *Concurrency) Acquire(ctx context.Context) error {
	select {
	case c.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *Concurrency) Release() {
	select {
	case <-c.sem:
	default:
	}
}

func BackupJSON(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reliability: backup read: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
func RestoreJSON(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reliability: restore read: %w", err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("reliability: invalid backup: %w", err)
	}
	return os.WriteFile(dst, data, 0o600)
}

func Cost(usage provider.Usage, promptPer1K, completionPer1K float64) float64 {
	return float64(usage.PromptTokens)/1000*promptPer1K + float64(usage.CompletionTokens)/1000*completionPer1K
}
