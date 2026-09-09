package provider

import "testing"

func TestModelProviderPresets(t *testing.T) {
	for _, name := range []string{"glm", "deepseek", "qwen", "groq", "openai"} {
		if _, err := NewProvider(name, ModelConfig{APIKey: "k"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewProvider("unknown", ModelConfig{}); err == nil {
		t.Fatal("expected unsupported provider")
	}
	if got := NewGLM("k", ""); got.Model != "glm-4-flash" || got.BaseURL == "" {
		t.Fatalf("%#v", got)
	}
}
