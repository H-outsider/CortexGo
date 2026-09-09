package tasks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cortexgo/cortexgo/internal/core"
)

type Status string

const (
	Queued    Status = "queued"
	Running   Status = "running"
	Completed Status = "completed"
	Failed    Status = "failed"
)

type Record struct {
	ID                    string
	Status                Status
	Error                 error
	StartedAt, FinishedAt time.Time
}

type Queue struct {
	jobs    chan core.Task
	workers int
	mu      sync.RWMutex
	records map[string]Record
}

func NewQueue(buffer, workers int) *Queue {
	if buffer < 1 {
		buffer = 1
	}
	if workers < 1 {
		workers = 1
	}
	return &Queue{jobs: make(chan core.Task, buffer), workers: workers, records: make(map[string]Record)}
}
func (q *Queue) Start(ctx context.Context) {
	for n := 0; n < q.workers; n++ {
		go q.worker(ctx)
	}
}
func (q *Queue) Submit(ctx context.Context, task core.Task) error {
	if task == nil || task.ID() == "" {
		return fmt.Errorf("tasks: task and ID are required")
	}
	q.set(Record{ID: task.ID(), Status: Queued})
	select {
	case q.jobs <- task:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (q *Queue) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-q.jobs:
			q.run(ctx, task)
		}
	}
}
func (q *Queue) run(ctx context.Context, task core.Task) {
	now := time.Now().UTC()
	q.set(Record{ID: task.ID(), Status: Running, StartedAt: now})
	run := &core.Run{ID: "run-" + task.ID(), TaskID: task.ID(), Status: string(Running), StartedAt: now}
	err := task.Execute(ctx, run)
	rec := Record{ID: task.ID(), Status: Completed, StartedAt: now, FinishedAt: time.Now().UTC(), Error: err}
	if err != nil {
		rec.Status = Failed
	}
	q.set(rec)
}
func (q *Queue) set(r Record) { q.mu.Lock(); defer q.mu.Unlock(); q.records[r.ID] = r }
func (q *Queue) Get(id string) (Record, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	r, ok := q.records[id]
	return r, ok
}

type Cron struct {
	queue *Queue
	tasks []cronEntry
	mu    sync.Mutex
}
type cronEntry struct {
	spec string
	task func() core.Task
	last time.Time
}

func NewCron(queue *Queue) *Cron { return &Cron{queue: queue} }
func (c *Cron) Add(spec string, task func() core.Task) error {
	if c.queue == nil || task == nil {
		return fmt.Errorf("tasks: cron dependencies are required")
	}
	if !validSpec(spec) {
		return fmt.Errorf("tasks: cron spec must have five fields")
	}
	c.mu.Lock()
	c.tasks = append(c.tasks, cronEntry{spec: spec, task: task})
	c.mu.Unlock()
	return nil
}
func (c *Cron) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	c.tick(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			c.tick(ctx, now)
		}
	}
}
func (c *Cron) tick(ctx context.Context, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for n := range c.tasks {
		e := &c.tasks[n]
		if sameMinute(e.last, now) || !match(e.spec, now) {
			continue
		}
		e.last = now
		_ = c.queue.Submit(ctx, e.task())
	}
}
func sameMinute(a, b time.Time) bool {
	return !a.IsZero() && a.Truncate(time.Minute).Equal(b.Truncate(time.Minute))
}
func validSpec(s string) bool {
	var a, b, c, d, e string
	_, err := fmt.Sscan(s, &a, &b, &c, &d, &e)
	return err == nil
}
func match(spec string, t time.Time) bool {
	var f [5]string
	if _, e := fmt.Sscan(spec, &f[0], &f[1], &f[2], &f[3], &f[4]); e != nil {
		return false
	}
	vals := [5]int{t.Minute(), t.Hour(), t.Day(), int(t.Month()), int(t.Weekday())}
	for i, v := range vals {
		if f[i] != "*" && f[i] != fmt.Sprint(v) {
			return false
		}
	}
	return true
}
