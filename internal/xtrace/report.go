package xtrace

import (
	"time"
)

// Report is a machine-readable summary of a Trace: the same selection
// (filtering, ordering, capping) that Render draws as text, but as data an
// agent or script can consume directly instead of having to re-derive it
// from an ASCII bar chart.
type Report struct {
	Target                string            `json:"target"`
	DurationMS            float64           `json:"duration_ms"`
	GoroutineCount        int               `json:"goroutine_count"`         // total observed, including hidden and not-shown
	HiddenRuntimeInternal int               `json:"hidden_runtime_internal"` // excluded unless requested; see (*Goroutine).IsRuntimeInternal
	Goroutines            []GoroutineReport `json:"goroutines"`              // most-active-first, capped at maxRows
}

// GoroutineReport summarizes one goroutine's activity over the capture.
type GoroutineReport struct {
	ID                int64        `json:"id"`
	Creator           string       `json:"creator"`             // see creatorLabel
	IsRuntimeInternal bool         `json:"is_runtime_internal"` // GC/tracer machinery, not target-program code
	Reason            string       `json:"reason,omitempty"`    // dominant block reason, e.g. "chan receive"
	FirstSeenMS       float64      `json:"first_seen_ms"`       // 0 if alive since before the capture started
	LastSeenMS        float64      `json:"last_seen_ms"`        // == duration_ms if still alive when the capture ended
	TotalsMS          StateTotals  `json:"totals_ms"`
	Spans             []SpanReport `json:"spans"`
}

// StateTotals is time spent in each state, summed across every span.
type StateTotals struct {
	RunningMS  float64 `json:"running_ms"`
	RunnableMS float64 `json:"runnable_ms"`
	WaitingMS  float64 `json:"waiting_ms"`
	SyscallMS  float64 `json:"syscall_ms"`
}

// SpanReport is one interval a goroutine spent in a single state.
type SpanReport struct {
	State   string  `json:"state"`
	Reason  string  `json:"reason,omitempty"`
	StartMS float64 `json:"start_ms"`
	EndMS   float64 `json:"end_ms"`
}

func ms(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

// BuildReport selects goroutines the same way Render does (see
// selectGoroutines) and returns the result as data instead of text.
func BuildReport(t *Trace, target string, maxRows int, all bool) Report {
	shown, hidden := selectGoroutines(t, maxRows, all)

	r := Report{
		Target:                target,
		DurationMS:            ms(t.Duration),
		GoroutineCount:        len(t.Goroutines),
		HiddenRuntimeInternal: hidden,
		Goroutines:            make([]GoroutineReport, 0, len(shown)),
	}
	for _, id := range shown {
		g := t.Goroutines[id]
		gr := GoroutineReport{
			ID:                int64(g.ID),
			Creator:           creatorLabel(g),
			IsRuntimeInternal: g.IsRuntimeInternal(),
			Reason:            dominantReason(g),
			Spans:             make([]SpanReport, len(g.Spans)),
		}
		for i, s := range g.Spans {
			gr.Spans[i] = SpanReport{
				State:   s.State,
				Reason:  s.Reason,
				StartMS: ms(s.Start),
				EndMS:   ms(s.End),
			}
			d := ms(s.End - s.Start)
			switch s.State {
			case "Running":
				gr.TotalsMS.RunningMS += d
			case "Runnable":
				gr.TotalsMS.RunnableMS += d
			case "Syscall":
				gr.TotalsMS.SyscallMS += d
			case "Waiting":
				gr.TotalsMS.WaitingMS += d
			}
		}
		if n := len(g.Spans); n > 0 {
			gr.FirstSeenMS = ms(g.Spans[0].Start)
			gr.LastSeenMS = ms(g.Spans[n-1].End)
		}
		r.Goroutines = append(r.Goroutines, gr)
	}
	return r
}
