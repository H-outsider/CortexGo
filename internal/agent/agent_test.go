package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cortexgo/cortexgo/internal/memory"
	"github.com/cortexgo/cortexgo/internal/provider"
)

func TestChatPersistsConversation(t *testing.T) {
	store := memory.NewInMemory()
	a := New(provider.Echo{}, store)
	ctx := context.Background()

	if got, err := a.Chat(ctx, "s1", "第一句"); err != nil || got != "收到：第一句" {
		t.Fatalf("first chat = %q, %v", got, err)
	}
	if _, err := a.Chat(ctx, "s1", "第二句"); err != nil {
		t.Fatal(err)
	}
	history, err := store.List(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 || history[0].Role != "user" || history[1].Role != "assistant" {
		t.Fatalf("unexpected history: %#v", history)
	}

	result, err := a.ChatWithResult(ctx, "metadata", "元数据")
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "收到：元数据" || result.Model != "echo" || result.FinishReason != "stop" {
		t.Fatalf("unexpected chat result: %#v", result)
	}
}

func TestChatStreamPersistsConversation(t *testing.T) {
	store := memory.NewInMemory()
	a := New(provider.Echo{}, store)

	var output strings.Builder
	result, err := a.ChatStreamWithResult(context.Background(), "stream", "第一句", func(delta string) error {
		output.WriteString(delta)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != output.String() || result.Content != "收到：第一句" {
		t.Fatalf("stream output = %#v, collected = %q", result, output.String())
	}
	if result.Model != "echo" || result.FinishReason != "stop" {
		t.Fatalf("unexpected stream result: %#v", result)
	}
	history, err := store.List(context.Background(), "stream")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[1].Content != result.Content {
		t.Fatalf("unexpected history: %#v", history)
	}
}
