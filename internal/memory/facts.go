package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	Sensitive bool      `json:"sensitive,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

type FactPrivacyPolicy struct {
	AllowSensitive bool
	MaxValueBytes  int
	DefaultTTL     time.Duration
}

type FactStore interface {
	Upsert(ctx context.Context, fact Fact) error
	List(ctx context.Context, userID string) ([]Fact, error)
	Forget(ctx context.Context, userID, key string) error
}

type UserForgetStore interface {
	FactStore
	ForgetUser(ctx context.Context, userID string) error
}

// InMemoryFactStore provides deterministic, concurrency-safe long-term memory for local use.
type InMemoryFactStore struct {
	mu     sync.RWMutex
	facts  map[string]Fact
	policy FactPrivacyPolicy
}

func NewInMemoryFactStore() *InMemoryFactStore {
	return NewInMemoryFactStoreWithPolicy(FactPrivacyPolicy{})
}

func NewInMemoryFactStoreWithPolicy(policy FactPrivacyPolicy) *InMemoryFactStore {
	return &InMemoryFactStore{facts: make(map[string]Fact), policy: policy}
}

func (s *InMemoryFactStore) Upsert(ctx context.Context, fact Fact) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fact.UserID + "\x00" + fact.Key
	var existing *Fact
	if value, ok := s.facts[id]; ok {
		existing = &value
	}
	var err error
	fact, err = normalizeFact(fact, s.policy, existing, now)
	if err != nil {
		return err
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
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Fact, 0)
	now := time.Now().UTC()
	for id, fact := range s.facts {
		if isExpired(fact, now) {
			delete(s.facts, id)
			continue
		}
		if fact.Sensitive && !s.policy.AllowSensitive {
			continue
		}
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

func (s *InMemoryFactStore) ForgetUser(ctx context.Context, userID string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, fact := range s.facts {
		if fact.UserID == userID {
			delete(s.facts, id)
		}
	}
	return nil
}

func validateFact(fact Fact, policy FactPrivacyPolicy) error {
	if fact.UserID == "" || fact.Key == "" {
		return fmt.Errorf("memory: fact user ID and key are required")
	}
	if fact.Sensitive && !policy.AllowSensitive {
		return fmt.Errorf("memory: sensitive facts are disabled by policy")
	}
	if policy.MaxValueBytes > 0 && len([]byte(fact.Value)) > policy.MaxValueBytes {
		return fmt.Errorf("memory: fact value exceeds privacy policy limit")
	}
	return nil
}

func normalizeFact(fact Fact, policy FactPrivacyPolicy, existing *Fact, now time.Time) (Fact, error) {
	if err := validateFact(fact, policy); err != nil {
		return Fact{}, err
	}
	if existing != nil {
		fact.CreatedAt = existing.CreatedAt
		if fact.ExpiresAt.IsZero() {
			fact.ExpiresAt = existing.ExpiresAt
		}
	}
	if fact.ExpiresAt.IsZero() && policy.DefaultTTL > 0 {
		fact.ExpiresAt = now.Add(policy.DefaultTTL)
	}
	return fact, nil
}

func isExpired(fact Fact, now time.Time) bool {
	return !fact.ExpiresAt.IsZero() && !now.Before(fact.ExpiresAt)
}

// FileFactStore persists user facts as JSON with restrictive file permissions.
type FileFactStore struct {
	mu     sync.Mutex
	path   string
	facts  map[string]Fact
	policy FactPrivacyPolicy
}

func OpenFileFactStore(path string) (*FileFactStore, error) {
	return OpenFileFactStoreWithPolicy(path, FactPrivacyPolicy{})
}

func OpenFileFactStoreWithPolicy(path string, policy FactPrivacyPolicy) (*FileFactStore, error) {
	s := &FileFactStore{path: path, facts: make(map[string]Fact), policy: policy}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory: read fact store: %w", err)
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.facts); err != nil {
		return nil, fmt.Errorf("memory: decode fact store: %w", err)
	}
	return s, nil
}

func (s *FileFactStore) Upsert(ctx context.Context, fact Fact) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fact.UserID + "\x00" + fact.Key
	var existing *Fact
	if value, ok := s.facts[id]; ok {
		existing = &value
	}
	var err error
	fact, err = normalizeFact(fact, s.policy, existing, now)
	if err != nil {
		return err
	}
	if fact.CreatedAt.IsZero() {
		fact.CreatedAt = now
	}
	fact.UpdatedAt = now
	s.facts[id] = fact
	return s.persistLocked()
}

func (s *FileFactStore) List(ctx context.Context, userID string) ([]Fact, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Fact, 0)
	changed := false
	now := time.Now().UTC()
	for id, fact := range s.facts {
		if isExpired(fact, now) {
			delete(s.facts, id)
			changed = true
			continue
		}
		if fact.Sensitive && !s.policy.AllowSensitive {
			continue
		}
		if fact.UserID == userID {
			result = append(result, fact)
		}
	}
	if changed {
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
	}
	sort.Slice(result, func(a, b int) bool { return result[a].Key < result[b].Key })
	return result, nil
}

func (s *FileFactStore) Forget(ctx context.Context, userID, key string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.facts, userID+"\x00"+key)
	return s.persistLocked()
}
func (s *FileFactStore) ForgetUser(ctx context.Context, userID string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, fact := range s.facts {
		if fact.UserID == userID {
			delete(s.facts, id)
		}
	}
	return s.persistLocked()
}

func (s *FileFactStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("memory: create fact store directory: %w", err)
	}
	data, err := json.MarshalIndent(s.facts, "", "  ")
	if err != nil {
		return fmt.Errorf("memory: encode fact store: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".facts-*")
	if err != nil {
		return fmt.Errorf("memory: create fact store temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("memory: secure fact store temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("memory: write fact store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("memory: close fact store: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("memory: replace fact store: %w", err)
	}
	return nil
}
