package memory

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cortexgo/cortexgo/internal/provider"
)

func TestFileStorePersistsAndReloadsSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.json")
	store, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	messages := []provider.Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}}
	if err := store.Append(context.Background(), "s1", messages...); err != nil {
		t.Fatal(err)
	}
	reloaded, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	history, err := reloaded.List(context.Background(), "s1")
	if err != nil || len(history) != 2 || history[1].Content != "hi" {
		t.Fatalf("history=%#v err=%v", history, err)
	}
	if err := reloaded.Delete(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	history, _ = reloaded.List(context.Background(), "s1")
	if len(history) != 0 {
		t.Fatalf("history after delete=%#v", history)
	}
}
