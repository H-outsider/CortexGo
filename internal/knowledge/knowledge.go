package knowledge

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
)

type Document struct {
	ID       string
	Title    string
	Content  string
	Metadata map[string]string
}

type Chunk struct {
	ID         string
	DocumentID string
	Title      string
	Text       string
	Ordinal    int
	Metadata   map[string]string
}

type ChunkOptions struct {
	MaxCharacters int
	Overlap       int
}

func SplitDocument(document Document, options ChunkOptions) ([]Chunk, error) {
	if strings.TrimSpace(document.ID) == "" {
		return nil, fmt.Errorf("knowledge: document ID is required")
	}
	if strings.TrimSpace(document.Content) == "" {
		return nil, fmt.Errorf("knowledge: document content is required")
	}
	max := options.MaxCharacters
	if max <= 0 {
		max = 1200
	}
	overlap := options.Overlap
	if overlap < 0 || overlap >= max {
		return nil, fmt.Errorf("knowledge: overlap must be between 0 and max characters")
	}
	runes := []rune(strings.TrimSpace(document.Content))
	chunks := make([]Chunk, 0, (len(runes)+max-1)/max)
	for start, ordinal := 0, 0; start < len(runes); ordinal++ {
		end := start + max
		if end > len(runes) {
			end = len(runes)
		}
		if end < len(runes) {
			if boundary := findBoundary(runes, start, end); boundary > start {
				end = boundary
			}
		}
		text := strings.TrimSpace(string(runes[start:end]))
		if text != "" {
			chunks = append(chunks, Chunk{
				ID: fmt.Sprintf("%s:%d", document.ID, ordinal), DocumentID: document.ID,
				Title: document.Title, Text: text, Ordinal: ordinal,
				Metadata: cloneMetadata(document.Metadata),
			})
		}
		if end == len(runes) {
			break
		}
		start = end - overlap
	}
	return chunks, nil
}

func findBoundary(text []rune, start, end int) int {
	for i := end; i > start+end/4; i-- {
		if unicode.IsSpace(text[i-1]) || strings.ContainsRune("。！？.!?", text[i-1]) {
			return i
		}
	}
	return end
}

type Result struct {
	Chunk Chunk
	Score float64
}

type Index interface {
	Add(ctx context.Context, document Document) error
	Search(ctx context.Context, query string, limit int) ([]Result, error)
}

// MetadataFilter matches chunks whose metadata contains all requested values.
type MetadataFilter map[string]string

type FilteredIndex interface {
	SearchWithFilter(ctx context.Context, query string, limit int, filter MetadataFilter) ([]Result, error)
}

type DocumentRemover interface {
	RemoveDocument(ctx context.Context, documentID string) error
}

type InMemoryIndex struct {
	mu     sync.RWMutex
	chunks map[string]Chunk
}

func NewInMemoryIndex() *InMemoryIndex {
	return &InMemoryIndex{chunks: make(map[string]Chunk)}
}

func (i *InMemoryIndex) Add(ctx context.Context, document Document) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	chunks, err := SplitDocument(document, ChunkOptions{})
	if err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, chunk := range chunks {
		if err := i.addChunkLocked(chunk); err != nil {
			return err
		}
	}
	return nil
}

func (i *InMemoryIndex) addChunk(chunk Chunk) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.addChunkLocked(chunk)
}

func (i *InMemoryIndex) addChunkLocked(chunk Chunk) error {
	if chunk.ID == "" || strings.TrimSpace(chunk.Text) == "" {
		return fmt.Errorf("knowledge: chunk ID and text are required")
	}
	i.chunks[chunk.ID] = cloneChunk(chunk)
	return nil
}

func (i *InMemoryIndex) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	return i.SearchWithFilter(ctx, query, limit, nil)
}

func (i *InMemoryIndex) SearchWithFilter(ctx context.Context, query string, limit int, filter MetadataFilter) ([]Result, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil, fmt.Errorf("knowledge: query is empty")
	}
	if limit <= 0 {
		limit = 10
	}
	i.mu.RLock()
	results := make([]Result, 0, len(i.chunks))
	for _, chunk := range i.chunks {
		if !matchesMetadata(chunk.Metadata, filter) {
			continue
		}
		score := scoreChunk(terms, chunk.Text)
		if score > 0 {
			results = append(results, Result{Chunk: cloneChunk(chunk), Score: score})
		}
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

func (i *InMemoryIndex) RemoveDocument(ctx context.Context, documentID string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	for id, chunk := range i.chunks {
		if chunk.DocumentID == documentID {
			delete(i.chunks, id)
		}
	}
	return nil
}

func matchesMetadata(metadata map[string]string, filter MetadataFilter) bool {
	for key, value := range filter {
		if metadata == nil || metadata[key] != value {
			return false
		}
	}
	return true
}

func tokenize(text string) []string {
	var tokens []string
	var word []rune
	var han []rune
	flush := func() {
		if len(word) > 0 {
			tokens = append(tokens, string(word))
			word = word[:0]
		}
	}
	flushHan := func() {
		if len(han) == 0 {
			return
		}
		tokens = append(tokens, string(han))
		for _, r := range han {
			tokens = append(tokens, string(r))
		}
		for i := 0; i+1 < len(han); i++ {
			tokens = append(tokens, string(han[i:i+2]))
		}
		han = han[:0]
	}
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.Is(unicode.Han, r):
			flush()
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushHan()
			word = append(word, r)
		default:
			flushHan()
			flush()
		}
	}
	flushHan()
	flush()
	return tokens
}

func scoreChunk(terms []string, text string) float64 {
	tokens := tokenize(text)
	counts := make(map[string]int, len(tokens))
	for _, token := range tokens {
		counts[token]++
	}
	var score float64
	for _, term := range terms {
		if counts[term] > 0 {
			tf := float64(counts[term])
			// BM25-style term-frequency saturation (IDF is unavailable in the in-memory index).
			score += (tf * 2.2) / (tf + 1.2)
		}
	}
	return score
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}
	clone := make(map[string]string, len(metadata))
	for key, value := range metadata {
		clone[key] = value
	}
	return clone
}

func cloneChunk(chunk Chunk) Chunk {
	chunk.Metadata = cloneMetadata(chunk.Metadata)
	return chunk
}
