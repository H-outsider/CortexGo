package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegistryInvokeValidatesSchema(t *testing.T) {
	var received json.RawMessage
	registry := NewRegistry()
	if err := registry.Register(Definition{
		Name:        "lookup_user",
		Description: "Look up a user by ID",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{"id":{"type":"integer"},"role":{"type":"string","enum":["admin","member"]}},
			"required":["id"],
			"additionalProperties":false
		}`),
		Handler: func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
			received = append([]byte(nil), input...)
			return json.RawMessage(`{"name":"Kuban"}`), nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	output, err := registry.Invoke(context.Background(), "lookup_user", json.RawMessage(`{"id":7,"role":"admin"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(received) != `{"id":7,"role":"admin"}` || string(output) != `{"name":"Kuban"}` {
		t.Fatalf("received = %s, output = %s", received, output)
	}
}

func TestRegistryInvokeRejectsInvalidInput(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Definition{
		Name:        "lookup_user",
		Description: "Look up a user by ID",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{"id":{"type":"integer"}},
			"required":["id"],
			"additionalProperties":false
		}`),
		Handler: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	tests := []json.RawMessage{
		json.RawMessage(`{"id":"7"}`),
		json.RawMessage(`{"role":"admin"}`),
		json.RawMessage(`{"id":7,"extra":true}`),
		json.RawMessage(`[]`),
		json.RawMessage(`{`),
	}
	for _, input := range tests {
		if _, err := registry.Invoke(context.Background(), "lookup_user", input); err == nil {
			t.Fatalf("expected error for %s", input)
		}
	}
}

func TestRegistryValidatesDefinitions(t *testing.T) {
	handler := func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}
	tests := []Definition{
		{Name: "invalid name", Description: "invalid", InputSchema: json.RawMessage(`{"type":"object"}`), Handler: handler},
		{Name: "valid_name", Description: "", InputSchema: json.RawMessage(`{"type":"object"}`), Handler: handler},
		{Name: "valid_name", Description: "valid", InputSchema: json.RawMessage(`{"type":"object"}`), Handler: nil},
		{Name: "valid_name", Description: "valid", InputSchema: json.RawMessage(`{"type":"string"}`), Handler: handler},
		{Name: "valid_name", Description: "valid", InputSchema: json.RawMessage(`{`), Handler: handler},
		{Name: "valid_name", Description: "valid", InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer","minimum":1}}}`), Handler: handler},
	}
	for _, def := range tests {
		if err := NewRegistry().Register(def); err == nil {
			t.Fatalf("expected error for %#v", def)
		}
	}
}

func TestRegistryRejectsDuplicateNames(t *testing.T) {
	def := Definition{
		Name:        "duplicate",
		Description: "duplicate",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler:     func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, nil },
	}
	registry := NewRegistry()
	if err := registry.Register(def); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(def); err == nil {
		t.Fatal("expected duplicate name error")
	}
}
