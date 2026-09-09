package knowledge

import (
	"context"
	"fmt"
)

type MilvusHit struct {
	ID         string
	DocumentID string
	Text       string
	Title      string
	Metadata   map[string]string
	Score      float64
}
type MilvusClient interface {
	Upsert(context.Context, string, []Chunk, [][]float64) error
	Delete(context.Context, string, string) error
	Search(context.Context, string, []float64, int, MetadataFilter) ([]MilvusHit, error)
	Count(context.Context, string) (int, error)
}

// MilvusVectorStore adapts a Milvus SDK client to the repository's VectorDatabase contract.
type MilvusVectorStore struct {
	client     MilvusClient
	collection string
}

func NewMilvusVectorStore(client MilvusClient, collection string) (*MilvusVectorStore, error) {
	if client == nil || collection == "" {
		return nil, fmt.Errorf("knowledge: milvus client and collection are required")
	}
	return &MilvusVectorStore{client: client, collection: collection}, nil
}
func (s *MilvusVectorStore) Upsert(ctx context.Context, chunks []Chunk, vectors [][]float64) error {
	if len(chunks) != len(vectors) {
		return fmt.Errorf("knowledge: chunks and vectors length mismatch")
	}
	return s.client.Upsert(ctx, s.collection, chunks, vectors)
}
func (s *MilvusVectorStore) SearchVector(ctx context.Context, vector []float64, limit int) ([]VectorResult, error) {
	return s.SearchVectorWithFilter(ctx, vector, limit, nil)
}
func (s *MilvusVectorStore) SearchVectorWithFilter(ctx context.Context, vector []float64, limit int, filter MetadataFilter) ([]VectorResult, error) {
	hits, err := s.client.Search(ctx, s.collection, vector, limit, filter)
	if err != nil {
		return nil, err
	}
	out := make([]VectorResult, len(hits))
	for i, h := range hits {
		out[i] = VectorResult{Chunk: Chunk{ID: h.ID, DocumentID: h.DocumentID, Text: h.Text, Title: h.Title, Metadata: cloneMetadata(h.Metadata)}, Score: h.Score}
	}
	return out, nil
}
func (s *MilvusVectorStore) RemoveDocument(ctx context.Context, documentID string) error {
	return s.client.Delete(ctx, s.collection, documentID)
}
func (s *MilvusVectorStore) Count(ctx context.Context) int {
	n, err := s.client.Count(ctx, s.collection)
	if err != nil {
		return 0
	}
	return n
}
