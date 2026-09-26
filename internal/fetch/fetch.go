// Package fetch retrieves goroutine dumps from wherever a target process
// makes them available: an HTTP pprof-style endpoint (net/http/pprof or
// gshow's own probe package), a gshow probe listening on a Unix domain
// socket, or a dump saved to a file.
package fetch

import (
	"context"

	"github.com/hiroyukim/gshow/internal/goroutine"
)

// Source is anything gshow can poll for a goroutine dump.
type Source interface {
	// Target is a short, human-readable label for what this source reads
	// from, shown in the dashboard header.
	Target() string
	// Fetch retrieves and parses the current goroutine dump.
	Fetch(ctx context.Context) ([]goroutine.Goroutine, error)
}
