package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFileFactStorePersistsAndForgets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "facts.json")
	policy := FactPrivacyPolicy{AllowSensitive: true}
	store, err := OpenFileFactStoreWithPolicy(path, policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(context.Background(), Fact{UserID: "u1", Key: "name", Value: "Ada", Sensitive: true}); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFileFactStoreWithPolicy(path, policy)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := reopened.List(context.Background(), "u1")
	if err != nil || len(facts) != 1 || facts[0].Value != "Ada" || !facts[0].Sensitive {
		t.Fatalf("facts=%#v err=%v", facts, err)
	}
	if err := reopened.ForgetUser(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	facts, _ = reopened.List(context.Background(), "u1")
	if len(facts) != 0 {
		t.Fatal(facts)
	}
}

func TestFileFactStoreExpiresFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "facts.json")
	store, err := OpenFileFactStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(context.Background(), Fact{UserID: "u", Key: "old", Value: "x", ExpiresAt: time.Now().Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	facts, err := store.List(context.Background(), "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("facts=%#v", facts)
	}
}
