package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Fact is a user-scoped long-term memory item. Values should not contain secrets.
type Fact struct {
	UserID    string    `json:"user_id"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type FactStore interface {
	Upsert(ctx context.Context, fact Fact) error
	List(ctx context.Context, userID string) ([]Fact, error)
	Forget(ctx context.Context, userID, key string) error
}

// InMemoryFactStore provides deterministic, concurrency-safe long-term memory for local use.
type InMemoryFactStore struct {
	mu    sync.RWMutex
	facts map[string]Fact
}

func NewInMemoryFactStore() *InMemoryFactStore {
	return &InMemoryFactStore{facts: make(map[string]Fact)}
}

func (s *InMemoryFactStore) Upsert(ctx context.Context, fact Fact) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if fact.UserID == "" || fact.Key == "" {
		return fmt.Errorf("memory: fact user ID and key are required")
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fact.UserID + "\x00" + fact.Key
	if existing, ok := s.facts[id]; ok {
		fact.CreatedAt = existing.CreatedAt
	}
	if fact.CreatedAt.IsZero() {
		fact.CreatedAt = now
	}
	fact.UpdatedAt = now
	s.facts[id] = fact
	return nil
}

func (s *InMemoryFactStore) List(ctx context.Context, userID string) ([]Fact, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Fact, 0)
	for _, fact := range s.facts {
		if fact.UserID == userID {
			result = append(result, fact)
		}
	}
	sort.Slice(result, func(a, b int) bool { return result[a].Key < result[b].Key })
	return result, nil
}

func (s *InMemoryFactStore) Forget(ctx context.Context, userID, key string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.facts, userID+"\x00"+key)
	return nil
}
