package fetch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/hiroyukim/gshow/internal/goroutine"
)

// FileSource reads a goroutine dump from a file, or from stdin if path is
// "-". It's for looking at a single saved snapshot (e.g. one collected by
// curl'ing a pprof endpoint, or from a process that dumps goroutines to a
// file on SIGQUIT) rather than a live process. Live polling still runs
// against it, but stdin is only readable once so it's cached after the
// first read; a regular file is re-read on every poll, so overwriting it
// between polls (e.g. from a cron job) does update the view.
type FileSource struct {
	path string

	once   sync.Once
	stdin  []byte
	stdErr error
}

// NewFileSource builds a Source that reads a dump from path ("-" for stdin).
func NewFileSource(path string) *FileSource {
	return &FileSource{path: path}
}

func (s *FileSource) Target() string {
	if s.path == "-" {
		return "stdin"
	}
	return s.path
}

func (s *FileSource) Fetch(ctx context.Context) ([]goroutine.Goroutine, error) {
	var r io.Reader
	if s.path == "-" {
		s.once.Do(func() { s.stdin, s.stdErr = io.ReadAll(os.Stdin) })
		if s.stdErr != nil {
			return nil, fmt.Errorf("read stdin: %w", s.stdErr)
		}
		r = bytes.NewReader(s.stdin)
	} else {
		f, err := os.Open(s.path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", s.path, err)
		}
		defer f.Close()
		r = f
	}
	gs, err := goroutine.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse dump from %s: %w", s.Target(), err)
	}
	return gs, nil
}
