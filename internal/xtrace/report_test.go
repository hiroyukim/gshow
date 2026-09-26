package xtrace_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	rtrace "golang.org/x/exp/trace"

	"github.com/hiroyukim/gshow/internal/xtrace"
)

// buildTestTrace returns a Trace with three goroutines, built by hand rather
// than parsed from a real capture, so BuildReport's own logic (selection,
// aggregation, JSON shape) can be tested without needing a real execution
// trace file:
//
//   - id 1: two spans, a real creator once the leaf park frame is skipped,
//     and one reasoned Waiting span - a stand-in for a per-request goroutine.
//   - id 2: looks like GC/tracer machinery (creator is a runtime function),
//     so it should be hidden by default.
//   - id 3: a single span, no creator frames at all, no reason - the "-"
//     fallback case.
func buildTestTrace() *xtrace.Trace {
	return &xtrace.Trace{
		Duration: 500 * time.Millisecond,
		Goroutines: map[rtrace.GoID]*xtrace.Goroutine{
			1: {
				ID: 1,
				CreatedBy: []rtrace.StackFrame{
					{Func: "runtime.gopark"}, // leaf frame; not a useful label
					{Func: "main.doWork"},    // outermost frame; the real creator
				},
				Spans: []xtrace.Span{
					{State: "Running", Start: 0, End: 10 * time.Millisecond},
					{State: "Waiting", Reason: "sleep", Start: 10 * time.Millisecond, End: 500 * time.Millisecond},
				},
			},
			2: {
				ID:        2,
				CreatedBy: []rtrace.StackFrame{{Func: "runtime.gcBgMarkWorker"}},
				Spans: []xtrace.Span{
					{State: "Waiting", Start: 0, End: 500 * time.Millisecond},
				},
			},
			3: {
				ID: 3,
				Spans: []xtrace.Span{
					{State: "Waiting", Start: 0, End: 500 * time.Millisecond},
				},
			},
		},
	}
}

func TestBuildReport(t *testing.T) {
	tr := buildTestTrace()
	r := xtrace.BuildReport(tr, "test-target", 10, false)

	if r.Target != "test-target" {
		t.Errorf("Target = %q, want %q", r.Target, "test-target")
	}
	if r.DurationMS != 500 {
		t.Errorf("DurationMS = %v, want 500", r.DurationMS)
	}
	if r.GoroutineCount != 3 {
		t.Errorf("GoroutineCount = %d, want 3 (total observed, hidden included)", r.GoroutineCount)
	}
	if r.HiddenRuntimeInternal != 1 {
		t.Errorf("HiddenRuntimeInternal = %d, want 1", r.HiddenRuntimeInternal)
	}
	if len(r.Goroutines) != 2 {
		t.Fatalf("len(Goroutines) = %d, want 2 (id 2 hidden)", len(r.Goroutines))
	}

	// id 1 has more spans than id 3, so it must sort first regardless of
	// map iteration order.
	g1 := r.Goroutines[0]
	if g1.ID != 1 {
		t.Fatalf("Goroutines[0].ID = %d, want 1", g1.ID)
	}
	if g1.Creator != "main.doWork" {
		t.Errorf("Goroutines[0].Creator = %q, want %q (outermost non-runtime frame, not the gopark leaf)", g1.Creator, "main.doWork")
	}
	if g1.IsRuntimeInternal {
		t.Error("Goroutines[0].IsRuntimeInternal = true, want false")
	}
	if g1.Reason != "sleep" {
		t.Errorf("Goroutines[0].Reason = %q, want %q", g1.Reason, "sleep")
	}
	if g1.FirstSeenMS != 0 || g1.LastSeenMS != 500 {
		t.Errorf("Goroutines[0] seen range = [%v, %v], want [0, 500]", g1.FirstSeenMS, g1.LastSeenMS)
	}
	wantTotals := xtrace.StateTotals{RunningMS: 10, WaitingMS: 490}
	if g1.TotalsMS != wantTotals {
		t.Errorf("Goroutines[0].TotalsMS = %+v, want %+v", g1.TotalsMS, wantTotals)
	}
	if len(g1.Spans) != 2 {
		t.Fatalf("len(Goroutines[0].Spans) = %d, want 2", len(g1.Spans))
	}
	if g1.Spans[1].Reason != "sleep" || g1.Spans[1].EndMS != 500 {
		t.Errorf("Goroutines[0].Spans[1] = %+v", g1.Spans[1])
	}

	g3 := r.Goroutines[1]
	if g3.ID != 3 {
		t.Fatalf("Goroutines[1].ID = %d, want 3", g3.ID)
	}
	if g3.Creator != "-" {
		t.Errorf("Goroutines[1].Creator = %q, want %q (no creator frames at all)", g3.Creator, "-")
	}
	if g3.Reason != "" {
		t.Errorf("Goroutines[1].Reason = %q, want empty (no span carries a reason)", g3.Reason)
	}
}

