package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cortexgo/cortexgo/internal/agent"
	"github.com/cortexgo/cortexgo/internal/memory"
	"github.com/cortexgo/cortexgo/internal/provider"
)

func TestHealthAndChat(t *testing.T) {
	h := NewServer(agent.New(provider.Echo{}, memory.NewInMemory())).Handler()
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"status":"ok"`) {
		t.Fatalf("health: %d %s", r.Code, r.Body)
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"session_id":"s1","input":"hello"}`)))
	if r.Code != http.StatusOK {
		t.Fatalf("chat: %d %s", r.Code, r.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["object"] != "chat.completion" {
		t.Fatalf("response=%v", got)
	}
}

func TestChatValidation(t *testing.T) {
	h := NewServer(agent.New(provider.Echo{}, memory.NewInMemory())).Handler()
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(`{}`)))
	if r.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", r.Code, r.Body)
	}
}

func TestTenantRBACAndRateLimit(t *testing.T) {
	s := NewServer(agent.New(provider.Echo{}, memory.NewInMemory()))
	s.ResolveTenant = HeaderTenantResolver
	s.Limiter = NewSlidingWindowLimiter(1, time.Minute)
	h := s.Handler()
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(`{"input":"hello"}`))
		req.Header.Set("X-Tenant-ID", "acme")
		h.ServeHTTP(r, req)
		return r
	}
	if r := request(); r.Code != http.StatusOK {
		t.Fatalf("allowed request: %d %s", r.Code, r.Body)
	}
	if r := request(); r.Code != http.StatusTooManyRequests || r.Header().Get("Retry-After") == "" {
		t.Fatalf("limited request: %d headers=%v", r.Code, r.Header())
	}
}
