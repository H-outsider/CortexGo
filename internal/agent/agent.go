package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cortexgo/cortexgo/internal/memory"
	"github.com/cortexgo/cortexgo/internal/provider"
	"github.com/cortexgo/cortexgo/internal/tool"
)

const (
	defaultMaxToolRounds = 4
	defaultToolTimeout   = 15 * time.Second
)

type Agent struct {
	model         provider.ChatModel
	memory        memory.Store
	tools         *tool.Registry
	maxToolRounds int
	toolTimeout   time.Duration
	authorizer    ToolAuthorizer
	auditLogger   ToolAuditLogger
	contextLimit  int
	summarizer    memory.SummaryFunc
}

type ChatResult = provider.ChatResult

type Option func(*Agent)

type ToolAuthorizer func(context.Context, string) error

type ToolAuditEvent struct {
	SessionID  string
	CallID     string
	ToolName   string
	StartedAt  time.Time
	FinishedAt time.Time
	Duration   time.Duration
	Error      string
}

type ToolAuditLogger func(context.Context, ToolAuditEvent)

func WithTools(registry *tool.Registry) Option {
	return func(a *Agent) {
		a.tools = registry
	}
}

func WithMaxToolRounds(rounds int) Option {
	return func(a *Agent) {
		if rounds > 0 {
			a.maxToolRounds = rounds
		}
	}
}

func WithToolTimeout(timeout time.Duration) Option {
	return func(a *Agent) {
		if timeout > 0 {
			a.toolTimeout = timeout
		}
	}
}

func WithToolAuthorizer(authorizer ToolAuthorizer) Option {
	return func(a *Agent) { a.authorizer = authorizer }
}

func WithToolAuditLogger(logger ToolAuditLogger) Option {
	return func(a *Agent) { a.auditLogger = logger }
}

func WithContextMessageLimit(limit int) Option {
	return func(a *Agent) {
		if limit > 0 {
			a.contextLimit = limit
		}
	}
}

func WithConversationSummarizer(summarizer memory.SummaryFunc) Option {
	return func(a *Agent) { a.summarizer = summarizer }
}

func New(model provider.ChatModel, store memory.Store, options ...Option) *Agent {
	a := &Agent{
		model:         model,
		memory:        store,
		maxToolRounds: defaultMaxToolRounds,
		toolTimeout:   defaultToolTimeout,
	}
	for _, option := range options {
		option(a)
	}
	return a
}

func (a *Agent) Chat(ctx context.Context, sessionID, input string) (string, error) {
	result, err := a.ChatWithResult(ctx, sessionID, input)
	return result.Content, err
}

func (a *Agent) ChatWithResult(ctx context.Context, sessionID, input string) (ChatResult, error) {
	return a.run(ctx, sessionID, input, false, nil)
}

func (a *Agent) ChatStream(ctx context.Context, sessionID, input string, onDelta func(string) error) (string, error) {
	result, err := a.ChatStreamWithResult(ctx, sessionID, input, onDelta)
	return result.Content, err
}

func (a *Agent) ChatStreamWithResult(ctx context.Context, sessionID, input string, onDelta func(string) error) (ChatResult, error) {
	return a.run(ctx, sessionID, input, true, onDelta)
}

func (a *Agent) run(ctx context.Context, sessionID, input string, stream bool, onDelta func(string) error) (ChatResult, error) {
	history, err := a.memory.List(ctx, sessionID)
	if err != nil {
		return ChatResult{}, err
	}
	historyStart := len(history)
	if a.contextLimit > 0 && len(history)+1 > a.contextLimit {
		compacted, compactErr := compactHistory(ctx, history, a.contextLimit-1, a.summarizer)
		if compactErr != nil {
			return ChatResult{}, compactErr
		}
		if replacer, ok := a.memory.(memory.ReplaceStore); ok {
			if err := replacer.Replace(ctx, sessionID, compacted...); err != nil {
				return ChatResult{}, fmt.Errorf("agent: persist compacted history: %w", err)
			}
		}
		history = compacted
		historyStart = len(history)
	}
	transcript := append(history, provider.Message{Role: "user", Content: input})
	options := provider.ChatOptions{Tools: providerTools(a.tools)}
	totalUsage := provider.Usage{}
	var streamed strings.Builder
	streamDelta := onDelta
	if stream {
		streamDelta = func(delta string) error {
			streamed.WriteString(delta)
			if onDelta != nil {
				return onDelta(delta)
			}
			return nil
		}
	}

	for round := 0; ; round++ {
		result, err := a.callModel(ctx, transcript, stream, streamDelta, options)
		if err != nil {
			return ChatResult{}, err
		}
		totalUsage = addUsage(totalUsage, result.Usage)

		if len(result.ToolCalls) == 0 {
			result.Usage = totalUsage
			assistantContent := result.Content
			if stream {
				result.Content = streamed.String()
			}
			transcript = append(transcript, provider.Message{
				Role:    "assistant",
				Content: assistantContent,
			})
			if err := a.memory.Append(ctx, sessionID, transcript[historyStart:]...); err != nil {
				return ChatResult{}, err
			}
			return result, nil
		}

		if round >= a.maxToolRounds {
			return ChatResult{}, fmt.Errorf("agent: exceeded maximum tool rounds (%d)", a.maxToolRounds)
		}
		if err := validateToolCalls(result.ToolCalls); err != nil {
			return ChatResult{}, err
		}

		transcript = append(transcript, provider.Message{
			Role:      "assistant",
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		})
		for _, call := range result.ToolCalls {
			output, invokeErr := a.invokeTool(ctx, sessionID, call)
			if invokeErr != nil {
				errorOutput, _ := json.Marshal(map[string]string{"error": invokeErr.Error()})
				output = errorOutput
			}
			transcript = append(transcript, provider.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    string(output),
			})
		}
	}
}

