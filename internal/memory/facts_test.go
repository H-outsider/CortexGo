package memory

import (
	"context"
	"testing"
)

func TestInMemoryFactStoreUpsertListAndForget(t *testing.T) {
	store := NewInMemoryFactStore()
	ctx := context.Background()
	if err := store.Upsert(ctx, Fact{UserID: "u1", Key: "language", Value: "Go", Source: "conversation"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(ctx, Fact{UserID: "u1", Key: "language", Value: "Rust"}); err != nil {
		t.Fatal(err)
	}
	facts, err := store.List(ctx, "u1")
	if err != nil || len(facts) != 1 || facts[0].Value != "Rust" || facts[0].CreatedAt.IsZero() || facts[0].UpdatedAt.IsZero() {
		t.Fatalf("facts=%#v err=%v", facts, err)
	}
	if err := store.Forget(ctx, "u1", "language"); err != nil {
		t.Fatal(err)
	}
	facts, _ = store.List(ctx, "u1")
	if len(facts) != 0 {
		t.Fatalf("facts after forget=%#v", facts)
	}
}
