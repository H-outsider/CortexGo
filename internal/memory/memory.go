package memory

import (
	"context"
	"sync"

	"github.com/cortexgo/cortexgo/internal/provider"
)

// Store 保存和读取会话消息。接口刻意保持简单，便于替换为 PostgreSQL、Redis 或向量记忆。
type Store interface {
	Append(ctx context.Context, sessionID string, messages ...provider.Message) error
	List(ctx context.Context, sessionID string) ([]provider.Message, error)
}

// ReplaceStore optionally supports replacing a session after compaction.
type ReplaceStore interface {
	Store
	Replace(ctx context.Context, sessionID string, messages ...provider.Message) error
}

type InMemory struct {
	mu       sync.RWMutex
	sessions map[string][]provider.Message
}

func NewInMemory() *InMemory { return &InMemory{sessions: make(map[string][]provider.Message)} }

func (m *InMemory) Append(_ context.Context, sessionID string, messages ...provider.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionID] = append(m.sessions[sessionID], messages...)
	return nil
}

func (m *InMemory) List(_ context.Context, sessionID string) ([]provider.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := append([]provider.Message(nil), m.sessions[sessionID]...)
	return result, nil
}

func (m *InMemory) Replace(_ context.Context, sessionID string, messages ...provider.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionID] = append([]provider.Message(nil), messages...)
	return nil
}
