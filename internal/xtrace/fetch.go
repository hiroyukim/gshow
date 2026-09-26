package xtrace

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Fetch captures an execution trace from addr for the given duration. addr
// may be a bare host:port (assumed to serve /debug/pprof/trace, either via
// net/http/pprof or gshow's probe package) or a full URL.
func Fetch(ctx context.Context, addr string, duration time.Duration) ([]byte, error) {
	url := normalizeURL(addr, duration)
	client := &http.Client{Timeout: duration + 10*time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("fetch %s: unexpected status %s: %s", url, resp.Status, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func normalizeURL(addr string, duration time.Duration) string {
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	if !strings.Contains(addr, "/debug/pprof/trace") {
		addr = strings.TrimRight(addr, "/") + "/debug/pprof/trace"
	}
	sep := "?"
	if strings.Contains(addr, "?") {
		sep = "&"
	}
	return addr + sep + "seconds=" + strconv.FormatFloat(duration.Seconds(), 'f', -1, 64)
}
