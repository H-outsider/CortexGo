package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cortexgo/cortexgo/internal/tool"
)

func TestDefinitionToolAndEvents(t *testing.T) {
	d := tool.Definition{Name: "echo", Description: "echo", InputSchema: json.RawMessage(`{"type":"object"}`), Handler: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true}`), nil
	}}
	result, err := (DefinitionTool{Definition: d}).Execute(context.Background(), nil)
	if err != nil || string(result) != `{"ok":true}` {
		t.Fatalf("result=%s err=%v", result, err)
	}
	sink := &MemoryEventSink{}
	if err := sink.Publish(context.Background(), Event{Type: "run.completed"}); err != nil || len(sink.Events) != 1 {
		t.Fatal(err)
	}
}
