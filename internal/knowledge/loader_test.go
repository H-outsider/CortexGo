package knowledge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileLoaderLoadsMarkdownAndMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guide.md")
	if err := os.WriteFile(path, []byte("# Guide\n\nCortexGo knowledge"), 0o600); err != nil {
		t.Fatal(err)
	}
	document, err := (FileLoader{}).Load(path)
	if err != nil || document.Title != "guide.md" || document.Metadata["extension"] != ".md" {
		t.Fatalf("document=%#v err=%v", document, err)
	}
}

func TestFileLoaderStripsHTML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(path, []byte("<h1>Guide</h1><p>Hello &amp; welcome</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	document, err := (FileLoader{}).Load(path)
	if err != nil || document.Content != "Guide Hello & welcome" {
		t.Fatalf("document=%#v err=%v", document, err)
	}
}

func TestFileLoaderRejectsUnsupportedExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.pdf")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (FileLoader{}).Load(path); err == nil {
		t.Fatal("expected unsupported extension error")
	}
}