func TestBuildReportMaxRowsCapsWithoutAffectingHiddenCount(t *testing.T) {
	tr := buildTestTrace()
	r := xtrace.BuildReport(tr, "test-target", 1, false)

	if len(r.Goroutines) != 1 {
		t.Fatalf("len(Goroutines) = %d, want 1 (capped by maxRows)", len(r.Goroutines))
	}
	if r.Goroutines[0].ID != 1 {
		t.Errorf("Goroutines[0].ID = %d, want 1 (the more active one)", r.Goroutines[0].ID)
	}
	// id 3 was dropped by the row cap, not because it looked like runtime
	// machinery, so it must not be counted as hidden.
	if r.HiddenRuntimeInternal != 1 {
		t.Errorf("HiddenRuntimeInternal = %d, want 1 (unchanged by the row cap)", r.HiddenRuntimeInternal)
	}
	if r.GoroutineCount != 3 {
		t.Errorf("GoroutineCount = %d, want 3 (still the total observed)", r.GoroutineCount)
	}
}

func TestBuildReportAllShowsRuntimeInternal(t *testing.T) {
	tr := buildTestTrace()
	r := xtrace.BuildReport(tr, "test-target", 10, true)

	if r.HiddenRuntimeInternal != 0 {
		t.Errorf("HiddenRuntimeInternal = %d, want 0 with all=true", r.HiddenRuntimeInternal)
	}
	if len(r.Goroutines) != 3 {
		t.Fatalf("len(Goroutines) = %d, want 3 with all=true", len(r.Goroutines))
	}
	var sawRuntimeInternal bool
	for _, g := range r.Goroutines {
		if g.ID == 2 {
			sawRuntimeInternal = true
			if !g.IsRuntimeInternal {
				t.Error("id 2's IsRuntimeInternal = false, want true even though it's shown")
			}
			if g.Creator != "runtime.gcBgMarkWorker" {
				t.Errorf("id 2's Creator = %q, want %q", g.Creator, "runtime.gcBgMarkWorker")
			}
		}
	}
	if !sawRuntimeInternal {
		t.Error("id 2 not present with all=true")
	}
}

// TestReportJSONSchema locks the wire schema documented in the README: field
// names and the omitempty behavior of optional fields. A rename here is a
// breaking change for anything consuming -json, so it should never happen
// silently.
func TestReportJSONSchema(t *testing.T) {
	tr := buildTestTrace()
	r := xtrace.BuildReport(tr, "test-target", 10, false)

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	doc := string(data)

	for _, field := range []string{
		`"target":`, `"duration_ms":`, `"goroutine_count":`, `"hidden_runtime_internal":`, `"goroutines":`,
		`"id":`, `"creator":`, `"is_runtime_internal":`, `"first_seen_ms":`, `"last_seen_ms":`,
		`"totals_ms":`, `"running_ms":`, `"runnable_ms":`, `"waiting_ms":`, `"syscall_ms":`,
		`"spans":`, `"state":`, `"start_ms":`, `"end_ms":`,
	} {
		if !strings.Contains(doc, field) {
			t.Errorf("JSON output missing documented field %s\ngot: %s", field, doc)
		}
	}

	// id 3 has no dominant reason, and "reason" is omitempty: it must not
	// appear on that goroutine's object. Rather than hunt for it inside the
	// combined document, round-trip through a generic map per goroutine.
	var generic struct {
		Goroutines []map[string]any `json:"goroutines"`
	}
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for _, g := range generic.Goroutines {
		if id, _ := g["id"].(float64); id == 3 {
			if _, ok := g["reason"]; ok {
				t.Errorf("id 3 has a \"reason\" field, want it omitted (omitempty, no dominant reason)")
			}
		}
	}
}
