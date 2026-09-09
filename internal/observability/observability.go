package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cortexgo/cortexgo/internal/core"
	"github.com/cortexgo/cortexgo/internal/telemetry"
)

type Sink interface {
	core.EventSink
	telemetry.Recorder
}
type MultiSink []Sink

func (m MultiSink) Publish(ctx context.Context, e core.Event) error {
	for _, s := range m {
		if err := s.Publish(ctx, e); err != nil {
			return err
		}
	}
	return nil
}
func (m MultiSink) RecordModel(ctx context.Context, e telemetry.ModelEvent) {
	for _, s := range m {
		s.RecordModel(ctx, e)
	}
}

type Langfuse struct {
	BaseURL, PublicKey, SecretKey string
	HTTPClient                    *http.Client
}
type ingestionEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Body      any       `json:"body"`
}

func (l *Langfuse) Publish(ctx context.Context, e core.Event) error {
	kind := "span-create"
	if e.Type == "run.started" {
		kind = "trace-create"
	}
	return l.send(ctx, ingestionEvent{ID: e.ID, Type: kind, Timestamp: e.Time, Body: map[string]any{"id": e.ID, "traceId": e.RunID, "name": e.Type, "metadata": json.RawMessage(e.Payload)}})
}
func (l *Langfuse) RecordModel(ctx context.Context, e telemetry.ModelEvent) {
	body := map[string]any{"name": e.Operation, "model": e.Model, "startTime": e.StartedAt, "endTime": e.StartedAt.Add(e.Duration), "usage": e.Usage, "cost": e.CostUSD}
	if e.Err != nil {
		body["statusMessage"] = e.Err.Error()
	}
	_ = l.send(ctx, ingestionEvent{ID: fmt.Sprintf("generation-%d", e.StartedAt.UnixNano()), Type: "generation-create", Timestamp: e.StartedAt, Body: body})
}
func (l *Langfuse) send(ctx context.Context, event ingestionEvent) error {
	if l.BaseURL == "" || l.PublicKey == "" || l.SecretKey == "" {
		return fmt.Errorf("observability: langfuse configuration is incomplete")
	}
	data, err := json.Marshal(map[string]any{"batch": []ingestionEvent{event}})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(l.BaseURL, "/")+"/api/public/ingestion", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.SetBasicAuth(l.PublicKey, l.SecretKey)
	req.Header.Set("Content-Type", "application/json")
	client := l.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("observability: langfuse ingestion: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("observability: langfuse ingestion returned %d", resp.StatusCode)
	}
	return nil
}
