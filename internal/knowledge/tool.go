package knowledge

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cortexgo/cortexgo/internal/tool"
)

// SearchTool exposes an Index as a model-callable tool with citation metadata.
func SearchTool(index Index) tool.Definition {
	return tool.Definition{
		Name:        "knowledge_search",
		Description: "Search the knowledge base and return matching text with citations.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"],"additionalProperties":false}`),
		Handler: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			if index == nil {
				return nil, fmt.Errorf("knowledge index is not configured")
			}
			var request struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(input, &request); err != nil {
				return nil, err
			}
			if request.Limit <= 0 {
				request.Limit = 5
			}
			results, err := index.Search(ctx, request.Query, request.Limit)
			if err != nil {
				return nil, err
			}
			response := make([]map[string]any, 0, len(results))
			for _, result := range results {
				response = append(response, map[string]any{
					"citation":    result.Chunk.ID,
					"document_id": result.Chunk.DocumentID,
					"title":       result.Chunk.Title,
					"text":        result.Chunk.Text,
					"score":       result.Score,
				})
			}
			return json.Marshal(response)
		},
	}
}
