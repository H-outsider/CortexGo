package knowledge

import (
	"context"
	"testing"
)

type fakeMilvus struct{ chunks []Chunk }

func (f *fakeMilvus) Upsert(_ context.Context, _ string, c []Chunk, _ [][]float64) error {
	f.chunks = c
	return nil
}
func (f *fakeMilvus) Delete(_ context.Context, _ string, id string) error {
	var out []Chunk
	for _, c := range f.chunks {
		if c.DocumentID != id {
			out = append(out, c)
		}
	}
	f.chunks = out
	return nil
}
func (f *fakeMilvus) Search(_ context.Context, _ string, _ []float64, limit int, filter MetadataFilter) ([]MilvusHit, error) {
	var out []MilvusHit
	for _, c := range f.chunks {
		if matchesMetadata(c.Metadata, filter) {
			out = append(out, MilvusHit{ID: c.ID, DocumentID: c.DocumentID, Text: c.Text, Metadata: c.Metadata, Score: 1})
		}
	}
	if len(out) > limit && limit > 0 {
		out = out[:limit]
	}
	return out, nil
}
func (f *fakeMilvus) Count(context.Context, string) (int, error) { return len(f.chunks), nil }
func TestMilvusVectorStoreAdapter(t *testing.T) {
	f := &fakeMilvus{}
	s, err := NewMilvusVectorStore(f, "docs")
	if err != nil {
		t.Fatal(err)
	}
	c := Chunk{ID: "d:0", DocumentID: "d", Text: "x", Metadata: map[string]string{"tenant": "a"}}
	if err := s.Upsert(context.Background(), []Chunk{c}, [][]float64{{1, 0}}); err != nil {
		t.Fatal(err)
	}
	r, err := s.SearchVectorWithFilter(context.Background(), []float64{1, 0}, 5, MetadataFilter{"tenant": "a"})
	if err != nil || len(r) != 1 {
		t.Fatalf("%#v %v", r, err)
	}
	if s.Count(context.Background()) != 1 {
		t.Fatal("count")
	}
	if err := s.RemoveDocument(context.Background(), "d"); err != nil || s.Count(context.Background()) != 0 {
		t.Fatal(err)
	}
}
