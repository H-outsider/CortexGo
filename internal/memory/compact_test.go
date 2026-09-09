package memory

import (
	"context"
	"testing"

	"github.com/cortexgo/cortexgo/internal/provider"
)

func TestCompactSummarizesOlderMessages(t *testing.T) {
	messages := []provider.Message{{Role: "user", Content: "one"}, {Role: "assistant", Content: "two"}, {Role: "user", Content: "three"}, {Role: "assistant", Content: "four"}}
	result, err := Compact(context.Background(), messages, 3, func(_ context.Context, old []provider.Message) (string, error) {
		if len(old) != 2 {
			t.Fatalf("old messages = %#v", old)
		}
		return "one and two", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 || result[0].Role != "system" || result[1].Content != "three" || result[2].Content != "four" {
		t.Fatalf("compact result = %#v", result)
	}
}

func TestCompactRequiresSummaryFunction(t *testing.T) {
	_, err := Compact(context.Background(), []provider.Message{{Role: "user", Content: "one"}, {Role: "assistant", Content: "two"}}, 1, nil)
	if err == nil {
		t.Fatal("expected missing summary function error")
	}
}

func TestCompactKeepsToolCallWithItsResult(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: "request"},
		{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "call-1", Type: "function"}}},
		{Role: "tool", ToolCallID: "call-1", Content: "result"},
		{Role: "user", Content: "follow-up"},
	}
	result, err := Compact(context.Background(), messages, 3, func(context.Context, []provider.Message) (string, error) { return "summary", nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(result) < 3 || result[1].Role != "assistant" || result[2].Role != "tool" || result[2].ToolCallID != "call-1" {
		t.Fatalf("tool pair was split: %#v", result)
	}
}

func TestEstimateTokensCountsContentAndToolCalls(t *testing.T) {
	short := EstimateTokens([]provider.Message{{Role: "user", Content: "hello"}})
	long := EstimateTokens([]provider.Message{{Role: "user", Content: "hello hello hello hello"}})
	if short <= 0 || long <= short {
		t.Fatalf("short=%d long=%d", short, long)
	}
}

func TestCompactToTokenBudget(t *testing.T) {
	messages := []provider.Message{{Role: "user", Content: "old old old old old old old old old old"}, {Role: "assistant", Content: "reply reply"}, {Role: "user", Content: "latest"}}
	result, err := CompactToTokenBudget(context.Background(), messages, 20, func(context.Context, []provider.Message) (string, error) { return "summary", nil })
	if err != nil {
		t.Fatal(err)
	}
	if EstimateTokens(result) > 20 || result[0].Role != "system" {
		t.Fatalf("result=%#v tokens=%d", result, EstimateTokens(result))
	}
}
