package goroutine_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hiroyukim/gshow/internal/goroutine"
)

// buildTestGoroutines returns three goroutines built by hand rather than
// parsed from a real dump, so BuildReport's own logic (grouping, ordering,
// JSON shape) can be tested without needing a captured pprof dump:
//
//   - id 1 and id 2: identical state and stack, so GroupBySignature must
//     fold them into one group of two, with a real CreatedBy.
//   - id 3: a different state and stack, alone in its own group, with a
//     zero-value CreatedBy (e.g. goroutine 1 / main, which nothing spawned).
func buildTestGoroutines() []goroutine.Goroutine {
	stack1 := []goroutine.Frame{{Func: "main.worker.func1()", File: "/app/main.go", Line: 42}}
	createdBy := goroutine.Frame{Func: "main.startWorkerPool", File: "/app/main.go", Line: 40}

	return []goroutine.Goroutine{
		{
			ID: 1, State: "chan receive", Stack: stack1, CreatedBy: createdBy,
			Raw: "goroutine 1 [chan receive]:\nmain.worker.func1()\n\t/app/main.go:42 +0x1\ncreated by main.startWorkerPool in goroutine 1\n\t/app/main.go:40 +0x2\n",
		},
		{
			ID: 2, State: "chan receive", Stack: stack1, CreatedBy: createdBy,
			Raw: "goroutine 2 [chan receive]:\nmain.worker.func1()\n\t/app/main.go:42 +0x1\ncreated by main.startWorkerPool in goroutine 1\n\t/app/main.go:40 +0x2\n",
		},
		{
			ID: 3, State: "running", Wait: "5 minutes",
			Stack: []goroutine.Frame{{Func: "runtime/pprof.writeGoroutineStacks(...)", File: "/usr/local/go/src/runtime/pprof/pprof.go", Line: 816}},
			Raw:   "goroutine 3 [running, 5 minutes]:\nruntime/pprof.writeGoroutineStacks(...)\n\t/usr/local/go/src/runtime/pprof/pprof.go:816 +0x1\n",
		},
	}
}

func TestBuildReport(t *testing.T) {
	gs := buildTestGoroutines()
	r := goroutine.BuildReport(gs, "test-target")

	if r.Target != "test-target" {
		t.Errorf("Target = %q, want %q", r.Target, "test-target")
	}
	if since := time.Since(r.CapturedAt); since < 0 || since > time.Minute {
		t.Errorf("CapturedAt = %v, want close to now (time.Since = %v)", r.CapturedAt, since)
	}
	if r.GoroutineCount != 3 {
		t.Errorf("GoroutineCount = %d, want 3", r.GoroutineCount)
	}
	if len(r.Groups) != 2 {
		t.Fatalf("len(Groups) = %d, want 2 (ids 1 and 2 share a signature)", len(r.Groups))
	}

	// The two-member group must sort first (GroupBySignature orders by
	// member count, largest first).
	g0 := r.Groups[0]
	if g0.State != "chan receive" || g0.Count != 2 {
		t.Fatalf("Groups[0] = %+v, want state=chan receive count=2", g0)
	}
	if g0.CreatedBy != "main.startWorkerPool" {
		t.Errorf("Groups[0].CreatedBy = %q, want %q", g0.CreatedBy, "main.startWorkerPool")
	}
	if g0.TopFrame != "main.worker.func1" {
		t.Errorf("Groups[0].TopFrame = %q, want %q (args stripped)", g0.TopFrame, "main.worker.func1")
	}
	if got := g0.MemberIDs; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("Groups[0].MemberIDs = %v, want [1 2]", got)
	}
	if !strings.HasPrefix(g0.Stack, "goroutine 1 [chan receive]:") {
		t.Errorf("Groups[0].Stack = %q, want the lowest-ID member's raw dump (goroutine 1)", g0.Stack)
	}
	if g0.Wait != "" {
		t.Errorf("Groups[0].Wait = %q, want empty", g0.Wait)
	}

	g1 := r.Groups[1]
	if g1.State != "running" || g1.Count != 1 {
		t.Fatalf("Groups[1] = %+v, want state=running count=1", g1)
	}
	if g1.Wait != "5 minutes" {
		t.Errorf("Groups[1].Wait = %q, want %q", g1.Wait, "5 minutes")
	}
	if g1.CreatedBy != "" {
		t.Errorf("Groups[1].CreatedBy = %q, want empty (zero-value Frame - nothing spawned this one)", g1.CreatedBy)
	}
	if got := g1.MemberIDs; len(got) != 1 || got[0] != 3 {
		t.Errorf("Groups[1].MemberIDs = %v, want [3]", got)
	}
}

// TestReportJSONSchema locks the wire schema documented in the README:
// field names and the omitempty behavior of optional fields. A rename here
// is a breaking change for anything consuming -json, so it should never
// happen silently.
func TestReportJSONSchema(t *testing.T) {
	gs := buildTestGoroutines()
	r := goroutine.BuildReport(gs, "test-target")

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	doc := string(data)

	for _, field := range []string{
		`"target":`, `"captured_at":`, `"goroutine_count":`, `"groups":`,
		`"state":`, `"count":`, `"member_ids":`, `"stack":`,
	} {
		if !strings.Contains(doc, field) {
			t.Errorf("JSON output missing documented field %s\ngot: %s", field, doc)
		}
	}

	// "wait" and "created_by" are omitempty: the running/id-3 group (which
	// has a Wait but no CreatedBy) and the chan-receive group (which has a
	// CreatedBy but no Wait) each exercise one side of that, so round-trip
	// through a generic map per group rather than grepping the whole
	// document.
	var generic struct {
		Groups []map[string]any `json:"groups"`
	}
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for _, g := range generic.Groups {
		state, _ := g["state"].(string)
		switch state {
		case "chan receive":
			if _, ok := g["wait"]; ok {
				t.Errorf("chan receive group has a \"wait\" field, want it omitted (empty Wait)")
			}
			if _, ok := g["created_by"]; !ok {
				t.Errorf("chan receive group is missing \"created_by\", want it present (non-empty CreatedBy)")
			}
		case "running":
			if _, ok := g["wait"]; !ok {
				t.Errorf("running group is missing \"wait\", want it present (non-empty Wait)")
			}
			if _, ok := g["created_by"]; ok {
				t.Errorf("running group has a \"created_by\" field, want it omitted (zero-value CreatedBy)")
			}
		}
	}
}
