package knowledge

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Loader interface {
	Load(path string) (Document, error)
}

type FileLoader struct{}

func (FileLoader) Load(path string) (Document, error) {
	if strings.TrimSpace(path) == "" {
		return Document{}, fmt.Errorf("knowledge: file path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Document{}, fmt.Errorf("knowledge: read file: %w", err)
	}
	ext := filepath.Ext(path)
	content := string(data)
	switch strings.ToLower(ext) {
	case ".txt", ".md", ".markdown":
	case ".html", ".htm":
		content = stripHTML(content)
	default:
		return Document{}, fmt.Errorf("knowledge: unsupported file extension %q", ext)
	}
	if strings.TrimSpace(content) == "" {
		return Document{}, fmt.Errorf("knowledge: file content is empty")
	}
	return Document{ID: path, Title: filepath.Base(path), Content: content, Metadata: map[string]string{"source": path, "extension": ext}}, nil
}

var htmlTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

func stripHTML(content string) string {
	content = htmlTagPattern.ReplaceAllString(content, " ")
	content = html.UnescapeString(content)
	return strings.Join(strings.Fields(content), " ")
}
