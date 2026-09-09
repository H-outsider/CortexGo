package memory

import (
	"context"
	"fmt"
	"unicode/utf8"

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
	// Never leave a retained tool result without its assistant tool-call message.
	for cut > 0 && cut < len(messages) && messages[cut].Role == "tool" {
		cut--
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

// EstimateTokens returns a conservative, provider-independent token estimate.
// It is intentionally replaceable because exact tokenization differs by model.
func EstimateTokens(messages []provider.Message) int {
	total := 0
	for _, message := range messages {
		chars := utf8.RuneCountInString(message.Content) + utf8.RuneCountInString(message.ToolCallID)
		for _, call := range message.ToolCalls {
			chars += utf8.RuneCountInString(call.ID) + utf8.RuneCountInString(call.Function.Name) + utf8.RuneCountInString(call.Function.Arguments)
		}
		total += 4 + (chars+3)/4
	}
	return total
}

// CompactToTokenBudget compacts messages until they fit the approximate budget.
func CompactToTokenBudget(ctx context.Context, messages []provider.Message, budget int, summarize SummaryFunc) ([]provider.Message, error) {
	if budget <= 0 {
		return nil, fmt.Errorf("memory: token budget must be positive")
	}
	if EstimateTokens(messages) <= budget {
		return cloneMessages(messages), nil
	}
	if summarize == nil {
		return nil, fmt.Errorf("memory: summary function is required when compaction is needed")
	}
	if len(messages) > 1 {
		summary, err := summarize(ctx, messages[:len(messages)-1])
		if err != nil {
			return nil, fmt.Errorf("memory: summarize conversation: %w", err)
		}
		result := []provider.Message{{Role: "system", Content: "Conversation summary:\n" + summary}, messages[len(messages)-1]}
		if EstimateTokens(result) <= budget {
			return result, nil
		}
	}
	return nil, fmt.Errorf("memory: unable to fit conversation into token budget %d", budget)
}
