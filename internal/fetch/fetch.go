// Package fetch retrieves goroutine dumps from a running Go process's
// net/http/pprof endpoint.
package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hiroyukim/gshow/internal/goroutine"
)

// Client polls a single target's goroutine profile endpoint.
type Client struct {
	URL        string
	HTTPClient *http.Client
}

// NewClient builds a Client for the given pprof base address, e.g.
// "localhost:6060" or "http://localhost:6060". The address may already
// include the "/debug/pprof/goroutine" path, in which case it is used as-is.
func NewClient(addr string, timeout time.Duration) *Client {
	url := normalizeURL(addr)
	return &Client{
		URL:        url,
		HTTPClient: &http.Client{Timeout: timeout},
	}
}

func normalizeURL(addr string) string {
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	if strings.Contains(addr, "/debug/pprof/goroutine") {
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
	return addr + "/debug/pprof/goroutine?debug=2"
}

// Fetch retrieves and parses the current goroutine dump from the target.
func (c *Client) Fetch(ctx context.Context) ([]goroutine.Goroutine, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", c.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("fetch %s: unexpected status %s: %s", c.URL, resp.Status, strings.TrimSpace(string(body)))
	}
	gs, err := goroutine.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse dump from %s: %w", c.URL, err)
	}
	return gs, nil
}
