package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOpenAICompatibleChatUsesOpenAIWireFormat(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"response-model",
			"choices":[{"message":{"role":"assistant","content":"你好"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}
		}`))
	}))
	defer server.Close()

	got, err := OpenAICompatible{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"}.Chat(
		context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "你好" {
		t.Fatalf("response = %#v", got)
	}
	if got.Model != "response-model" || got.FinishReason != "stop" {
		t.Fatalf("unexpected metadata: %#v", got)
	}
	if got.Usage != (Usage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10}) {
		t.Fatalf("unexpected usage: %#v", got.Usage)
	}
	if requestBody["model"] != "test-model" {
		t.Errorf("model = %#v", requestBody["model"])
	}
	messages, ok := requestBody["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v", requestBody["messages"])
	}
	encoded, _ := json.Marshal(messages[0])
	var message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(encoded, &message); err != nil {
		t.Fatal(err)
	}
	if message.Role != "user" || message.Content != "hi" {
		t.Fatalf("message wire format = %s", encoded)
	}
}

func TestOpenAICompatibleChatRetriesServerErrors(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	got, err := OpenAICompatible{
		BaseURL:      server.URL,
		Model:        "test-model",
		MaxRetries:   1,
		RetryBackoff: time.Nanosecond,
	}.Chat(
		context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "ok" || requests != 2 {
		t.Fatalf("response = %#v, requests = %d", got, requests)
	}
}

func TestOpenAICompatibleChatStreamConsumesDeltas(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"model\":\"response-model\",\"choices\":[{\"delta\":{\"content\":\"好\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	var output strings.Builder
	result, err := OpenAICompatible{BaseURL: server.URL, Model: "test-model"}.ChatStream(
		context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
		func(delta string) error {
			output.WriteString(delta)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != "你好" || result.Content != "" {
		t.Fatalf("stream output = %q", output.String())
	}
	if result.Model != "response-model" || result.FinishReason != "stop" {
		t.Fatalf("unexpected stream metadata: %#v", result)
	}
	if result.Usage != (Usage{PromptTokens: 7, CompletionTokens: 2, TotalTokens: 9}) {
		t.Fatalf("unexpected stream usage: %#v", result.Usage)
	}
	if requestBody["stream"] != true {
		t.Fatalf("stream was not requested: %#v", requestBody)
	}
	streamOptions, ok := requestBody["stream_options"].(map[string]any)
	if !ok || streamOptions["include_usage"] != true {
		t.Fatalf("stream options = %#v", requestBody["stream_options"])
	}
}

func TestOpenAICompatibleChatStreamRetriesServerErrors(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	var output strings.Builder
	_, err := OpenAICompatible{
		BaseURL:      server.URL,
		Model:        "test-model",
		MaxRetries:   1,
		RetryBackoff: time.Nanosecond,
	}.ChatStream(
		context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
		func(delta string) error {
			output.WriteString(delta)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != "ok" || requests != 2 {
		t.Fatalf("output = %q, requests = %d", output.String(), requests)
	}
}

func TestOpenAICompatibleChatHonorsRequestTimeout(t *testing.T) {
	transport := &blockingRoundTripper{started: make(chan struct{})}

	_, err := OpenAICompatible{
		BaseURL:        "https://unit.test",
		Model:          "test-model",
		RequestTimeout: time.Millisecond,
		HTTPClient:     &http.Client{Transport: transport},
	}.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

type blockingRoundTripper struct {
	started chan struct{}
	once    sync.Once
}

func (b *blockingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	b.once.Do(func() { close(b.started) })
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestOpenAICompatibleRequiresConfiguration(t *testing.T) {
	if _, err := (OpenAICompatible{}).Chat(context.Background(), nil); err == nil {
		t.Fatal("expected Chat configuration error")
	}
	if _, err := (OpenAICompatible{}).ChatStream(context.Background(), nil, nil); err == nil {
		t.Fatal("expected ChatStream configuration error")
	}
}
