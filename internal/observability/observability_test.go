package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cortexgo/cortexgo/internal/core"
)

func TestLangfusePublishesAuthenticatedEvent(t *testing.T) {
	called := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "pk" || p != "sk" {
			t.Error("auth")
		}
		if r.URL.Path != "/api/public/ingestion" {
			t.Error(r.URL.Path)
		}
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	defer s.Close()
	l := &Langfuse{BaseURL: s.URL, PublicKey: "pk", SecretKey: "sk"}
	if err := l.Publish(context.Background(), core.Event{ID: "e", RunID: "r", Type: "run.started", Time: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("not called")
	}
}
