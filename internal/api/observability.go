package api

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type contextKey string

const requestIDKey contextKey = "request_id"

func RequestID(ctx context.Context) string { v, _ := ctx.Value(requestIDKey).(string); return v }
func newID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e == nil {
		return fmt.Sprintf("%x", b)
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

type Metrics struct{ Requests, Errors, InFlight uint64 }

func (m *Metrics) Handler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "cortexgo_http_requests_total %d\ncortexgo_http_errors_total %d\ncortexgo_http_inflight %d\n", atomic.LoadUint64(&m.Requests), atomic.LoadUint64(&m.Errors), atomic.LoadUint64(&m.InFlight))
}
func (m *Metrics) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&m.Requests, 1)
		atomic.AddUint64(&m.InFlight, 1)
		defer atomic.AddUint64(&m.InFlight, ^uint64(0))
		next.ServeHTTP(w, r)
	})
}
func requestMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 128 || strings.ContainsAny(id, "\r\n \t") {
			id = newID()
		}
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Trace-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key, X-Tenant-ID, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r.Header.Get("Accept-Encoding")) || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Del("Content-Length")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(gzipResponseWriter{ResponseWriter: w, Writer: gz}, r)
	})
}
func acceptsGzip(v string) bool {
	for _, part := range strings.Split(v, ",") {
		p := strings.Split(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(p[0]), "gzip") {
			for _, x := range p[1:] {
				if strings.TrimSpace(x) == "q=0" || strings.TrimSpace(x) == "q=0.0" {
					return false
				}
			}
			return true
		}
	}
	return false
}

type gzipResponseWriter struct {
	http.ResponseWriter
	Writer *gzip.Writer
}

func (w gzipResponseWriter) Flush() {
	_ = w.Writer.Flush()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w gzipResponseWriter) Write(p []byte) (int, error) { return w.Writer.Write(p) }
