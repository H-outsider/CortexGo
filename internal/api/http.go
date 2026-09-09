// Package api exposes the HTTP interface for CortexGo agents.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cortexgo/cortexgo/internal/agent"
	"github.com/cortexgo/cortexgo/internal/security"
)

type Server struct {
	Agent         *agent.Agent
	MaxBodyBytes  int64
	ResolveTenant TenantResolver
	Authorizer    Authorizer
	Limiter       *SlidingWindowLimiter
	APIKeys       *security.APIKeyStore
	JWT           *security.JWTVerifier
	Metrics       *Metrics
	Logger        func(requestID, method, path string, status int, duration time.Duration)
}

func NewServer(a *agent.Agent) *Server { return &Server{Agent: a, MaxBodyBytes: 1 << 20} }
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/v1/chat/completions", s.chat)
	mux.HandleFunc("/chat", s.chat)
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if s.Metrics == nil {
			writeJSON(w, 404, map[string]string{"error": "metrics disabled"})
			return
		}
		s.Metrics.Handler(w, r)
	})
	mux.HandleFunc("/openapi.json", openAPI)
	base := withJSON(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			mux.ServeHTTP(w, r)
			return
		}
		tenant := Tenant{ID: "default"}
		authenticated := false
		if s.APIKeys != nil || s.JWT != nil {
			var id security.Identity
			var ok bool
			if key := r.Header.Get("X-API-Key"); key != "" && s.APIKeys != nil {
				id, ok = s.APIKeys.Authenticate(key)
			} else if bearer, e := security.ParseBearer(r.Header.Get("Authorization")); e == nil && s.JWT != nil {
				id, e = s.JWT.Verify(bearer)
				ok = e == nil
			}
			if !ok || id.Tenant == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "valid API key or JWT is required"})
				return
			}
			tenant = Tenant{ID: id.Tenant, Roles: id.Roles}
			authenticated = true
		}
		if s.ResolveTenant != nil {
			resolved, rok := s.ResolveTenant(r)
			if authenticated {
				if rok && resolved.ID != tenant.ID {
					writeJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
					return
				}
			} else {
				tenant = resolved
			}
			ok := true
			if !authenticated {
				ok = rok
			}
			if !ok || tenant.ID == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "tenant is required"})
				return
			}
		}
		if s.Authorizer != nil && !s.Authorizer.Allow(tenant, "chat") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "permission denied"})
			return
		}
		if s.Limiter != nil {
			if ok, retry := s.Limiter.Allow(tenant.ID); !ok {
				if retry < 0 {
					retry = 0
				}
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retry.Seconds())+1))
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
				return
			}
		}
		r = r.WithContext(withTenant(r.Context(), tenant))
		start := time.Now()
		mux.ServeHTTP(w, r)
		if s.Logger != nil {
			s.Logger(RequestID(r.Context()), r.Method, r.URL.Path, 200, time.Since(start))
		}
	}))
	base = requestMiddleware(base)
	base = corsMiddleware(base)
	base = gzipMiddleware(base)
	if s.Metrics != nil {
		base = s.Metrics.middleware(base)
	}
	return base
}

func withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path != "/healthz" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type chatRequest struct {
	SessionID string `json:"session_id"`
	Input     string `json:"input"`
	Stream    bool   `json:"stream"`
	Model     string `json:"model"`
	Messages  []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.Agent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent is not configured"})
		return
	}
	limit := s.MaxBodyBytes
	if limit <= 0 {
		limit = 1 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	defer r.Body.Close()
	var req chatRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		status := http.StatusBadRequest
		if err == io.EOF {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request must contain exactly one JSON object"})
		return
	}
	req.Input = security.SanitizePrompt(req.Input)
	if strings.TrimSpace(req.Input) == "" {
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				req.Input = req.Messages[i].Content
				break
			}
		}
	}
	req.Input = security.SanitizePrompt(req.Input)
	if strings.TrimSpace(req.Input) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "input or a user message is required"})
		return
	}
	if req.SessionID == "" {
		req.SessionID = "default"
	}
	if tenant, ok := TenantFromContext(r.Context()); ok && tenant.ID != "default" {
		req.SessionID = tenant.ID + ":" + req.SessionID
	}
	if req.Stream {
		s.stream(w, r, req)
		return
	}
	result, err := s.Agent.ChatWithResult(r.Context(), req.SessionID, req.Input)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, completionResponse(req.Model, result, req.Input))
}
func completionResponse(model string, result agent.ChatResult, input string) map[string]any {
	if model == "" {
		model = result.Model
	}
	if model == "" {
		model = "cortexgo"
	}
	return map[string]any{"id": fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()), "object": "chat.completion", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": result.Content}, "finish_reason": result.FinishReason}}, "usage": result.Usage}
}
func (s *Server) stream(w http.ResponseWriter, r *http.Request, req chatRequest) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	enc := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); fl.Flush() }
	_, err := s.Agent.ChatStreamWithResult(r.Context(), req.SessionID, req.Input, func(delta string) error {
		enc(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": delta}, "index": 0}}})
		return nil
	})
	if err != nil {
		enc(map[string]string{"error": err.Error()})
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	fl.Flush()
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Serve starts the server and returns when ctx is cancelled or the server fails.
func Serve(ctx context.Context, srv *http.Server) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// ServeTLS starts an HTTPS server and shuts it down when ctx is cancelled.
func ServeTLS(ctx context.Context, srv *http.Server, certFile, keyFile string) error {
	errC := make(chan error, 1)
	go func() { errC <- srv.ListenAndServeTLS(certFile, keyFile) }()
	select {
	case err := <-errC:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}
