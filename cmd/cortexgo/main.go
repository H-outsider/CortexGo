package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cortexgo/cortexgo/internal/agent"
	"github.com/cortexgo/cortexgo/internal/knowledge"
	"github.com/cortexgo/cortexgo/internal/memory"
	"github.com/cortexgo/cortexgo/internal/provider"
	"github.com/cortexgo/cortexgo/internal/tool"
)

func main() {
	ctx := context.Background()
	flags := flag.NewFlagSet("cortexgo", flag.ContinueOnError)
	providerName := flags.String("provider", envDefault("CORTEXGO_PROVIDER", "echo"), "model provider: echo or openai")
	baseURL := flags.String("base-url", os.Getenv("CORTEXGO_BASE_URL"), "OpenAI-compatible API base URL")
	modelName := flags.String("model", envDefault("CORTEXGO_MODEL", ""), "model name")
	sessionID := flags.String("session", envDefault("CORTEXGO_SESSION_ID", "default"), "session ID")
	maxRetries := flags.Int("retries", envInt("CORTEXGO_MAX_RETRIES", 2), "maximum retry attempts")
	retryBackoff := flags.Duration("retry-backoff", envDuration("CORTEXGO_RETRY_BACKOFF", 100*time.Millisecond), "base delay between retries")
	requestTimeout := flags.Duration("timeout", envDuration("CORTEXGO_TIMEOUT", 120*time.Second), "request timeout")
	stream := flags.Bool("stream", true, "stream output when the provider supports it")
	showUsage := flags.Bool("show-usage", envBool("CORTEXGO_SHOW_USAGE", false), "print model and token usage after each response")
	enableTools := flags.Bool("tools", envBool("CORTEXGO_TOOLS", true), "enable the built-in tool registry")
	embeddingModel := flags.String("embedding-model", envDefault("CORTEXGO_EMBEDDING_MODEL", ""), "embedding model for knowledge commands")
	knowledgeFile := flags.String("knowledge-file", "", "ingest a text file into an in-memory knowledge index")
	knowledgeQuery := flags.String("knowledge-query", "", "query the ingested knowledge file and exit")
	knowledgeIndex := flags.String("knowledge-index", envDefault("CORTEXGO_KNOWLEDGE_INDEX", ".cortexgo-knowledge.json"), "persistent knowledge index path")
	memoryFile := flags.String("memory-file", envDefault("CORTEXGO_MEMORY_FILE", ""), "persist conversation memory to a JSON file")
	if err := flags.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}

	chatModel, err := newModel(*providerName, provider.OpenAICompatible{
		BaseURL:        *baseURL,
		APIKey:         os.Getenv("CORTEXGO_API_KEY"),
		Model:          *modelName,
		MaxRetries:     *maxRetries,
		RetryBackoff:   *retryBackoff,
		RequestTimeout: *requestTimeout,
		Logger:         log.Default(),
	})
	if err != nil {
		log.Fatal(err)
	}
	registry := tool.NewRegistry()
	if *enableTools {
		if err := registry.Register(tool.CurrentTime()); err != nil {
			log.Fatal(err)
		}
	}
	if *knowledgeFile != "" || *knowledgeQuery != "" {
		if *providerName != "openai" {
			log.Fatal("knowledge commands require -provider=openai")
		}
		if *knowledgeFile == "" || strings.TrimSpace(*knowledgeQuery) == "" {
			log.Fatal("-knowledge-file and -knowledge-query must be provided together")
		}
		content, err := os.ReadFile(*knowledgeFile)
		if err != nil {
			log.Fatal(err)
		}
		embedder := provider.OpenAICompatible{BaseURL: *baseURL, APIKey: os.Getenv("CORTEXGO_API_KEY"), Model: *modelName, EmbeddingModel: *embeddingModel, MaxRetries: *maxRetries, RetryBackoff: *retryBackoff, RequestTimeout: *requestTimeout, Logger: log.Default()}
		index, err := knowledge.NewPersistentHybridIndex(*knowledgeIndex, embedder, 0.5)
		if err != nil {
			log.Fatal(err)
		}
		if err := index.Add(ctx, knowledge.Document{ID: *knowledgeFile, Title: *knowledgeFile, Content: string(content)}); err != nil {
			log.Fatal(err)
		}
		results, err := index.Search(ctx, *knowledgeQuery, 5)
		if err != nil {
			log.Fatal(err)
		}
		for _, result := range results {
			fmt.Printf("[%s score=%.3f] %s\n", result.Chunk.ID, result.Score, result.Chunk.Text)
		}
		return
	}
	var store memory.Store = memory.NewInMemory()
	if *memoryFile != "" {
		fileStore, err := memory.OpenFileStore(*memoryFile)
		if err != nil {
			log.Fatal(err)
		}
		store = fileStore
	}
	a := agent.New(chatModel, store, agent.WithTools(registry))

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	fmt.Fprintf(os.Stderr, "CortexGo provider=%s session=%s\n", *providerName, *sessionID)
	fmt.Fprintln(os.Stderr, "输入内容开始对话，/exit 退出。")

	for fmt.Fprint(os.Stderr, "you> "); scanner.Scan(); fmt.Fprint(os.Stderr, "you> ") {
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "/exit" || input == "/quit" {
			break
		}

		var result agent.ChatResult
		var err error
		if *stream {
			fmt.Fprint(os.Stderr, "agent> ")
			result, err = a.ChatStreamWithResult(ctx, *sessionID, input, func(delta string) error {
				fmt.Print(delta)
				return nil
			})
			fmt.Println()
		} else {
			result, err = a.ChatWithResult(ctx, *sessionID, input)
			fmt.Println(result.Content)
		}
		if err != nil {
			log.Fatal(err)
		}
		if *showUsage {
			printUsage(result)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}

func printUsage(result agent.ChatResult) {
	fmt.Fprintf(os.Stderr, "usage model=%s finish=%s prompt=%d completion=%d total=%d\n",
		result.Model,
		result.FinishReason,
		result.Usage.PromptTokens,
		result.Usage.CompletionTokens,
		result.Usage.TotalTokens,
	)
}

func newModel(name string, openAI provider.OpenAICompatible) (provider.ChatModel, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "echo":
		return provider.Echo{}, nil
	case "openai":
		if openAI.BaseURL == "" || openAI.Model == "" {
			return nil, fmt.Errorf("openai provider requires CORTEXGO_BASE_URL and CORTEXGO_MODEL")
		}
		return openAI, nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", name)
	}
}

func envDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return fallback
	}
	return number
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}

func envBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
