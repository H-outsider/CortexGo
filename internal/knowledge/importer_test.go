package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVersionStoreAndAsyncImporter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	vs := NewVersionStore()
	idx := NewInMemoryIndex()
	imp := NewAsyncImporter(FileLoader{}, idx, vs)
	id, err := imp.Submit(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		p, ok := imp.Progress(id)
		if ok && (p.Status == "completed" || p.Status == "failed") {
			if p.Err != nil {
				t.Fatal(p.Err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("import timeout")
		}
		time.Sleep(time.Millisecond)
	}
	if len(vs.List(path)) != 1 {
		t.Fatal("version missing")
	}
}
