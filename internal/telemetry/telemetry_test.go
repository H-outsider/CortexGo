package telemetry

import (
	"context"
	"testing"

	"github.com/cortexgo/cortexgo/internal/provider"
)

type recorder struct{ event ModelEvent }

func (r *recorder) RecordModel(_ context.Context, event ModelEvent) { r.event = event }

func TestCostRates(t *testing.T) {
	r := &recorder{}
	m := CostMonitor{Recorder: r, Rates: map[string]CostRates{"m": {PromptPer1KUSD: 1, CompletionPer1KUSD: 2}}}
	m.Record(context.Background(), ModelEvent{Model: "m", Usage: provider.Usage{PromptTokens: 500, CompletionTokens: 250}})
	if r.event.CostUSD != 1 {
		t.Fatalf("cost=%v", r.event.CostUSD)
	}
}
