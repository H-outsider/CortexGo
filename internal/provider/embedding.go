package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Embed implements knowledge.EmbeddingModel using an OpenAI-compatible endpoint.
func (p OpenAICompatible) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	model := p.EmbeddingModel
	if model == "" {
		model = p.Model
	}
	if p.BaseURL == "" || model == "" {
		return nil, &Error{Op: "embed", Err: fmt.Errorf("BaseURL and Model are required")}
	}
	if len(texts) == 0 {
		return nil, &Error{Op: "embed", Err: fmt.Errorf("input must not be empty")}
	}
	body, err := json.Marshal(embeddingRequest{Model: model, Input: texts})
	if err != nil {
		return nil, &Error{Op: "encode embed request", Err: err}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, p.timeout(60*time.Second))
	defer cancel()
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	url := strings.TrimRight(p.BaseURL, "/") + "/v1/embeddings"
	retries := nonNegative(p.MaxRetries)
	for attempt := 0; ; attempt++ {
		p.logf("embedding request attempt=%d model=%s inputs=%d", attempt+1, model, len(texts))
		req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, &Error{Op: "create embed request", Err: err}
		}
		req.Header.Set("Content-Type", "application/json")
		if p.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.APIKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			if attempt >= retries {
				return nil, &Error{Op: "embed request", Err: err}
			}
			if waitErr := waitForRetry(requestCtx, retryDelay(p.RetryBackoff, attempt)); waitErr != nil {
				return nil, &Error{Op: "embed retry", Err: waitErr}
			}
			continue
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			if attempt >= retries {
				return nil, &Error{Op: "read embed response", Err: readErr}
			}
			continue
		}
		if retryableStatus(resp.StatusCode) {
			if attempt >= retries {
				return nil, &Error{Op: "embed", Status: resp.StatusCode, Err: fmt.Errorf("%s", strings.TrimSpace(string(data)))}
			}
			if waitErr := waitForRetry(requestCtx, retryDelay(p.RetryBackoff, attempt)); waitErr != nil {
				return nil, &Error{Op: "embed retry", Err: waitErr}
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, &Error{Op: "embed", Status: resp.StatusCode, Err: fmt.Errorf("%s", strings.TrimSpace(string(data)))}
		}
		var result embeddingResponse
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, &Error{Op: "decode embed response", Err: err}
		}
		if result.Error != nil {
			return nil, &Error{Op: "embed", Err: fmt.Errorf("%s", result.Error.Message)}
		}
		if len(result.Data) != len(texts) {
			return nil, &Error{Op: "embed", Err: fmt.Errorf("expected %d embeddings, got %d", len(texts), len(result.Data))}
		}
		vectors := make([][]float64, len(texts))
		for _, item := range result.Data {
			if item.Index < 0 || item.Index >= len(vectors) || len(item.Embedding) == 0 {
				return nil, &Error{Op: "embed", Err: fmt.Errorf("invalid embedding index or vector")}
			}
			vectors[item.Index] = item.Embedding
		}
		for index, vector := range vectors {
			if len(vector) == 0 {
				return nil, &Error{Op: "embed", Err: fmt.Errorf("missing embedding at index %d", index)}
			}
		}
		return vectors, nil
	}
}
