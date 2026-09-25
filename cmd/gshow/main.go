// Command gshow is a live terminal dashboard for a running Go process's
// goroutines. It polls the target's net/http/pprof goroutine endpoint,
// groups goroutines that are doing the identical thing, and shows what
// changed between polls.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hiroyukim/gshow/internal/fetch"
	"github.com/hiroyukim/gshow/internal/tui"
)

func main() {
	addr := flag.String("addr", "localhost:6060", "target process's pprof address, e.g. localhost:6060 or a full /debug/pprof/goroutine URL")
	interval := flag.Duration("interval", time.Second, "how often to poll the target")
	timeout := flag.Duration("timeout", 5*time.Second, "HTTP timeout per poll")
	flag.Parse()

	client := fetch.NewClient(*addr, *timeout)
	m := tui.New(client, *interval)

	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gshow:", err)
		os.Exit(1)
	}
}
