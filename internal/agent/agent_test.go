package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cortexgo/cortexgo/internal/memory"
	"github.com/cortexgo/cortexgo/internal/provider"
	"github.com/cortexgo/cortexgo/internal/tool"
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

func TestChatExecutesToolLoop(t *testing.T) {
	store := memory.NewInMemory()
	registry := tool.NewRegistry()
	var toolInput json.RawMessage
	if err := registry.Register(tool.Definition{
		Name:        "lookup_user",
		Description: "Look up a user by ID",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{"id":{"type":"integer"}},
			"required":["id"],
			"additionalProperties":false
		}`),
		Handler: func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
			toolInput = append([]byte(nil), input...)
			return json.RawMessage(`{"name":"Kuban"}`), nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	model := &scriptedToolModel{}
	a := New(model, store, WithTools(registry))
	result, err := a.ChatWithResult(context.Background(), "tools", "查一下用户 7")
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "用户是 Kuban" || result.FinishReason != "stop" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Usage != (provider.Usage{PromptTokens: 7, CompletionTokens: 4, TotalTokens: 11}) {
		t.Fatalf("usage was not accumulated: %#v", result.Usage)
	}
	if string(toolInput) != `{"id":7}` || len(model.capturedMessages) != 2 {
		t.Fatalf("tool input = %s, model calls = %d", toolInput, len(model.capturedMessages))
	}

	secondMessages := model.capturedMessages[1]
	if len(secondMessages) != 3 ||
		secondMessages[1].Role != "assistant" ||
		len(secondMessages[1].ToolCalls) != 1 ||
		secondMessages[2].Role != "tool" ||
		secondMessages[2].ToolCallID != "call-1" ||
		secondMessages[2].Content != `{"name":"Kuban"}` {
		t.Fatalf("unexpected second model messages: %#v", secondMessages)
	}
	history, err := store.List(context.Background(), "tools")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 || history[2].Role != "tool" || history[3].Content != result.Content {
		t.Fatalf("unexpected persisted transcript: %#v", history)
	}
}

func TestChatStreamExecutesToolLoop(t *testing.T) {
	store := memory.NewInMemory()
	registry := tool.NewRegistry()
	if err := registry.Register(tool.Definition{
		Name:        "lookup_user",
		Description: "Look up a user by ID",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}`),
		Handler: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"name":"Kuban"}`), nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	model := &scriptedToolModel{}
	a := New(model, store, WithTools(registry))
	result, err := a.ChatStreamWithResult(context.Background(), "stream-tools", "查用户", func(delta string) error {
		output.WriteString(delta)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "用户是 Kuban" || output.String() != result.Content {
		t.Fatalf("result = %#v, output = %q", result, output.String())
	}
	history, err := store.List(context.Background(), "stream-tools")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 || history[2].Role != "tool" {
		t.Fatalf("unexpected stream transcript: %#v", history)
	}
}

func TestChatStopsAfterMaximumToolRounds(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(tool.Definition{
		Name:        "lookup_user",
		Description: "Look up a user by ID",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Handler: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	model := &loopingToolModel{}
	a := New(model, memory.NewInMemory(), WithTools(registry), WithMaxToolRounds(1))
	if _, err := a.Chat(context.Background(), "loop", "一直查"); err == nil {
		t.Fatal("expected maximum tool round error")
	}
	if model.calls != 2 {
		t.Fatalf("model calls = %d", model.calls)
	}
}

func TestToolAuthorizationAndAudit(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(tool.Definition{
		Name: "lookup_user", Description: "Look up a user", InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"ok":true}`), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	var events []ToolAuditEvent
	a := New(&scriptedToolModel{}, memory.NewInMemory(), WithTools(registry),
		WithToolAuthorizer(func(_ context.Context, name string) error {
			if name != "lookup_user" {
				return fmt.Errorf("denied")
			}
			return nil
		}),
		WithToolAuditLogger(func(_ context.Context, event ToolAuditEvent) { events = append(events, event) }),
	)
	if _, err := a.Chat(context.Background(), "audit", "查用户"); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].SessionID != "audit" || events[0].ToolName != "lookup_user" || events[0].CallID != "call-1" || events[0].Error != "" || events[0].Duration < 0 || events[0].FinishedAt.Before(events[0].StartedAt) {
		t.Fatalf("unexpected audit events: %#v", events)
	}
}

type scriptedToolModel struct {
	calls             int
	capturedMessages  [][]provider.Message
	capturedToolSpecs [][]provider.Tool
}

func (m *scriptedToolModel) Chat(_ context.Context, messages []provider.Message) (provider.ChatResult, error) {
	return m.chat(messages)
}

func (m *scriptedToolModel) ChatWithOptions(_ context.Context, messages []provider.Message, options ...provider.ChatOptions) (provider.ChatResult, error) {
	m.capture(messages, options)
	return m.response(), nil
}

func (m *scriptedToolModel) chat(messages []provider.Message) (provider.ChatResult, error) {
	m.capture(messages, nil)
	return m.response(), nil
}

func (m *scriptedToolModel) response() provider.ChatResult {
	if m.calls == 1 {
		return provider.ChatResult{
			Model:        "scripted",
			FinishReason: "tool_calls",
			Usage:        provider.Usage{PromptTokens: 3, CompletionTokens: 1, TotalTokens: 4},
			ToolCalls: []provider.ToolCall{{
				ID:       "call-1",
				Type:     "function",
				Function: provider.ToolCallFunction{Name: "lookup_user", Arguments: `{"id":7}`},
			}},
		}
	}
	return provider.ChatResult{
		Content:      "用户是 Kuban",
		Model:        "scripted",
		FinishReason: "stop",
		Usage:        provider.Usage{PromptTokens: 4, CompletionTokens: 3, TotalTokens: 7},
	}
}

func (m *scriptedToolModel) ChatStream(ctx context.Context, messages []provider.Message, onDelta func(string) error) (provider.ChatResult, error) {
	return m.ChatStreamWithOptions(ctx, messages, onDelta)
}

func (m *scriptedToolModel) ChatStreamWithOptions(ctx context.Context, messages []provider.Message, onDelta func(string) error, options ...provider.ChatOptions) (provider.ChatResult, error) {
	result, err := m.ChatWithOptions(ctx, messages, options...)
	if err != nil {
		return provider.ChatResult{}, err
	}
	if m.calls == 2 && onDelta != nil {
		if err := onDelta(result.Content); err != nil {
			return provider.ChatResult{}, err
		}
	}
	return result, nil
}

func (m *scriptedToolModel) capture(messages []provider.Message, options []provider.ChatOptions) {
	m.calls++
	m.capturedMessages = append(m.capturedMessages, append([]provider.Message(nil), messages...))
	if len(options) == 1 {
		m.capturedToolSpecs = append(m.capturedToolSpecs, append([]provider.Tool(nil), options[0].Tools...))
	}
}

type loopingToolModel struct{ calls int }

func (m *loopingToolModel) Chat(_ context.Context, messages []provider.Message) (provider.ChatResult, error) {
	m.calls++
	return provider.ChatResult{
		FinishReason: "tool_calls",
		ToolCalls: []provider.ToolCall{{
			ID:       fmt.Sprintf("call-%d", m.calls),
			Type:     "function",
			Function: provider.ToolCallFunction{Name: "lookup_user", Arguments: "{}"},
		}},
	}, nil
}
