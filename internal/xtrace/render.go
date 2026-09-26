package xtrace

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	rtrace "golang.org/x/exp/trace"
)

var (
	runningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("120"))
	runnableStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("221"))
	waitingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	syscallStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
	idStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
)

const (
	runningGlyph  = '█'
	runnableGlyph = '▒'
	waitingGlyph  = '·'
	syscallGlyph  = '▓'
	emptyGlyph    = ' '
)

func glyph(state string) (rune, lipgloss.Style) {
	switch state {
	case "Running":
		return runningGlyph, runningStyle
	case "Runnable":
		return runnableGlyph, runnableStyle
	case "Syscall":
		return syscallGlyph, syscallStyle
	case "Waiting":
		return waitingGlyph, waitingStyle
	default:
		return '?', dimStyle
	}
}

// stateAt returns the state active at time t, or "" if the goroutine
// wasn't alive yet / anymore.
func (g *Goroutine) stateAt(t time.Duration) string {
	// Spans are in chronological order; a linear scan is fine for the
	// goroutine counts a terminal can usefully display anyway.
	for _, s := range g.Spans {
		if t >= s.Start && t < s.End {
			return s.State
		}
	}
	if n := len(g.Spans); n > 0 && t == g.Spans[n-1].End {
		return g.Spans[n-1].State
	}
	return ""
}

// creatorLabel picks the most useful frame to summarize a goroutine by. The
// leaf frame (CreatedBy[0]) is where the tracer caught it - for a blocked
// goroutine that's always some internal park function (runtime.gopark and
// friends), never anything from the goroutine's own code. The outermost
// frame, close to wherever "go func(){...}" was called, is far more likely
// to say what the goroutine actually is.
func creatorLabel(g *Goroutine) string {
	for _, frame := range slices.Backward(g.CreatedBy) {
		if f := frame.Func; f != "" && !isRuntimeFunc(f) {
			return f
		}
	}
	if len(g.CreatedBy) > 0 {
		return g.CreatedBy[len(g.CreatedBy)-1].Func
	}
	return "-"
}

// dominantReason returns the Reason of whichever span covered the most of
// the capture window, e.g. "chan receive" or "select" - a hint at what a
// mostly-Waiting goroutine is actually waiting on.
func dominantReason(g *Goroutine) string {
	var best Span
	var bestDur time.Duration
	for _, s := range g.Spans {
		if s.Reason == "" {
			continue
		}
		if d := s.End - s.Start; d > bestDur {
			best, bestDur = s, d
		}
	}
	return best.Reason
}

func isRuntimeFunc(fn string) bool {
	return strings.HasPrefix(fn, "runtime.") ||
		strings.HasPrefix(fn, "runtime/trace.") ||
		strings.HasPrefix(fn, "runtime/pprof.")
}

// IsRuntimeInternal reports whether a goroutine appears to be GC or trace
// machinery rather than anything the target program's own code spawned -
// noise that clutters every capture regardless of target, filtered out by
// Render's default view.
func (g *Goroutine) IsRuntimeInternal() bool {
	return isRuntimeFunc(creatorLabel(g))
}

// Render draws a text timeline: one row per goroutine (most active first,
// capped at maxRows), each a bar of width timelineWidth showing which state
// it was in at each point in the capture. totalWidth bounds the whole line
// (label + bar); the label column shrinks to fit. Goroutines that look like
// GC or trace machinery rather than target-program code are hidden unless
// all is true - see (*Goroutine).IsRuntimeInternal.
func Render(t *Trace, totalWidth, maxRows int, all bool) string {
	if len(t.Goroutines) == 0 {
		return "no goroutine activity captured"
	}
	ids := make([]rtrace.GoID, 0, len(t.Goroutines))
	hidden := 0
	for id, g := range t.Goroutines {
		if !all && g.IsRuntimeInternal() {
			hidden++
			continue
		}
		ids = append(ids, id)
	}
	// Most active first: goroutines that changed state more often during
	// the window are more likely to be doing something interesting than
	// ones that just sat in a single state the whole time - busy
	// (running/syscall) time alone is often ~0 for every goroutine in an
	// I/O-bound program, which would make that ordering meaningless.
	sort.Slice(ids, func(i, j int) bool {
		gi, gj := t.Goroutines[ids[i]], t.Goroutines[ids[j]]
		if len(gi.Spans) != len(gj.Spans) {
			return len(gi.Spans) > len(gj.Spans)
		}
		return gi.TotalBusy() > gj.TotalBusy()
	})

	labelWidth := 34
	timelineWidth := max(totalWidth-labelWidth-1, 10)
	bucket := t.Duration / time.Duration(timelineWidth)
	if bucket <= 0 {
		bucket = 1
	}

	var b strings.Builder
	fmt.Fprintf(&b, "captured %s across %d goroutines (most active first, showing up to %d)",
		t.Duration.Round(time.Millisecond), len(ids), maxRows)
	if hidden > 0 {
		fmt.Fprintf(&b, " - %d runtime/GC housekeeping goroutines hidden, pass -all to show them", hidden)
	}
	b.WriteString("\n\n")

	shown := ids
	if len(shown) > maxRows {
		shown = shown[:maxRows]
	}
	for _, id := range shown {
		g := t.Goroutines[id]
		label := fmt.Sprintf("g%-7d %s", g.ID, creatorLabel(g))
		if reason := dominantReason(g); reason != "" {
			label += " (" + reason + ")"
		}
		if len(label) > labelWidth {
			label = label[:labelWidth-1] + "…"
		} else {
			label = label + strings.Repeat(" ", labelWidth-len(label))
		}

		var bar strings.Builder
		for i := range timelineWidth {
			t0 := time.Duration(i) * bucket
			state := g.stateAt(t0 + bucket/2)
			r, style := ' ', dimStyle
			if state != "" {
				r, style = glyph(state)
			} else {
				r = emptyGlyph
			}
			bar.WriteString(style.Render(string(r)))
		}
		b.WriteString(idStyle.Render(label))
		b.WriteByte(' ')
		b.WriteString(bar.String())
		b.WriteByte('\n')
	}

	fmt.Fprintf(&b, "\n%s running   %s runnable  %s syscall   %s waiting   %s not alive\n",
		runningStyle.Render(string(runningGlyph)),
		runnableStyle.Render(string(runnableGlyph)),
		syscallStyle.Render(string(syscallGlyph)),
		waitingStyle.Render(string(waitingGlyph)),
		dimStyle.Render(string(emptyGlyph)))
	return b.String()
}
