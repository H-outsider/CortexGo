package knowledge

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cortexgo/cortexgo/internal/tool"
)

func TestSearchToolReturnsCitations(t *testing.T) {
	index := NewInMemoryIndex()
	if err := index.Add(context.Background(), Document{ID: "doc-1", Title: "Guide", Content: "CortexGo supports vector search."}); err != nil {
		t.Fatal(err)
	}
	definition := SearchTool(index)
	registry := tool.NewRegistry()
	if err := registry.Register(definition); err != nil {
		t.Fatal(err)
	}
	output, err := registry.Invoke(context.Background(), definition.Name, json.RawMessage(`{"query":"vector search"}`))
	if err != nil {
		t.Fatal(err)
	}
	var results []struct {
		Citation string `json:"citation"`
		Text     string `json:"text"`
	}
	if err := json.Unmarshal(output, &results); err != nil || len(results) != 1 || results[0].Citation != "doc-1:0" {
		t.Fatalf("results=%s err=%v", output, err)
	}
}
