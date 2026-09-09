package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

type Definition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Handler     func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}

type Registry struct {
	mu    sync.RWMutex
	tools map[string]Definition
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Definition)}
}

func (r *Registry) Register(def Definition) error {
	if err := def.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[def.Name]; exists {
		return fmt.Errorf("tool %q is already registered", def.Name)
	}
	r.tools[def.Name] = def
	return nil
}

func (r *Registry) Get(name string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.tools[name]
	return def, ok
}

func (r *Registry) Definitions() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	definitions := make([]Definition, 0, len(names))
	for _, name := range names {
		definitions = append(definitions, r.tools[name])
	}
	return definitions
}

func (r *Registry) Invoke(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	def, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool %q is not registered", name)
	}
	if err := def.ValidateInput(input); err != nil {
		return nil, fmt.Errorf("validate input for tool %q: %w", name, err)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	output, err := safeInvoke(ctx, def.Handler, normalizeInput(input))
	if err != nil {
		return nil, fmt.Errorf("invoke tool %q: %w", name, err)
	}
	if len(output) == 0 {
		output = json.RawMessage("null")
	}
	if !json.Valid(output) {
		return nil, fmt.Errorf("tool %q returned invalid JSON", name)
	}
	return output, nil
}

func safeInvoke(ctx context.Context, handler func(context.Context, json.RawMessage) (json.RawMessage, error), input json.RawMessage) (output json.RawMessage, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			output = nil
			err = fmt.Errorf("handler panicked: %v", recovered)
		}
	}()
	return handler(ctx, input)
}

func (d Definition) Validate() error {
	if !namePattern.MatchString(d.Name) {
		return fmt.Errorf("tool name must match %s", namePattern.String())
	}
	if strings.TrimSpace(d.Description) == "" {
		return fmt.Errorf("tool %q must have a description", d.Name)
	}
	if d.Handler == nil {
		return fmt.Errorf("tool %q must have a handler", d.Name)
	}
	var schema schemaNode
	if err := unmarshalSchema(d.InputSchema, &schema); err != nil {
		return fmt.Errorf("tool %q has an invalid schema: %w", d.Name, err)
	}
	if schema.Type != "object" {
		return fmt.Errorf("tool %q schema root must be type object", d.Name)
	}
	return nil
}

func (d Definition) ValidateInput(input json.RawMessage) error {
	var value any
	if err := unmarshalValue(input, &value); err != nil {
		return err
	}
	return validateValue(d.InputSchema, value, "input")
}

type schemaNode struct {
	Type                 string                     `json:"type"`
	Description          string                     `json:"description"`
	Required             []string                   `json:"required"`
	Properties           map[string]json.RawMessage `json:"properties"`
	AdditionalProperties *bool                      `json:"additionalProperties"`
	Items                json.RawMessage            `json:"items"`
	Enum                 []any                      `json:"enum"`
}

func unmarshalSchema(raw json.RawMessage, schema *schemaNode) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return fmt.Errorf("schema is empty or invalid JSON")
	}
	if err := json.Unmarshal(raw, schema); err != nil {
		return fmt.Errorf("decode schema: %w", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return fmt.Errorf("schema must be an object: %w", err)
	}
	allowed := map[string]bool{
		"type": true, "description": true, "required": true, "properties": true,
		"additionalProperties": true, "items": true, "enum": true,
	}
	for key := range object {
		if !allowed[key] {
			return fmt.Errorf("unsupported schema keyword %q", key)
		}
	}
	for name, property := range schema.Properties {
		var child schemaNode
		if err := unmarshalSchema(property, &child); err != nil {
			return fmt.Errorf("property %q: %w", name, err)
		}
	}
	return nil
}

func unmarshalValue(raw json.RawMessage, value *any) error {
	normalized := normalizeInput(raw)
	if !json.Valid(normalized) {
		return fmt.Errorf("input is invalid JSON")
	}
	if err := json.Unmarshal(normalized, value); err != nil {
		return fmt.Errorf("decode input: %w", err)
	}
	return nil
}

func normalizeInput(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return json.RawMessage("{}")
	}
	return raw
}

func validateValue(rawSchema json.RawMessage, value any, path string) error {
	var schema schemaNode
	if err := unmarshalSchema(rawSchema, &schema); err != nil {
		return err
	}

	if !schemaTypeMatches(schema.Type, value) {
		return fmt.Errorf("%s must be %s", path, schema.Type)
	}
	if len(schema.Enum) > 0 && !enumContains(schema.Enum, value) {
		return fmt.Errorf("%s must be one of the allowed enum values", path)
	}

	switch schema.Type {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		for _, required := range schema.Required {
			if _, exists := object[required]; !exists {
				return fmt.Errorf("%s.%s is required", path, required)
			}
		}
		if schema.AdditionalProperties != nil && !*schema.AdditionalProperties {
			for name := range object {
				if _, defined := schema.Properties[name]; !defined {
					return fmt.Errorf("%s.%s is not allowed", path, name)
				}
			}
		}
		for name, propertySchema := range schema.Properties {
			propertyValue, exists := object[name]
			if !exists {
				continue
			}
			if err := validateValue(propertySchema, propertyValue, path+"."+name); err != nil {
				return err
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if len(schema.Items) > 0 {
			for index, item := range items {
				if err := validateValue(schema.Items, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func schemaTypeMatches(schemaType string, value any) bool {
	switch schemaType {
	case "", "any":
		return true
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	case "integer":
		number, ok := value.(float64)
		return ok && number == float64(int64(number))
	case "number":
		_, ok := value.(float64)
		return ok
	default:
		return false
	}
}

func enumContains(allowed []any, value any) bool {
	for _, candidate := range allowed {
		if reflect.DeepEqual(candidate, value) {
			return true
		}
	}
	return false
}
