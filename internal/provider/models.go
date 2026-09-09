package provider

import "fmt"

// ModelConfig describes an OpenAI-compatible endpoint.
type ModelConfig struct {
	BaseURL, APIKey, Model, EmbeddingModel string
	MaxRetries                             int
}

func NewOpenAICompatible(config ModelConfig) OpenAICompatible {
	return OpenAICompatible{BaseURL: config.BaseURL, APIKey: config.APIKey, Model: config.Model, EmbeddingModel: config.EmbeddingModel, MaxRetries: config.MaxRetries}
}

func NewGLM(apiKey, model string) OpenAICompatible {
	if model == "" {
		model = "glm-4-flash"
	}
	return OpenAICompatible{BaseURL: "https://open.bigmodel.cn/api/paas", APIKey: apiKey, Model: model}
}
func NewDeepSeek(apiKey, model string) OpenAICompatible {
	if model == "" {
		model = "deepseek-chat"
	}
	return OpenAICompatible{BaseURL: "https://api.deepseek.com", APIKey: apiKey, Model: model}
}
func NewQwen(apiKey, model string) OpenAICompatible {
	if model == "" {
		model = "qwen-plus"
	}
	return OpenAICompatible{BaseURL: "https://dashscope.aliyuncs.com/compatible-mode", APIKey: apiKey, Model: model}
}
func NewGroq(apiKey, model string) OpenAICompatible {
	if model == "" {
		model = "llama-3.1-8b-instant"
	}
	return OpenAICompatible{BaseURL: "https://api.groq.com/openai", APIKey: apiKey, Model: model}
}

func NewProvider(name string, config ModelConfig) (ChatModel, error) {
	switch name {
	case "openai", "compatible":
		return NewOpenAICompatible(config), nil
	case "glm":
		return NewGLM(config.APIKey, config.Model), nil
	case "deepseek":
		return NewDeepSeek(config.APIKey, config.Model), nil
	case "qwen":
		return NewQwen(config.APIKey, config.Model), nil
	case "groq":
		return NewGroq(config.APIKey, config.Model), nil
	default:
		return nil, fmt.Errorf("provider: unsupported model provider %q", name)
	}
}
