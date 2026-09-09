package security

import (
	"context"
	"github.com/cortexgo/cortexgo/internal/memory"
	"testing"
)

func TestTenantMemoryIsolation(t *testing.T) {
	s := TenantMemory{Store: memory.NewInMemory()}
	if err := s.Append(WithTenant(context.Background(), "a"), "session"); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(WithTenant(context.Background(), "b"), "session")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("cross tenant data")
	}
}
