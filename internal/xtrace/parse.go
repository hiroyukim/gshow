// Package xtrace turns a Go execution trace (runtime/trace's binary format,
// as served by net/http/pprof's or gshow's own /debug/pprof/trace) into a
// per-goroutine timeline: which goroutine was running, runnable, blocked,
// or in a syscall, and when - a much finer-grained picture than a periodic
// goroutine dump, at the cost of only covering a short capture window.
package xtrace

import (
	"fmt"
	"io"
	"time"

	rtrace "golang.org/x/exp/trace"
)

// Span is one interval during which a goroutine stayed in a single state.
type Span struct {
	State  string // "Running", "Runnable", "Waiting", "Syscall", ...
	Reason string // e.g. "chan receive"; empty if the state doesn't carry one
	Start  time.Duration
	End    time.Duration
}

// Goroutine is one goroutine's state history over the capture window.
type Goroutine struct {
	ID        rtrace.GoID
	CreatedBy []rtrace.StackFrame // the stack the first time this goroutine was observed
	Spans     []Span              // in chronological order, non-overlapping
}

// TotalBusy returns time spent actually running or in a syscall, as opposed
// to idle (runnable, waiting on the scheduler) or blocked (waiting on
// something else, e.g. a channel or mutex).
func (g *Goroutine) TotalBusy() time.Duration {
	var d time.Duration
	for _, s := range g.Spans {
		if s.State == "Running" || s.State == "Syscall" {
			d += s.End - s.Start
		}
	}
	return d
}

// Trace is a parsed execution trace: every goroutine observed and what it
// was doing, relative to the start of the capture.
type Trace struct {
	Duration   time.Duration
	Goroutines map[rtrace.GoID]*Goroutine
}

// Parse reads a full execution trace, as produced by runtime/trace.Start
// (directly, or via net/http/pprof's or gshow probe's /debug/pprof/trace).
func Parse(r io.Reader) (*Trace, error) {
	rd, err := rtrace.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("new trace reader: %w", err)
	}

	t := &Trace{Goroutines: map[rtrace.GoID]*Goroutine{}}

	type openSpan struct {
		state  string
		reason string
		start  time.Duration
	}
	open := map[rtrace.GoID]openSpan{}

	var t0 rtrace.Time
	haveT0 := false
	var last time.Duration

	closeSpan := func(id rtrace.GoID, end time.Duration) {
		o, ok := open[id]
		if !ok {
			return
		}
		delete(open, id)
		t.Goroutines[id].Spans = append(t.Goroutines[id].Spans, Span{
			State: o.state, Reason: o.reason, Start: o.start, End: end,
		})
	}

	for {
		ev, err := rd.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read event: %w", err)
		}

		if !haveT0 {
			t0, haveT0 = ev.Time(), true
		}
		at := ev.Time().Sub(t0)
		if at > last {
			last = at
		}

		if ev.Kind() != rtrace.EventStateTransition {
			continue
		}
		st := ev.StateTransition()
		if st.Resource.Kind != rtrace.ResourceGoroutine {
			continue
		}
		id := st.Resource.Goroutine()
		_, to := st.Goroutine()

		g, ok := t.Goroutines[id]
		if !ok {
			g = &Goroutine{ID: id}
			for f := range st.Stack.Frames() {
				g.CreatedBy = append(g.CreatedBy, f)
			}
			t.Goroutines[id] = g
		}

		closeSpan(id, at)
		if to == rtrace.GoNotExist {
			continue
		}
		open[id] = openSpan{state: to.String(), reason: st.Reason, start: at}
	}

	for id := range open {
		closeSpan(id, last)
	}
	t.Duration = last
	return t, nil
}
