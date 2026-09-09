package reliability

import (
	"context"
	"testing"
	"time"
)

func TestBudgetAndConcurrency(t *testing.T) {
	b := NewBudget(map[string]Quota{"t": {RequestsPerMinute: 1, MaxTokens: 10, MaxCostUSD: 1}})
	if err := b.Allow("t", 5, .5); err != nil {
		t.Fatal(err)
	}
	if err := b.Allow("t", 1, .1); err == nil {
		t.Fatal("expected quota error")
	}
	c := NewConcurrency(1)
	if err := c.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		done <- c.Acquire(ctx)
	}()
	if err := <-done; err == nil {
		t.Fatal("expected backpressure")
	}
	c.Release()
}
