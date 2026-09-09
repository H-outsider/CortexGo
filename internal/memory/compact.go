package memory

import (
	"context"
	"fmt"

	"github.com/cortexgo/cortexgo/internal/provider"
)

// SummaryFunc turns older conversation messages into a compact plain-text summary.
type SummaryFunc func(context.Context, []provider.Message) (string, error)

// Compact keeps the newest messages and replaces older ones with one summary message.
// The limit counts the returned messages, including the summary when one is created.
func Compact(ctx context.Context, messages []provider.Message, limit int, summarize SummaryFunc) ([]provider.Message, error) {
	if limit <= 0 || len(messages) <= limit {
		return cloneMessages(messages), nil
	}
	if summarize == nil {
		return nil, fmt.Errorf("memory: summary function is required when compaction is needed")
	}
	cut := len(messages) - limit + 1
	if cut < 1 {
		cut = 1
	}
	summary, err := summarize(ctx, messages[:cut])
	if err != nil {
		return nil, fmt.Errorf("memory: summarize conversation: %w", err)
	}
	result := make([]provider.Message, 0, limit)
	result = append(result, provider.Message{Role: "system", Content: "Conversation summary:\n" + summary})
	result = append(result, cloneMessages(messages[cut:])...)
	return result, nil
}
