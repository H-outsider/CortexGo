package knowledge

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
)

// EmbeddingModel converts text into vectors. Implementations can wrap any model provider.
type EmbeddingModel interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

type VectorResult struct {
	Chunk Chunk
	Score float64
}

type VectorIndex interface {
	Upsert(ctx context.Context, chunks []Chunk, vectors [][]float64) error
	SearchVector(ctx context.Context, vector []float64, limit int) ([]VectorResult, error)
}

type InMemoryVectorIndex struct {
	mu      sync.RWMutex
	entries map[string]vectorEntry
}

type vectorEntry struct {
	chunk  Chunk
	vector []float64
}

func NewInMemoryVectorIndex() *InMemoryVectorIndex {
	return &InMemoryVectorIndex{entries: make(map[string]vectorEntry)}
}

func (i *InMemoryVectorIndex) Upsert(ctx context.Context, chunks []Chunk, vectors [][]float64) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if len(chunks) != len(vectors) {
		return fmt.Errorf("knowledge: chunks and vectors length mismatch")
	}
	for n, vector := range vectors {
		if len(vector) == 0 || norm(vector) == 0 {
			return fmt.Errorf("knowledge: vector %d is empty or zero", n)
		}
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	for n, chunk := range chunks {
		i.entries[chunk.ID] = vectorEntry{chunk: cloneChunk(chunk), vector: append([]float64(nil), vectors[n]...)}
	}
	return nil
}

func (i *InMemoryVectorIndex) SearchVector(ctx context.Context, vector []float64, limit int) ([]VectorResult, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if len(vector) == 0 || norm(vector) == 0 {
		return nil, fmt.Errorf("knowledge: query vector is empty or zero")
	}
	if limit <= 0 {
		limit = 10
	}
	i.mu.RLock()
	results := make([]VectorResult, 0, len(i.entries))
	for _, entry := range i.entries {
		if len(entry.vector) != len(vector) {
			continue
		}
		results = append(results, VectorResult{Chunk: cloneChunk(entry.chunk), Score: cosine(vector, entry.vector)})
	}
	i.mu.RUnlock()
	sort.Slice(results, func(a, b int) bool {
		if results[a].Score == results[b].Score {
			return results[a].Chunk.ID < results[b].Chunk.ID
		}
		return results[a].Score > results[b].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

type HybridIndex struct {
	lexical  *InMemoryIndex
	vector   VectorIndex
	embedder EmbeddingModel
	alpha    float64
}

func NewHybridIndex(embedder EmbeddingModel, alpha float64) (*HybridIndex, error) {
	if embedder == nil {
		return nil, fmt.Errorf("knowledge: embedding model is required")
	}
	if alpha < 0 || alpha > 1 {
		return nil, fmt.Errorf("knowledge: alpha must be between 0 and 1")
	}
	return &HybridIndex{lexical: NewInMemoryIndex(), vector: NewInMemoryVectorIndex(), embedder: embedder, alpha: alpha}, nil
}

func (i *HybridIndex) Add(ctx context.Context, document Document) error {
	if err := i.RemoveDocument(ctx, document.ID); err != nil {
		return err
	}
	chunks, err := SplitDocument(document, ChunkOptions{})
	if err != nil {
		return err
	}
	texts := make([]string, len(chunks))
	for n := range chunks {
		texts[n] = chunks[n].Text
	}
	vectors, err := i.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("knowledge: embed document %q: %w", document.ID, err)
	}
	if err := i.vector.Upsert(ctx, chunks, vectors); err != nil {
		return err
	}
	return i.lexical.Add(ctx, document)
}

func (i *HybridIndex) RemoveDocument(ctx context.Context, documentID string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if remover, ok := i.vector.(DocumentRemover); ok {
		if err := remover.RemoveDocument(ctx, documentID); err != nil {
			return err
		}
	}
	return i.lexical.RemoveDocument(ctx, documentID)
}

func (i *HybridIndex) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	return i.SearchWithFilter(ctx, query, limit, nil)
}

func (i *HybridIndex) SearchWithFilter(ctx context.Context, query string, limit int, filter MetadataFilter) ([]Result, error) {
	queries, err := i.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("knowledge: embed query: %w", err)
	}
	if len(queries) != 1 {
		return nil, fmt.Errorf("knowledge: embedding model returned %d query vectors", len(queries))
	}
	lexical, err := i.lexical.SearchWithFilter(ctx, query, limit, filter)
	if err != nil {
		return nil, err
	}
	vector, err := i.vector.SearchVector(ctx, queries[0], limit)
	if err != nil {
		return nil, err
	}
	scores := make(map[string]float64)
	chunks := make(map[string]Chunk)
	for _, result := range lexical {
		scores[result.Chunk.ID] += (1 - i.alpha) * normalizeScore(result.Score)
		chunks[result.Chunk.ID] = result.Chunk
	}
	for _, result := range vector {
		if !matchesMetadata(result.Chunk.Metadata, filter) {
			continue
		}
		scores[result.Chunk.ID] += i.alpha * ((result.Score + 1) / 2)
		chunks[result.Chunk.ID] = result.Chunk
	}
	results := make([]Result, 0, len(scores))
	for id, score := range scores {
		results = append(results, Result{Chunk: chunks[id], Score: score})
	}
	sort.Slice(results, func(a, b int) bool { return results[a].Score > results[b].Score })
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func norm(vector []float64) float64 {
	var sum float64
	for _, value := range vector {
		sum += value * value
	}
	return math.Sqrt(sum)
}

func cosine(left, right []float64) float64 {
	leftNorm, rightNorm := norm(left), norm(right)
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	var dot float64
	for n := range left {
		dot += left[n] * right[n]
	}
	return dot / (leftNorm * rightNorm)
}

func normalizeScore(score float64) float64 {
	if score <= 0 {
		return 0
	}
	return score / (score + 1)
}
