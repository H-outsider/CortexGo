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
