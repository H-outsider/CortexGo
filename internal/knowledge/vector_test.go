package knowledge

import (
	"context"
	"path/filepath"
	"testing"
)

type testEmbedder struct{}

func (testEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for n, text := range texts {
		if len(text) > 0 && text[0] == 'A' {
			result[n] = []float64{1, 0}
		} else {
			result[n] = []float64{0, 1}
		}
	}
	return result, nil
}

func TestFileVectorIndexPersistsAcrossOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.json")
	index, err := OpenFileVectorIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	chunk := Chunk{ID: "doc:0", DocumentID: "doc", Text: "persisted"}
	if err := index.Upsert(context.Background(), []Chunk{chunk}, [][]float64{{1, 0}}); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFileVectorIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	results, err := reopened.SearchVector(context.Background(), []float64{1, 0}, 1)
	if err != nil || len(results) != 1 || results[0].Chunk.Text != "persisted" {
		t.Fatalf("results=%#v err=%v", results, err)
	}
}

func TestInMemoryVectorIndexSearchesByCosine(t *testing.T) {
	index := NewInMemoryVectorIndex()
	chunks := []Chunk{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}}
	if err := index.Upsert(context.Background(), chunks, [][]float64{{1, 0}, {0, 1}}); err != nil {
		t.Fatal(err)
	}
	results, err := index.SearchVector(context.Background(), []float64{0.9, 0.1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Chunk.ID != "a" {
		t.Fatalf("unexpected vector results: %#v", results)
	}
}

func TestVectorDatabaseFiltersAndRemovesDocuments(t *testing.T) {
	db := NewInMemoryVectorIndex()
	ctx := context.Background()
	chunks := []Chunk{{ID: "a:0", DocumentID: "a", Text: "a", Metadata: map[string]string{"tenant": "one"}}, {ID: "b:0", DocumentID: "b", Text: "b", Metadata: map[string]string{"tenant": "two"}}}
	if err := db.Upsert(ctx, chunks, [][]float64{{1, 0}, {1, 0}}); err != nil {
		t.Fatal(err)
	}
	got, err := db.SearchVectorWithFilter(ctx, []float64{1, 0}, 10, MetadataFilter{"tenant": "one"})
	if err != nil || len(got) != 1 {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if db.Count(ctx) != 2 {
		t.Fatal("unexpected count")
	}
	if err := db.RemoveDocument(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if db.Count(ctx) != 1 {
		t.Fatal("document was not removed")
	}
}

func TestHybridIndexCombinesLexicalAndVectorSearch(t *testing.T) {
	index, err := NewHybridIndex(testEmbedder{}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Add(context.Background(), Document{ID: "a", Content: "Apple service"}); err != nil {
		t.Fatal(err)
	}
	if err := index.Add(context.Background(), Document{ID: "b", Content: "Banana service"}); err != nil {
		t.Fatal(err)
	}
	results, err := index.Search(context.Background(), "Apple", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].Chunk.DocumentID != "a" {
		t.Fatalf("unexpected hybrid results: %#v", results)
	}
}
