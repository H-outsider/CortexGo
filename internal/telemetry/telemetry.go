// Package telemetry provides dependency-free hooks that can be bridged to OpenTelemetry.
package telemetry

import (
	"context"
	"time"

	"github.com/cortexgo/cortexgo/internal/provider"
)

type ModelEvent struct {
	Operation string
	Model     string
	StartedAt time.Time
	Duration  time.Duration
	Usage     provider.Usage
	CostUSD   float64
	Err       error
}

type Recorder interface {
	RecordModel(context.Context, ModelEvent)
}

type CostRates struct{ PromptPer1KUSD, CompletionPer1KUSD float64 }

func (r CostRates) Calculate(usage provider.Usage) float64 {
	return float64(usage.PromptTokens)/1000*r.PromptPer1KUSD + float64(usage.CompletionTokens)/1000*r.CompletionPer1KUSD
}

type CostMonitor struct {
	Recorder Recorder
	Rates    map[string]CostRates
}

func (m CostMonitor) Record(ctx context.Context, event ModelEvent) {
	if rates, ok := m.Rates[event.Model]; ok {
		event.CostUSD = rates.Calculate(event.Usage)
	}
	if m.Recorder != nil {
		m.Recorder.RecordModel(ctx, event)
	}
}
