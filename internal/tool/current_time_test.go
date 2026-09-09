package tool

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestCurrentTimeReturnsTimestamp(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(CurrentTime()); err != nil {
		t.Fatal(err)
	}

	output, err := registry.Invoke(context.Background(), "current_time", json.RawMessage(`{"format":"RFC1123"}`))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Format string `json:"format"`
		Time   string `json:"time"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if result.Format != time.RFC1123 {
		t.Fatalf("format = %q", result.Format)
	}
	if _, err := time.Parse(time.RFC1123, result.Time); err != nil {
		t.Fatalf("time = %q: %v", result.Time, err)
	}
}
