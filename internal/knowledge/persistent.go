package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type FileVectorIndex struct {
	mu      sync.RWMutex
	path    string
	entries map[string]vectorEntry
}

type persistedVectorEntry struct {
	Chunk  Chunk     `json:"chunk"`
	Vector []float64 `json:"vector"`
}

func OpenFileVectorIndex(path string) (*FileVectorIndex, error) {
	if path == "" {
		return nil, fmt.Errorf("knowledge: vector index path is required")
	}
	index := &FileVectorIndex{path: path, entries: make(map[string]vectorEntry)}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return index, nil
	}
	if err != nil {
		return nil, fmt.Errorf("knowledge: read vector index: %w", err)
	}
	var stored map[string]persistedVectorEntry
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("knowledge: decode vector index: %w", err)
	}
	for id, entry := range stored {
		if len(entry.Vector) == 0 || entry.Chunk.ID == "" {
			return nil, fmt.Errorf("knowledge: invalid persisted entry %q", id)
		}
		index.entries[id] = vectorEntry{chunk: cloneChunk(entry.Chunk), vector: append([]float64(nil), entry.Vector...)}
	}
	return index, nil
}

func (i *FileVectorIndex) Upsert(ctx context.Context, chunks []Chunk, vectors [][]float64) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if len(chunks) != len(vectors) {
		return fmt.Errorf("knowledge: chunks and vectors length mismatch")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	for n, chunk := range chunks {
		if len(vectors[n]) == 0 || norm(vectors[n]) == 0 {
			return fmt.Errorf("knowledge: vector %d is empty or zero", n)
		}
		i.entries[chunk.ID] = vectorEntry{chunk: cloneChunk(chunk), vector: append([]float64(nil), vectors[n]...)}
	}
	return i.persistLocked()
}

func (i *FileVectorIndex) SearchVector(ctx context.Context, vector []float64, limit int) ([]VectorResult, error) {
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
		if len(entry.vector) == len(vector) {
			results = append(results, VectorResult{Chunk: cloneChunk(entry.chunk), Score: cosine(vector, entry.vector)})
		}
	}
	i.mu.RUnlock()
	sort.Slice(results, func(a, b int) bool { return results[a].Score > results[b].Score })
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (i *FileVectorIndex) RemoveDocument(ctx context.Context, documentID string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	for id, entry := range i.entries {
		if entry.chunk.DocumentID == documentID {
			delete(i.entries, id)
		}
	}
	return i.persistLocked()
}

func (i *FileVectorIndex) Chunks() []Chunk {
	i.mu.RLock()
	defer i.mu.RUnlock()
	chunks := make([]Chunk, 0, len(i.entries))
	for _, entry := range i.entries {
		chunks = append(chunks, cloneChunk(entry.chunk))
	}
	return chunks
}

func (i *FileVectorIndex) persistLocked() error {
	stored := make(map[string]persistedVectorEntry, len(i.entries))
	for id, entry := range i.entries {
		stored[id] = persistedVectorEntry{Chunk: cloneChunk(entry.chunk), Vector: entry.vector}
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("knowledge: encode vector index: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(i.path), 0o755); err != nil {
		return fmt.Errorf("knowledge: create vector index directory: %w", err)
	}
	tmp := i.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("knowledge: write vector index: %w", err)
	}
	if err := os.Rename(tmp, i.path); err != nil {
		return fmt.Errorf("knowledge: commit vector index: %w", err)
	}
	return nil
}

func NewPersistentHybridIndex(path string, embedder EmbeddingModel, alpha float64) (*HybridIndex, error) {
	vector, err := OpenFileVectorIndex(path)
	if err != nil {
		return nil, err
	}
	if embedder == nil {
		return nil, fmt.Errorf("knowledge: embedding model is required")
	}
	if alpha < 0 || alpha > 1 {
		return nil, fmt.Errorf("knowledge: alpha must be between 0 and 1")
	}
	lexical := NewInMemoryIndex()
	for _, chunk := range vector.Chunks() {
		if err := lexical.addChunk(chunk); err != nil {
			return nil, err
		}
	}
	return &HybridIndex{lexical: lexical, vector: vector, embedder: embedder, alpha: alpha}, nil
}
