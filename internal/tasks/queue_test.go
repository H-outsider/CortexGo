package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cortexgo/cortexgo/internal/core"
)

type testTask struct {
	id  string
	err error
}

func (t testTask) ID() string                               { return t.id }
func (t testTask) Kind() string                             { return "test" }
func (t testTask) Execute(context.Context, *core.Run) error { return t.err }
func TestQueueRunsTasks(t *testing.T) {
	q := NewQueue(2, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)
	if err := q.Submit(ctx, testTask{id: "a"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		r, ok := q.Get("a")
		if ok && r.Status == Completed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
		time.Sleep(time.Millisecond)
	}
	if err := q.Submit(ctx, testTask{id: "b", err: errors.New("boom")}); err != nil {
		t.Fatal(err)
	}
}
func TestCronSpec(t *testing.T) {
	if !validSpec("* * * * *") || validSpec("*") {
		t.Fatal("invalid spec handling")
	}
	if !match("* * * * *", time.Now()) {
		t.Fatal("wildcard mismatch")
	}
}
