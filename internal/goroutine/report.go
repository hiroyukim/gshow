package goroutine

import "time"

// Report is a machine-readable snapshot of a single goroutine dump: the
// same grouping GroupBySignature produces, as data an agent or script can
// consume directly instead of parsing the live dashboard's table.
type Report struct {
	Target         string        `json:"target"`
	CapturedAt     time.Time     `json:"captured_at"`
	GoroutineCount int           `json:"goroutine_count"`
	Groups         []GroupReport `json:"groups"`
}

// GroupReport summarizes one group of goroutines doing the identical thing
// (see Group) - who spawned them, where they currently are, and which
// goroutine IDs belong to the group.
type GroupReport struct {
	State     string `json:"state"`
	Count     int    `json:"count"`
	Wait      string `json:"wait,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	TopFrame  string `json:"top_frame,omitempty"`
	MemberIDs []int  `json:"member_ids"`
	Stack     string `json:"stack"` // the raw dump block for one representative member
}

// BuildReport groups gs the same way GroupBySignature does and returns the
// result as data instead of a table.
func BuildReport(gs []Goroutine, target string) Report {
	groups := GroupBySignature(gs)
	r := Report{
		Target:         target,
		CapturedAt:     time.Now(),
		GoroutineCount: len(gs),
		Groups:         make([]GroupReport, len(groups)),
	}
	for i, g := range groups {
		ids := make([]int, len(g.Members))
		for j, m := range g.Members {
			ids[j] = m.ID
		}
		r.Groups[i] = GroupReport{
			State:     g.State,
			Count:     g.Count(),
			Wait:      g.Wait,
			CreatedBy: g.CreatedBy.FuncName(),
			TopFrame:  g.Top.FuncName(),
			MemberIDs: ids,
			Stack:     g.Members[0].Raw,
		}
	}
	return r
}
