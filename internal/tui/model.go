// Package tui is the live, terminal-based goroutine dashboard.
package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hiroyukim/gshow/internal/fetch"
	"github.com/hiroyukim/gshow/internal/goroutine"
)

type view int

const (
	viewList view = iota
	viewDetail
)

const maxEvents = 500

type logEvent struct {
	at   time.Time
	text string
}

// Model is the bubbletea program state for the live goroutine dashboard.
type Model struct {
	src      fetch.Source
	interval time.Duration
	target   string

	width, height int
	ready         bool

	view     view
	table    table.Model
	detailVP viewport.Model

	filterInput textinput.Model
	filtering   bool
	filter      string

	groups         []goroutine.Group
	filteredGroups []goroutine.Group

	prev   map[int]goroutine.Goroutine
	events []logEvent

	total, prevTotal int
	lastUpdate       time.Time
	err              error
	fetching         bool

	eventsHeight int
}

// New builds the dashboard model for the given source and poll interval.
func New(src fetch.Source, interval time.Duration) Model {
	cols := []table.Column{
		{Title: "STATE", Width: 16},
		{Title: "COUNT", Width: 5},
		{Title: "WAIT", Width: 10},
		{Title: "CREATED BY", Width: 28},
		{Title: "TOP FRAME", Width: 30},
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
	)
	ts := table.DefaultStyles()
	ts.Header = ts.Header.Bold(true).BorderBottom(true).BorderStyle(lipgloss.NormalBorder())
	ts.Selected = ts.Selected.Bold(true).Background(lipgloss.Color("62")).Foreground(lipgloss.Color("230"))
	t.SetStyles(ts)

	fi := textinput.New()
	fi.Prompt = "/"
	fi.Placeholder = "filter by state, created-by or function..."

	return Model{
		src:         src,
		interval:    interval,
		target:      src.Target(),
		table:       t,
		filterInput: fi,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(fetchCmd(m.src), tickCmd(m.interval))
}

type tickMsg time.Time

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type fetchResultMsg struct {
	gs  []goroutine.Goroutine
	err error
	at  time.Time
}

func fetchCmd(src fetch.Source) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		gs, err := src.Fetch(ctx)
		return fetchResultMsg{gs: gs, err: err, at: time.Now()}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.layout()
		return m, nil

	case tickMsg:
		if m.fetching {
			return m, tickCmd(m.interval)
		}
		m.fetching = true
		return m, tea.Batch(fetchCmd(m.src), tickCmd(m.interval))

	case fetchResultMsg:
		m.fetching = false
		m.lastUpdate = msg.at
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.applyFetch(msg.gs)
		m.layout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) layout() {
	headerH := 2
	footerH := 2
	filterH := 0
	if m.filtering {
		filterH = 1
	}
	body := max(m.height-headerH-footerH-filterH, 6)
	// Size the table to what it actually has to show (capped), and give the
	// rest of the body to the recent-activity log rather than padding the
	// table with empty rows.
	tableH := min(max(len(m.filteredGroups), 3), min(body-4, 15))
	eventsH := max(body-tableH-1, 1) // -1 for the "recent activity" title line
	m.table.SetHeight(tableH)
	m.table.SetWidth(m.width)
	m.eventsHeight = eventsH
	m.detailVP.Width = m.width
	m.detailVP.Height = body
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filtering = false
			m.filterInput.Blur()
			m.filterInput.SetValue("")
			m.filter = ""
			m.refilter()
			return m, nil
		case "enter":
			m.filtering = false
			m.filterInput.Blur()
			m.filter = strings.TrimSpace(m.filterInput.Value())
			m.refilter()
			return m, nil
		}
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "r":
		if !m.fetching {
			m.fetching = true
			return m, fetchCmd(m.src)
		}
		return m, nil
	case "/":
		m.filtering = true
		m.filterInput.Focus()
		m.layout()
		return m, textinput.Blink
	case "enter":
		if m.view == viewList && len(m.filteredGroups) > 0 {
			m.view = viewDetail
			m.detailVP.SetContent(renderDetail(m.filteredGroups[m.table.Cursor()]))
			m.detailVP.GotoTop()
		}
		return m, nil
	case "esc":
		if m.view == viewDetail {
			m.view = viewList
		}
		return m, nil
	}

	var cmd tea.Cmd
	if m.view == viewList {
		m.table, cmd = m.table.Update(msg)
	} else {
		m.detailVP, cmd = m.detailVP.Update(msg)
	}
	return m, cmd
}

// applyFetch folds a new dump into the model: it diffs against the previous
// poll to log goroutine churn, regroups, and refreshes the table.
func (m *Model) applyFetch(gs []goroutine.Goroutine) {
	curr := make(map[int]goroutine.Goroutine, len(gs))
	for _, g := range gs {
		curr[g.ID] = g
	}

	if m.prev != nil {
		var started, finished []goroutine.Goroutine
		for id, g := range curr {
			if _, ok := m.prev[id]; !ok {
				started = append(started, g)
			}
		}
		for id, g := range m.prev {
			if _, ok := curr[id]; !ok {
				finished = append(finished, g)
			}
		}
		m.logChurn("started", started)
		m.logChurn("finished", finished)
	}
	m.prev = curr

	m.prevTotal = m.total
	m.total = len(gs)
	m.groups = goroutine.GroupBySignature(gs)
	m.refilter()
}

func createdByKey(g goroutine.Goroutine) string {
	if g.CreatedBy.Func != "" {
		return g.CreatedBy.FuncName()
	}
	if len(g.Stack) > 0 {
		return "(no creator) " + g.Stack[0].FuncName()
	}
	return "(unknown)"
}

func (m *Model) logChurn(verb string, gs []goroutine.Goroutine) {
	if len(gs) == 0 {
		return
	}
	counts := map[string]int{}
	for _, g := range gs {
		counts[createdByKey(g)]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sign := "+"
	if verb == "finished" {
		sign = "-"
	}
	now := time.Now()
	for _, k := range keys {
		text := fmt.Sprintf("%s%d %s  created by %s", sign, counts[k], verb, k)
		m.events = append(m.events, logEvent{at: now, text: text})
	}
	if len(m.events) > maxEvents {
		m.events = m.events[len(m.events)-maxEvents:]
	}
}

func (m *Model) refilter() {
	if m.filter == "" {
		m.filteredGroups = m.groups
	} else {
		q := strings.ToLower(m.filter)
		m.filteredGroups = m.filteredGroups[:0]
		for _, g := range m.groups {
			hay := strings.ToLower(g.State + " " + g.CreatedBy.Func + " " + g.Top.Func)
			if strings.Contains(hay, q) {
				m.filteredGroups = append(m.filteredGroups, g)
			}
		}
	}
	rows := make([]table.Row, len(m.filteredGroups))
	for i, g := range m.filteredGroups {
		rows[i] = table.Row{
			g.State,
			fmt.Sprintf("%d", g.Count()),
			nz(g.Wait),
			nz(shorten(createdByLabel(g), 28)),
			nz(shorten(g.Top.FuncName(), 30)),
		}
	}
	m.table.SetRows(rows)
	if m.table.Cursor() >= len(rows) && len(rows) > 0 {
		m.table.SetCursor(len(rows) - 1)
	}
}

func createdByLabel(g goroutine.Group) string {
	if g.CreatedBy.Func == "" {
		return ""
	}
	return g.CreatedBy.FuncName()
}

func nz(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
