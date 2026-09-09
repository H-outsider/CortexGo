package agent

import (
	"context"
	"testing"

	"github.com/cortexgo/cortexgo/internal/memory"
	"github.com/cortexgo/cortexgo/internal/provider"
)

func BenchmarkEchoChat(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		a := New(provider.Echo{}, memory.NewInMemory())
		if _, err := a.Chat(ctx, "bench", "hello"); err != nil {
			b.Fatal(err)
		}
	}
}
