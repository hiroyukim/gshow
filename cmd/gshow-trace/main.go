// Command gshow-trace captures a short Go execution trace from a running
// process and renders a per-goroutine timeline in the terminal - a
// finer-grained, event-based view than gshow's periodic goroutine-dump
// dashboard, at the cost of only covering a short capture window instead of
// running continuously.
//
// The target needs the same thing gshow's live dashboard does: either
// net/http/pprof registered, or gshow's own probe package (which serves a
// matching /debug/pprof/trace endpoint) - see the probe package and
// cmd/gshow-build.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/hiroyukim/gshow/internal/xtrace"
)

func main() {
	addr := flag.String("addr", "localhost:6060", "target process's pprof address, e.g. localhost:6060")
	seconds := flag.Float64("seconds", 2, "how long to capture")
	out := flag.String("out", "", "also save the raw trace here, for `go tool trace`")
	width := flag.Int("width", 160, "output width in columns (ignored with -json)")
	maxRows := flag.Int("rows", 40, "max goroutines to show, most active first")
	all := flag.Bool("all", false, "also show goroutines that look like GC/trace runtime machinery")
	jsonOut := flag.Bool("json", false, "print a machine-readable JSON report on stdout instead of the text timeline")
	flag.Parse()

	duration := time.Duration(*seconds * float64(time.Second))
	fmt.Fprintf(os.Stderr, "capturing a %s execution trace from %s...\n", duration, *addr)

	ctx, cancel := context.WithTimeout(context.Background(), duration+15*time.Second)
	defer cancel()
	data, err := xtrace.Fetch(ctx, *addr, duration)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gshow-trace:", err)
		os.Exit(1)
	}

	if *out != "" {
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "gshow-trace:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "saved raw trace to %s (open with: go tool trace %s)\n", *out, *out)
	}

	tr, err := xtrace.Parse(bytes.NewReader(data))
	if err != nil {
		fmt.Fprintln(os.Stderr, "gshow-trace: parsing trace:", err)
		os.Exit(1)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(xtrace.BuildReport(tr, *addr, *maxRows, *all)); err != nil {
			fmt.Fprintln(os.Stderr, "gshow-trace: encoding report:", err)
			os.Exit(1)
		}
		return
	}

	fmt.Print(xtrace.Render(tr, *width, *maxRows, *all))
}
