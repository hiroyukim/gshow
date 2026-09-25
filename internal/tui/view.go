package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hiroyukim/gshow/internal/goroutine"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Padding(0, 1)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	errStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	upStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("120"))
	downStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
)

func (m Model) View() string {
	if !m.ready {
		return "starting gshow…"
	}

	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteByte('\n')

	if m.filtering {
		b.WriteString(m.filterInput.View())
		b.WriteByte('\n')
	}

	switch m.view {
	case viewDetail:
		b.WriteString(m.detailVP.View())
		b.WriteByte('\n')
	default:
		b.WriteString(m.table.View())
		b.WriteByte('\n')
		b.WriteString(m.renderEvents())
		b.WriteByte('\n')
	}

	b.WriteString(m.renderFooter())
	return b.String()
}

func (m Model) renderHeader() string {
	title := titleStyle.Render(" gshow ")
	delta := m.total - m.prevTotal
	deltaStr := fmt.Sprintf("%d", delta)
	deltaRendered := deltaStr
	switch {
	case delta > 0:
		deltaRendered = upStyle.Render("+" + deltaStr)
	case delta < 0:
		deltaRendered = downStyle.Render(deltaStr)
	default:
		deltaRendered = dimStyle.Render("±0")
	}

	status := fmt.Sprintf("target %s   goroutines %d (%s)   updated %s",
		m.target, m.total, deltaRendered, m.lastUpdate.Format("15:04:05"))
	if m.fetching {
		status += dimStyle.Render("  …")
	}
	line1 := title + "  " + status

	line2 := ""
	if m.err != nil {
		line2 = errStyle.Render("error: " + m.err.Error())
	} else if m.filter != "" {
		line2 = dimStyle.Render(fmt.Sprintf("filter: %q  (%d/%d groups shown)", m.filter, len(m.filteredGroups), len(m.groups)))
	} else {
		line2 = dimStyle.Render(fmt.Sprintf("%d distinct stacks", len(m.groups)))
	}
	return line1 + "\n" + line2
}

func (m Model) renderEvents() string {
	var b strings.Builder
	b.WriteString(sectionStyle.Render("recent activity"))
	b.WriteByte('\n')
	n := max(m.eventsHeight, 1)
	start := max(len(m.events)-n, 0)
	shown := m.events[start:]
	if len(shown) == 0 {
		b.WriteString(dimStyle.Render("(waiting for the next poll…)"))
		return b.String()
	}
	lines := make([]string, 0, len(shown))
	for _, e := range shown {
		style := dimStyle
		if strings.HasPrefix(e.text, "+") {
			style = upStyle
		} else if strings.HasPrefix(e.text, "-") {
			style = downStyle
		}
		lines = append(lines, dimStyle.Render(e.at.Format("15:04:05"))+" "+style.Render(e.text))
	}
	slices.Reverse(lines)
	b.WriteString(strings.Join(lines, "\n"))
	return b.String()
}

func (m Model) renderFooter() string {
	switch m.view {
	case viewDetail:
		return helpStyle.Render("↑/↓ pgup/pgdn scroll · esc back · q quit")
	default:
		if m.filtering {
			return helpStyle.Render("enter apply filter · esc cancel")
		}
		return helpStyle.Render("↑/↓ select · enter stack detail · / filter · r refresh now · q quit")
	}
}

func renderDetail(g goroutine.Group) string {
	var b strings.Builder
	b.WriteString(sectionStyle.Render(fmt.Sprintf("%d goroutine(s) — state: %s", g.Count(), g.State)))
	b.WriteByte('\n')
	if g.CreatedBy.Func != "" {
		b.WriteString(dimStyle.Render(fmt.Sprintf("created by %s (%s:%d)", g.CreatedBy.Func, g.CreatedBy.File, g.CreatedBy.Line)))
		b.WriteByte('\n')
	}
	ids := make([]string, len(g.Members))
	for i, mem := range g.Members {
		w := mem.Wait
		if w == "" {
			w = "-"
		}
		ids[i] = fmt.Sprintf("#%d(%s)", mem.ID, w)
	}
	b.WriteString(dimStyle.Render("members: " + strings.Join(ids, ", ")))
	b.WriteString("\n\n")
	b.WriteString(sectionStyle.Render("stack (goroutine " + fmt.Sprint(g.Members[0].ID) + ")"))
	b.WriteByte('\n')
	b.WriteString(strings.TrimRight(g.Members[0].Raw, "\n"))
	return b.String()
}
