package knowledge

import (
	"context"
	"testing"
)

func TestSplitDocumentPreservesUnicodeAndMetadata(t *testing.T) {
	chunks, err := SplitDocument(Document{ID: "doc-1", Title: "指南", Content: "第一段内容。第二段内容，包含中文。", Metadata: map[string]string{"team": "core"}}, ChunkOptions{MaxCharacters: 10, Overlap: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 || chunks[0].ID != "doc-1:0" || chunks[0].Metadata["team"] != "core" {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}

func TestInMemoryIndexSearchesAndRanksChunks(t *testing.T) {
	index := NewInMemoryIndex()
	if err := index.Add(context.Background(), Document{ID: "doc", Content: "Go 服务支持重试和超时。"}); err != nil {
		t.Fatal(err)
	}
	if err := index.Add(context.Background(), Document{ID: "other", Content: "这是一段无关内容。"}); err != nil {
		t.Fatal(err)
	}
	results, err := index.Search(context.Background(), "服务 重试", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Chunk.DocumentID != "doc" || results[0].Score <= 0 {
		t.Fatalf("unexpected results: %#v", results)
	}
	results[0].Chunk.Metadata = map[string]string{"mutated": "yes"}
	results, _ = index.Search(context.Background(), "服务", 5)
	if _, ok := results[0].Chunk.Metadata["mutated"]; ok {
		t.Fatal("index metadata was not cloned")
	}
}

func TestIndexHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewInMemoryIndex().Add(ctx, Document{ID: "doc", Content: "text"}); err == nil {
		t.Fatal("expected canceled context error")
	}
}
