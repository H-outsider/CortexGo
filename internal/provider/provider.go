package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Message 是模型上下文中的一条消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
}

// ChatModel 是所有 LLM 适配器必须实现的最小接口。
// 后续可以在此之上接入 OpenAI、Anthropic、本地模型或企业网关。
type ChatModel interface {
	Chat(ctx context.Context, messages []Message) (ChatResult, error)
}

// StreamChatModel is implemented by providers that can emit tokens as they arrive.
type StreamChatModel interface {
	ChatStream(ctx context.Context, messages []Message, onDelta func(string) error) (ChatResult, error)
}

type OpenAICompatible struct {
	BaseURL        string
	APIKey         string
	Model          string
	HTTPClient     *http.Client
	MaxRetries     int
	RequestTimeout time.Duration
	RetryBackoff   time.Duration
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatRequest struct {
	Model         string         `json:"model"`
	Messages      []Message      `json:"messages"`
	Stream        bool           `json:"stream,omitempty"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
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
	if p.BaseURL == "" || p.Model == "" {
		return ChatResult{}, fmt.Errorf("provider: BaseURL and Model are required")
	}
	body, err := json.Marshal(chatRequest{Model: p.Model, Messages: messages})
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
		result, retry, err := p.doChat(requestCtx, client, url, body)
		if err == nil {
			return result, nil
		}
		if !retry || attempt >= retries {
			return ChatResult{}, err
		}
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
		return ChatResult{}, true, fmt.Errorf("provider: http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ChatResult{}, false, fmt.Errorf("provider: http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
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

	chatResult := ChatResult{
		Content:      result.Choices[0].Message.Content,
		Model:        result.Model,
		FinishReason: result.Choices[0].FinishReason,
	}
	if result.Usage != nil {
		chatResult.Usage = *result.Usage
	}
	return chatResult, false, nil
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
			if waitErr := waitForRetry(requestCtx, retryDelay(p.RetryBackoff, attempt)); waitErr != nil {
				return ChatResult{}, waitErr
			}
			continue
		}

		if retryableStatus(resp.StatusCode) {
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if attempt >= retries {
				return ChatResult{}, fmt.Errorf("provider: stream http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
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
		return ChatResult{}, fmt.Errorf("provider: stream http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var result ChatResult
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
					Content string `json:"content"`
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
			if err := onDelta(choice.Delta.Content); err != nil {
				return ChatResult{}, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return ChatResult{}, err
	}
	return result, nil
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
