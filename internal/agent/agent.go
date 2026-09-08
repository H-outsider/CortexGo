package agent

import (
	"context"
	"strings"

	"github.com/cortexgo/cortexgo/internal/memory"
	"github.com/cortexgo/cortexgo/internal/provider"
)

type Agent struct {
	model  provider.ChatModel
	memory memory.Store
}

type ChatResult = provider.ChatResult

func New(model provider.ChatModel, store memory.Store) *Agent {
	return &Agent{model: model, memory: store}
}

func (a *Agent) Chat(ctx context.Context, sessionID, input string) (string, error) {
	result, err := a.ChatWithResult(ctx, sessionID, input)
	return result.Content, err
}

func (a *Agent) ChatWithResult(ctx context.Context, sessionID, input string) (ChatResult, error) {
	history, err := a.memory.List(ctx, sessionID)
	if err != nil {
		return ChatResult{}, err
	}
	history = append(history, provider.Message{Role: "user", Content: input})
	result, err := a.model.Chat(ctx, history)
	if err != nil {
		return ChatResult{}, err
	}
	if err := a.memory.Append(ctx, sessionID,
		provider.Message{Role: "user", Content: input},
		provider.Message{Role: "assistant", Content: result.Content},
	); err != nil {
		return ChatResult{}, err
	}
	return result, nil
}

func (a *Agent) ChatStream(ctx context.Context, sessionID, input string, onDelta func(string) error) (string, error) {
	result, err := a.ChatStreamWithResult(ctx, sessionID, input, onDelta)
	return result.Content, err
}

func (a *Agent) ChatStreamWithResult(ctx context.Context, sessionID, input string, onDelta func(string) error) (ChatResult, error) {
	streamModel, ok := a.model.(provider.StreamChatModel)
	if !ok {
		return a.ChatWithResult(ctx, sessionID, input)
	}

	history, err := a.memory.List(ctx, sessionID)
	if err != nil {
		return ChatResult{}, err
	}
	history = append(history, provider.Message{Role: "user", Content: input})

	var output strings.Builder
	result, err := streamModel.ChatStream(ctx, history, func(delta string) error {
		output.WriteString(delta)
		return onDelta(delta)
	})
	if err != nil {
		return ChatResult{}, err
	}
	result.Content = output.String()
	if err := a.memory.Append(ctx, sessionID,
		provider.Message{Role: "user", Content: input},
		provider.Message{Role: "assistant", Content: result.Content},
	); err != nil {
		return ChatResult{}, err
	}
	return result, nil
}
