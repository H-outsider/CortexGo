package memory

import (
	"context"
	"testing"
	"time"
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

func TestFactPrivacyPolicy(t *testing.T) {
	store := NewInMemoryFactStoreWithPolicy(FactPrivacyPolicy{MaxValueBytes: 4, DefaultTTL: time.Hour})
	ctx := context.Background()
	if err := store.Upsert(ctx, Fact{UserID: "u", Key: "secret", Value: "12345"}); err == nil {
		t.Fatal("expected value limit error")
	}
	if err := store.Upsert(ctx, Fact{UserID: "u", Key: "secret", Value: "1234", Sensitive: true}); err == nil {
		t.Fatal("expected sensitive fact error")
	}
	if err := store.Upsert(ctx, Fact{UserID: "u", Key: "ok", Value: "1234"}); err != nil {
		t.Fatal(err)
	}
	facts, err := store.List(ctx, "u")
	if err != nil || len(facts) != 1 || facts[0].ExpiresAt.IsZero() {
		t.Fatalf("facts=%#v err=%v", facts, err)
	}
	if err := store.Upsert(ctx, Fact{UserID: "u", Key: "expired", Value: "x", ExpiresAt: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	facts, _ = store.List(ctx, "u")
	if len(facts) != 1 {
		t.Fatalf("expired fact retained: %#v", facts)
	}
	if err := store.ForgetUser(ctx, "u"); err != nil {
		t.Fatal(err)
	}
	facts, _ = store.List(ctx, "u")
	if len(facts) != 0 {
		t.Fatal(facts)
	}
}
