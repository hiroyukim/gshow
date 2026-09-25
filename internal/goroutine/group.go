package goroutine

import "sort"

// Group is a set of goroutines that are all doing the identical thing:
// same state and same call stack.
type Group struct {
	Signature string
	State     string
	Wait      string // representative wait duration (from the first member)
	Top       Frame  // outermost frame, shared by every member
	CreatedBy Frame  // where these goroutines were spawned, if uniform
	Members   []Goroutine
}

func (g Group) Count() int { return len(g.Members) }

// Group buckets goroutines by StackSignature and returns the groups sorted
// by member count, largest first (ties broken by signature for stable
// ordering across polls).
func GroupBySignature(gs []Goroutine) []Group {
	idx := map[string]int{}
	var groups []Group
	for _, g := range gs {
		sig := g.StackSignature()
		if i, ok := idx[sig]; ok {
			groups[i].Members = append(groups[i].Members, g)
			continue
		}
		idx[sig] = len(groups)
		groups = append(groups, Group{
			Signature: sig,
			State:     g.State,
			Wait:      g.Wait,
			Top:       g.TopFrame(),
			CreatedBy: g.CreatedBy,
			Members:   []Goroutine{g},
		})
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Members) != len(groups[j].Members) {
			return len(groups[i].Members) > len(groups[j].Members)
		}
		return groups[i].Signature < groups[j].Signature
	})
	for i := range groups {
		sort.Slice(groups[i].Members, func(a, b int) bool {
			return groups[i].Members[a].ID < groups[i].Members[b].ID
		})
	}
	return groups
}

// StateCounts returns the number of goroutines in each state.
func StateCounts(gs []Goroutine) map[string]int {
	m := map[string]int{}
	for _, g := range gs {
		m[g.State]++
	}
	return m
}
