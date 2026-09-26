// Command gshow is a live terminal dashboard for a running Go process's
// goroutines. It polls the target's goroutine profile, groups goroutines
// that are doing the identical thing, and shows what changed between polls.
//
// The target can be reached three ways: an HTTP pprof-style endpoint
// (-addr, works against net/http/pprof or gshow's own probe package), a
// gshow probe on a Unix domain socket (-socket, see the probe package), or
// a saved dump file (-file, "-" for stdin).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hiroyukim/gshow/internal/fetch"
	"github.com/hiroyukim/gshow/internal/goroutine"
	"github.com/hiroyukim/gshow/internal/tui"
)

func main() {
	addr := flag.String("addr", "", "target process's pprof address, e.g. localhost:6060 or a full /debug/pprof/goroutine URL (default localhost:6060 unless -socket or -file is given)")
	socket := flag.String("socket", "", "path to a gshow probe Unix domain socket (see the probe package)")
	file := flag.String("file", "", `path to a saved goroutine dump ("-" for stdin) instead of a live target`)
	interval := flag.Duration("interval", time.Second, "how often to poll the target (ignored with -json)")
	timeout := flag.Duration("timeout", 5*time.Second, "HTTP timeout per poll")
	jsonOut := flag.Bool("json", false, "fetch one snapshot, print it as a JSON report on stdout, and exit instead of launching the live dashboard")
	flag.Parse()

	src, err := buildSource(*addr, *socket, *file, *timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gshow:", err)
		os.Exit(2)
	}

	if *jsonOut {
		if err := printSnapshot(src, *timeout); err != nil {
			fmt.Fprintln(os.Stderr, "gshow:", err)
			os.Exit(1)
		}
		return
	}

	m := tui.New(src, *interval)
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gshow:", err)
		os.Exit(1)
	}
}

func printSnapshot(src fetch.Source, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	gs, err := src.Fetch(ctx)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(goroutine.BuildReport(gs, src.Target()))
}

func buildSource(addr, socket, file string, timeout time.Duration) (fetch.Source, error) {
	set := 0
	for _, s := range []string{addr, socket, file} {
		if s != "" {
			set++
		}
	}
	if set > 1 {
		return nil, fmt.Errorf("only one of -addr, -socket, -file may be given")
	}
	switch {
	case file != "":
		return fetch.NewFileSource(file), nil
	case socket != "":
		return fetch.NewUnixSource(socket, timeout), nil
	default:
		if addr == "" {
			addr = "localhost:6060"
		}
		return fetch.NewHTTPSource(addr, timeout), nil
	}
}
