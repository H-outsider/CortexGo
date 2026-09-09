package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Message 是模型上下文中的一条消息。
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ChatOptions struct {
	Tools []Tool
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatResult struct {
	Content      string
	Model        string
	FinishReason string
	Usage        Usage
	ToolCalls    []ToolCall
}

// ChatModel 是所有 LLM 适配器必须实现的最小接口。
// 后续可以在此之上接入 OpenAI、Anthropic、本地模型或企业网关。
type ChatModel interface {
	Chat(ctx context.Context, messages []Message) (ChatResult, error)
}

// ToolChatModel is the optional extension implemented by providers that accept tool definitions.
type ToolChatModel interface {
	ChatWithOptions(ctx context.Context, messages []Message, options ...ChatOptions) (ChatResult, error)
}

// StreamChatModel is implemented by providers that can emit tokens as they arrive.
type StreamChatModel interface {
	ChatStream(ctx context.Context, messages []Message, onDelta func(string) error) (ChatResult, error)
}

// ToolStreamChatModel is the optional streaming extension for tool definitions.
type ToolStreamChatModel interface {
	ChatStreamWithOptions(ctx context.Context, messages []Message, onDelta func(string) error, options ...ChatOptions) (ChatResult, error)
}

type OpenAICompatible struct {
	BaseURL        string
	APIKey         string
	Model          string
	EmbeddingModel string
	HTTPClient     *http.Client
	MaxRetries     int
	RequestTimeout time.Duration
	RetryBackoff   time.Duration
	Logger         *log.Logger
}

// Error adds stable operation/status context while preserving the underlying error.
type Error struct {
	Op     string
	Status int
	Err    error
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("provider: %s (http %d): %v", e.Op, e.Status, e.Err)
	}
	return fmt.Sprintf("provider: %s: %v", e.Op, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func (p OpenAICompatible) logf(format string, args ...any) {
	if p.Logger != nil {
		p.Logger.Printf(format, args...)
	}
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatRequest struct {
	Model         string         `json:"model"`
	Messages      []Message      `json:"messages"`
	Stream        bool           `json:"stream,omitempty"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
	Tools         []chatTool     `json:"tools,omitempty"`
}

type chatTool struct {
	Type     string `json:"type"`
	Function Tool   `json:"function"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p OpenAICompatible) Chat(ctx context.Context, messages []Message) (ChatResult, error) {
	return p.ChatWithOptions(ctx, messages)
}

func (p OpenAICompatible) ChatWithOptions(ctx context.Context, messages []Message, options ...ChatOptions) (ChatResult, error) {
	if p.BaseURL == "" || p.Model == "" {
		return ChatResult{}, fmt.Errorf("provider: BaseURL and Model are required")
	}
	body, err := json.Marshal(chatRequest{
		Model:    p.Model,
		Messages: messages,
		Tools:    chatTools(options),
	})
	if err != nil {
		return ChatResult{}, err
	}

	requestCtx, cancel := context.WithTimeout(ctx, p.timeout(60*time.Second))
	defer cancel()
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	retries := nonNegative(p.MaxRetries)
	url := strings.TrimRight(p.BaseURL, "/") + "/v1/chat/completions"

	for attempt := 0; ; attempt++ {
		p.logf("chat request attempt=%d model=%s", attempt+1, p.Model)
		result, retry, err := p.doChat(requestCtx, client, url, body)
		if err == nil {
			return result, nil
		}
		if !retry || attempt >= retries {
			return ChatResult{}, err
		}
		p.logf("chat request failed; retrying attempt=%d error=%v", attempt+1, err)
		if waitErr := waitForRetry(requestCtx, retryDelay(p.RetryBackoff, attempt)); waitErr != nil {
			return ChatResult{}, waitErr
		}
	}
}

func (p OpenAICompatible) doChat(ctx context.Context, client *http.Client, url string, body []byte) (ChatResult, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return ChatResult{}, true, err
	}
	data, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return ChatResult{}, true, readErr
	}
	if retryableStatus(resp.StatusCode) {
		return ChatResult{}, true, &Error{Op: "chat", Status: resp.StatusCode, Err: fmt.Errorf("%s", strings.TrimSpace(string(data)))}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ChatResult{}, false, &Error{Op: "chat", Status: resp.StatusCode, Err: fmt.Errorf("%s", strings.TrimSpace(string(data)))}
	}

	var result chatResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return ChatResult{}, false, fmt.Errorf("provider: decode response: %w", err)
	}
	if result.Error != nil {
		return ChatResult{}, false, fmt.Errorf("provider: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return ChatResult{}, false, fmt.Errorf("provider: response has no choices")
	}

	message := result.Choices[0].Message
	chatResult := ChatResult{
		Content:      result.Choices[0].Message.Content,
		Model:        result.Model,
		FinishReason: result.Choices[0].FinishReason,
		ToolCalls:    message.ToolCalls,
	}
	if result.Usage != nil {
		chatResult.Usage = *result.Usage
	}
	return chatResult, false, nil
}

func chatTools(options []ChatOptions) []chatTool {
	if len(options) == 0 {
		return nil
	}
	tools := options[0].Tools
	if len(tools) == 0 {
		return nil
	}
	specs := make([]chatTool, 0, len(tools))
	for _, tool := range tools {
		specs = append(specs, chatTool{Type: "function", Function: tool})
	}
	return specs
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func (p OpenAICompatible) timeout(fallback time.Duration) time.Duration {
	if p.RequestTimeout > 0 {
		return p.RequestTimeout
	}
	return fallback
}

func retryDelay(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = 100 * time.Millisecond
	}
	if base > 5*time.Second {
		base = 5 * time.Second
	}
	delay := base
	for range attempt {
		if delay >= 2500*time.Millisecond {
			return 5 * time.Second
		}
		delay *= 2
	}
	if delay > 5*time.Second {
		return 5 * time.Second
	}
	return delay
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ChatStream consumes Server-Sent Events and calls onDelta for each text delta.
func (p OpenAICompatible) ChatStream(ctx context.Context, messages []Message, onDelta func(string) error) (ChatResult, error) {
	return p.ChatStreamWithOptions(ctx, messages, onDelta)
}

func (p OpenAICompatible) ChatStreamWithOptions(ctx context.Context, messages []Message, onDelta func(string) error, options ...ChatOptions) (ChatResult, error) {
	if p.BaseURL == "" || p.Model == "" {
		return ChatResult{}, fmt.Errorf("provider: BaseURL and Model are required")
	}
	if onDelta == nil {
		return ChatResult{}, fmt.Errorf("provider: onDelta is required")
	}
	body, err := json.Marshal(chatRequest{
		Model:         p.Model,
		Messages:      messages,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		Tools:         chatTools(options),
	})
	if err != nil {
		return ChatResult{}, err
	}

	requestCtx, cancel := context.WithTimeout(ctx, p.timeout(120*time.Second))
	defer cancel()
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	retries := nonNegative(p.MaxRetries)
	url := strings.TrimRight(p.BaseURL, "/") + "/v1/chat/completions"

	var resp *http.Response
	for attempt := 0; ; attempt++ {
		p.logf("stream request attempt=%d model=%s", attempt+1, p.Model)
		req, reqErr := http.NewRequestWithContext(requestCtx, http.MethodPost, url, bytes.NewReader(body))
		if reqErr != nil {
			return ChatResult{}, reqErr
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		if p.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.APIKey)
		}

		var doErr error
		resp, doErr = client.Do(req)
		if doErr != nil {
			if attempt >= retries {
				return ChatResult{}, doErr
			}
			p.logf("stream request failed; retrying attempt=%d error=%v", attempt+1, doErr)
			if waitErr := waitForRetry(requestCtx, retryDelay(p.RetryBackoff, attempt)); waitErr != nil {
				return ChatResult{}, waitErr
			}
			continue
		}

		if retryableStatus(resp.StatusCode) {
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if attempt >= retries {
				return ChatResult{}, &Error{Op: "stream", Status: resp.StatusCode, Err: fmt.Errorf("%s", strings.TrimSpace(string(data)))}
			}
			if waitErr := waitForRetry(requestCtx, retryDelay(p.RetryBackoff, attempt)); waitErr != nil {
				return ChatResult{}, waitErr
			}
			continue
		}
		break
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return ChatResult{}, &Error{Op: "stream", Status: resp.StatusCode, Err: fmt.Errorf("%s", strings.TrimSpace(string(data)))}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var result ChatResult
	toolCalls := make(map[int]ToolCall)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var event struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content   string           `json:"content"`
					ToolCalls []streamToolCall `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *Usage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return ChatResult{}, fmt.Errorf("provider: decode stream event: %w", err)
		}
		if event.Model != "" {
			result.Model = event.Model
		}
		if event.Usage != nil {
			result.Usage = *event.Usage
		}
		if len(event.Choices) == 0 {
			continue
		}
		choice := event.Choices[0]
		if choice.FinishReason != "" {
			result.FinishReason = choice.FinishReason
		}
		if choice.Delta.Content != "" {
			result.Content += choice.Delta.Content
			if err := onDelta(choice.Delta.Content); err != nil {
				return ChatResult{}, err
			}
		}
		for _, call := range choice.Delta.ToolCalls {
			existing := toolCalls[call.Index]
			existing.ID = firstNonEmpty(existing.ID, call.ID)
			existing.Type = firstNonEmpty(existing.Type, call.Type)
			existing.Function.Name = firstNonEmpty(existing.Function.Name, call.Function.Name)
			existing.Function.Arguments += call.Function.Arguments
			toolCalls[call.Index] = existing
		}
	}
	if err := scanner.Err(); err != nil {
		return ChatResult{}, err
	}
	result.ToolCalls = orderedToolCalls(toolCalls)
	return result, nil
}

type streamToolCall struct {
	Index    int              `json:"index"`
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func orderedToolCalls(calls map[int]ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(calls))
	for index := range calls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	ordered := make([]ToolCall, 0, len(indexes))
	for _, index := range indexes {
		call := calls[index]
		if call.Type == "" {
			call.Type = "function"
		}
		ordered = append(ordered, call)
	}
	return ordered
}

// Echo 是本地可运行的开发模型，用于验证 Agent 编排链路。
type Echo struct{}

func (Echo) Chat(_ context.Context, messages []Message) (ChatResult, error) {
	if len(messages) == 0 {
		return ChatResult{Model: "echo", FinishReason: "stop"}, nil
	}
	last := messages[len(messages)-1]
	return ChatResult{
		Content:      "收到：" + last.Content,
		Model:        "echo",
		FinishReason: "stop",
	}, nil
}

func (Echo) ChatStream(ctx context.Context, messages []Message, onDelta func(string) error) (ChatResult, error) {
	result, err := Echo{}.Chat(ctx, messages)
	if err != nil {
		return ChatResult{}, err
	}
	if onDelta == nil {
		return ChatResult{}, fmt.Errorf("provider: onDelta is required")
	}
	if err := onDelta(result.Content); err != nil {
		return ChatResult{}, err
	}
	return result, nil
}
