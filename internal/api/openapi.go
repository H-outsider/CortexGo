package api

import (
	"encoding/json"
	"net/http"
)

func openAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "method_not_allowed"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"openapi": "3.0.3", "info": map[string]string{"title": "CortexGo API", "version": "v1"}, "paths": map[string]any{"/healthz": map[string]any{"get": map[string]string{"summary": "Health check"}}, "/v1/chat/completions": map[string]any{"post": map[string]string{"summary": "Chat completion"}}, "/chat": map[string]any{"post": map[string]string{"summary": "Chat"}}}})
}
