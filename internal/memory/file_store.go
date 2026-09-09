package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/cortexgo/cortexgo/internal/provider"
)

// FileStore persists conversation sessions as a single JSON document.
// It is intended for local development; multi-process locking is not provided.
type FileStore struct {
	mu       sync.RWMutex
	path     string
	sessions map[string][]provider.Message
}

func OpenFileStore(path string) (*FileStore, error) {
	if path == "" {
		return nil, fmt.Errorf("memory: file path is required")
	}
	store := &FileStore{path: path, sessions: make(map[string][]provider.Message)}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory: read store: %w", err)
	}
	if err := json.Unmarshal(data, &store.sessions); err != nil {
		return nil, fmt.Errorf("memory: decode store: %w", err)
	}
	return store, nil
}

func (s *FileStore) Append(ctx context.Context, sessionID string, messages ...provider.Message) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if sessionID == "" {
		return fmt.Errorf("memory: session ID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = append(s.sessions[sessionID], cloneMessages(messages)...)
	return s.persistLocked()
}

func (s *FileStore) List(ctx context.Context, sessionID string) ([]provider.Message, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneMessages(s.sessions[sessionID]), nil
}

func (s *FileStore) Delete(ctx context.Context, sessionID string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
	return s.persistLocked()
}

func (s *FileStore) persistLocked() error {
	data, err := json.MarshalIndent(s.sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("memory: encode store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("memory: create store directory: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("memory: write store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("memory: commit store: %w", err)
	}
	return nil
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

func cloneMessages(messages []provider.Message) []provider.Message {
	if messages == nil {
		return nil
	}
	clone := make([]provider.Message, len(messages))
	for n, message := range messages {
		clone[n] = message
		if message.ToolCalls != nil {
			clone[n].ToolCalls = append([]provider.ToolCall(nil), message.ToolCalls...)
		}
	}
	return clone
}
