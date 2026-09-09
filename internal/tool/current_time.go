package tool

import (
	"context"
	"encoding/json"
	"time"
)

func CurrentTime() Definition {
	return Definition{
		Name:        "current_time",
		Description: "Get the current UTC time in RFC3339 or RFC1123 format.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"format":{
					"type":"string",
					"enum":["RFC3339","RFC1123"],
					"description":"Output timestamp format; defaults to RFC3339."
				}
			},
			"required":[],
			"additionalProperties":false
		}`),
		Handler: func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
			var request struct {
				Format string `json:"format"`
			}
			if err := json.Unmarshal(input, &request); err != nil {
				return nil, err
			}
			layout := time.RFC3339
			if request.Format == "RFC1123" {
				layout = time.RFC1123
			}
			return json.Marshal(map[string]string{
				"format": layout,
				"time":   time.Now().UTC().Format(layout),
			})
		},
	}
}
