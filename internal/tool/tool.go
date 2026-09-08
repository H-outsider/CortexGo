package tool

import (
	"context"
	"encoding/json"
)

type Definition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Handler     func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}

type Registry struct{ tools map[string]Definition }

func NewRegistry() *Registry { return &Registry{tools: make(map[string]Definition)} }

func (r *Registry) Register(def Definition) { r.tools[def.Name] = def }

func (r *Registry) Get(name string) (Definition, bool) {
	def, ok := r.tools[name]
	return def, ok
}
