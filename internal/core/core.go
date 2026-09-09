// Package core contains the transport-neutral execution primitives shared by
// agents, background workers and API adapters.
package core

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cortexgo/cortexgo/internal/tool"
)

type Task interface {
	ID() string
	Kind() string
	Execute(context.Context, *Run) error
}

type Run struct {
	ID         string
	TaskID     string
	TenantID   string
	Status     string
	StartedAt  time.Time
	FinishedAt time.Time
	Metadata   map[string]string
}

type Event struct {
	ID      string
	RunID   string
	TaskID  string
	Type    string
	Time    time.Time
	Payload json.RawMessage
}

type EventSink interface {
	Publish(context.Context, Event) error
}

type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage
	Execute(context.Context, json.RawMessage) (json.RawMessage, error)
}

type DefinitionTool struct{ Definition tool.Definition }

func (t DefinitionTool) Name() string                 { return t.Definition.Name }
func (t DefinitionTool) Description() string          { return t.Definition.Description }
func (t DefinitionTool) InputSchema() json.RawMessage { return t.Definition.InputSchema }
func (t DefinitionTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	return t.Definition.Handler(ctx, input)
}

type MemoryEventSink struct{ Events []Event }

func (s *MemoryEventSink) Publish(_ context.Context, event Event) error {
	s.Events = append(s.Events, event)
	return nil
}

type ChannelEventSink struct{ Events chan Event }

func NewChannelEventSink(size int) *ChannelEventSink {
	if size < 1 {
		size = 1
	}
	return &ChannelEventSink{Events: make(chan Event, size)}
}
func (s *ChannelEventSink) Publish(ctx context.Context, event Event) error {
	select {
	case s.Events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