func compactHistory(ctx context.Context, history []provider.Message, limit int, summarize memory.SummaryFunc) ([]provider.Message, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("agent: context message limit must be greater than one")
	}
	if len(history) <= limit {
		return append([]provider.Message(nil), history...), nil
	}
	if len(history) > 0 && history[0].Role == "system" && strings.HasPrefix(history[0].Content, "Conversation summary:\n") {
		keep := limit - 1
		if keep < 0 {
			keep = 0
		}
		start := len(history) - keep
		result := append([]provider.Message(nil), history[:1]...)
		result = append(result, history[start:]...)
		return result, nil
	}
	return memory.Compact(ctx, history, limit, summarize)
}

func (a *Agent) callModel(
	ctx context.Context,
	messages []provider.Message,
	stream bool,
	onDelta func(string) error,
	options provider.ChatOptions,
) (ChatResult, error) {
	if !stream {
		if model, ok := a.model.(provider.ToolChatModel); ok {
			return model.ChatWithOptions(ctx, messages, options)
		}
		return a.model.Chat(ctx, messages)
	}
	if model, ok := a.model.(provider.ToolStreamChatModel); ok {
		return model.ChatStreamWithOptions(ctx, messages, onDelta, options)
	}
	streamModel, ok := a.model.(provider.StreamChatModel)
	if !ok {
		if model, ok := a.model.(provider.ToolChatModel); ok {
			return model.ChatWithOptions(ctx, messages, options)
		}
		return a.model.Chat(ctx, messages)
	}
	if onDelta == nil {
		onDelta = func(string) error { return nil }
	}
	return streamModel.ChatStream(ctx, messages, onDelta)
}

func (a *Agent) invokeTool(ctx context.Context, sessionID string, call provider.ToolCall) (output json.RawMessage, err error) {
	startedAt := time.Now()
	defer func() {
		if a.auditLogger == nil {
			return
		}
		event := ToolAuditEvent{
			SessionID: sessionID, CallID: call.ID, ToolName: call.Function.Name,
			StartedAt: startedAt, FinishedAt: time.Now(), Error: errorString(err),
		}
		event.Duration = event.FinishedAt.Sub(event.StartedAt)
		safeAudit(a.auditLogger, ctx, event)
	}()

	if a.authorizer != nil {
		if err := a.authorizer(ctx, call.Function.Name); err != nil {
			return nil, fmt.Errorf("authorize tool %q: %w", call.Function.Name, err)
		}
	}
	if a.tools == nil {
		return nil, fmt.Errorf("tool %q is not registered", call.Function.Name)
	}
	toolCtx, cancel := context.WithTimeout(ctx, a.toolTimeout)
	defer cancel()
	result := make(chan toolResult, 1)
	go func() {
		output, err := a.tools.Invoke(toolCtx, call.Function.Name, json.RawMessage(call.Function.Arguments))
		result <- toolResult{output: output, err: err}
	}()
	select {
	case response := <-result:
		return response.output, response.err
	case <-toolCtx.Done():
		return nil, toolCtx.Err()
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func safeAudit(logger ToolAuditLogger, ctx context.Context, event ToolAuditEvent) {
	defer func() { _ = recover() }()
	logger(ctx, event)
}

type toolResult struct {
	output json.RawMessage
	err    error
}

func providerTools(registry *tool.Registry) []provider.Tool {
	if registry == nil {
		return nil
	}
	definitions := registry.Definitions()
	if len(definitions) == 0 {
		return nil
	}
	tools := make([]provider.Tool, 0, len(definitions))
	for _, definition := range definitions {
		tools = append(tools, provider.Tool{
			Name:        definition.Name,
			Description: definition.Description,
			Parameters:  definition.InputSchema,
		})
	}
	return tools
}

func validateToolCalls(calls []provider.ToolCall) error {
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		if call.ID == "" {
			return fmt.Errorf("agent: tool call has no id")
		}
		if _, exists := seen[call.ID]; exists {
			return fmt.Errorf("agent: duplicate tool call id %q", call.ID)
		}
		seen[call.ID] = struct{}{}
		if call.Type != "function" {
			return fmt.Errorf("agent: unsupported tool call type %q", call.Type)
		}
		if strings.TrimSpace(call.Function.Name) == "" {
			return fmt.Errorf("agent: tool call %q has no function name", call.ID)
		}
		arguments := strings.TrimSpace(call.Function.Arguments)
		if arguments != "" && !json.Valid([]byte(arguments)) {
			return fmt.Errorf("agent: tool call %q has invalid JSON arguments", call.ID)
		}
	}
	return nil
}

func addUsage(left, right provider.Usage) provider.Usage {
	return provider.Usage{
		PromptTokens:     left.PromptTokens + right.PromptTokens,
		CompletionTokens: left.CompletionTokens + right.CompletionTokens,
		TotalTokens:      left.TotalTokens + right.TotalTokens,
	}
}
