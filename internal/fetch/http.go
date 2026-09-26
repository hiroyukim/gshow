package fetch

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hiroyukim/gshow/internal/goroutine"
	"github.com/hiroyukim/gshow/probe"
)

// HTTPSource polls a goroutine profile endpoint over HTTP: either
// net/http/pprof's /debug/pprof/goroutine?debug=2, or gshow's own probe
// package serving the same format.
type HTTPSource struct {
	url    string
	client *http.Client
}

// NewHTTPSource builds an HTTPSource for the given pprof base address, e.g.
// "localhost:6060" or "http://localhost:6060". The address may already
// include the "/debug/pprof/goroutine" path, in which case it is used as-is.
func NewHTTPSource(addr string, timeout time.Duration) *HTTPSource {
	return &HTTPSource{
		url:    normalizeURL(addr),
		client: &http.Client{Timeout: timeout},
	}
}

// NewUnixSource builds a Source that polls a gshow probe (see the probe
// package) listening on a Unix domain socket at socketPath, rather than a
// TCP port. This is how you attach to a process that isn't reachable over
// the network at all, e.g. a sidecar dialing a socket shared over a mounted
// volume.
func NewUnixSource(socketPath string, timeout time.Duration) *HTTPSource {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &HTTPSource{
		url:    "http://unix" + probe.Path,
		client: &http.Client{Timeout: timeout, Transport: transport},
	}
}

func normalizeURL(addr string) string {
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	if strings.Contains(addr, probe.Path) {
		if !strings.Contains(addr, "debug=") {
			sep := "?"
			if strings.Contains(addr, "?") {
				sep = "&"
			}
			addr += sep + "debug=2"
		}
		return addr
	}
	addr = strings.TrimRight(addr, "/")
	return addr + probe.Path + "?debug=2"
}

func (s *HTTPSource) Target() string { return s.url }

// Fetch retrieves and parses the current goroutine dump from the target.
func (s *HTTPSource) Fetch(ctx context.Context) ([]goroutine.Goroutine, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", s.url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("fetch %s: unexpected status %s: %s", s.url, resp.Status, strings.TrimSpace(string(body)))
	}
	gs, err := goroutine.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse dump from %s: %w", s.url, err)
	}
	return gs, nil
}
