// Package goroutine parses the text produced by a Go process's
// /debug/pprof/goroutine?debug=2 endpoint into structured records.
package goroutine

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Frame is a single stack frame: the called function and its source location.
type Frame struct {
	Func string
	File string
	Line int
}

func (f Frame) String() string {
	return fmt.Sprintf("%s\n\t%s:%d", f.Func, f.File, f.Line)
}

// FuncName returns the called function without its argument list, e.g.
// "net/http.(*Server).Serve" for "net/http.(*Server).Serve(0xc0000, {0x1, 0x2})".
// Argument values are raw memory addresses that differ between otherwise
// identical goroutines, so callers that need to compare *what* a frame is
// doing (rather than its exact arguments) should use this instead of Func.
//
// The argument list is stripped by matching the closing ')' at the end of
// the line back to its opening '(', rather than cutting at the first '(':
// a pointer-receiver method like "(*Server).Serve" has its own parens that
// a naive first-'(' cut would mistake for the start of the argument list.
func (f Frame) FuncName() string {
	s := f.Func
	if !strings.HasSuffix(s, ")") {
		return s
	}
	depth := 0
	for i := len(s) - 1; i >= 0; i-- {
		switch s[i] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				return s[:i]
			}
		}
	}
	return s
}

// Goroutine is one goroutine's state at the moment the dump was taken.
type Goroutine struct {
	ID    int
	State string // e.g. "running", "chan receive", "IO wait", "select"
	Wait  string // e.g. "5 minutes"; empty if the state has no duration
	Extra string // extra state flags, e.g. "locked to thread"

	Stack     []Frame
	CreatedBy Frame  // where this goroutine was spawned; zero value if unknown (e.g. goroutine 1)
	InGor     int    // the goroutine ID that created this one, 0 if unknown
	Raw       string // the raw dump block, for display
}

// StackSignature is a stable key identifying goroutines that are doing the
// identical thing: same state and same call stack, ignoring goroutine ID,
// wait duration, PC offsets, and argument values (which are raw addresses
// that differ even between goroutines running identical code).
func (g Goroutine) StackSignature() string {
	var b strings.Builder
	b.WriteString(g.State)
	b.WriteByte('\n')
	for _, f := range g.Stack {
		b.WriteString(f.FuncName())
		b.WriteByte('\t')
		b.WriteString(f.File)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(f.Line))
		b.WriteByte('\n')
	}
	sum := sha1.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// TopFrame returns the outermost (currently executing) frame, or the zero
// Frame if the stack is empty.
func (g Goroutine) TopFrame() Frame {
	if len(g.Stack) == 0 {
		return Frame{}
	}
	return g.Stack[0]
}

// Parse reads a full goroutine dump (as produced by debug=2) and returns
// every goroutine found in it, in the order they appear.
func Parse(r io.Reader) ([]Goroutine, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var out []Goroutine
	var cur *strings.Builder

	flush := func() error {
		if cur == nil {
			return nil
		}
		g, err := parseBlock(cur.String())
		if err != nil {
			return err
		}
		out = append(out, g)
		cur = nil
		return nil
	}

	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "goroutine ") {
			if err := flush(); err != nil {
				return nil, err
			}
			cur = &strings.Builder{}
		}
		if cur != nil {
			cur.WriteString(line)
			cur.WriteByte('\n')
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseBlock(block string) (Goroutine, error) {
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	if len(lines) == 0 {
		return Goroutine{}, fmt.Errorf("empty goroutine block")
	}

	g := Goroutine{Raw: block}

	// Header: "goroutine 5 [chan receive, 3 minutes, locked to thread]:"
	header := lines[0]
	id, state, wait, extra, err := parseHeader(header)
	if err != nil {
		return Goroutine{}, err
	}
	g.ID, g.State, g.Wait, g.Extra = id, state, wait, extra

	i := 1
	for i < len(lines) {
		line := lines[i]
		if line == "" {
			i++
			continue
		}
		if created, ok := strings.CutPrefix(line, "created by "); ok {
			if idx := strings.Index(created, " in goroutine "); idx >= 0 {
				if n, err := strconv.Atoi(strings.TrimSpace(created[idx+len(" in goroutine "):])); err == nil {
					g.InGor = n
				}
				created = created[:idx]
			}
			if i+1 < len(lines) {
				file, ln := parseLoc(lines[i+1])
				g.CreatedBy = Frame{Func: created, File: file, Line: ln}
				i += 2
				continue
			}
			g.CreatedBy = Frame{Func: created}
			i++
			continue
		}
		// Function call line, e.g. "main.worker(0xc0000140a0)"
		fn := line
		if i+1 < len(lines) {
			file, ln := parseLoc(lines[i+1])
			g.Stack = append(g.Stack, Frame{Func: fn, File: file, Line: ln})
			i += 2
			continue
		}
		g.Stack = append(g.Stack, Frame{Func: fn})
		i++
	}

	return g, nil
}

// parseHeader parses "goroutine 5 [chan receive, 3 minutes, locked to thread]:"
func parseHeader(line string) (id int, state, wait, extra string, err error) {
	line = strings.TrimPrefix(line, "goroutine ")
	idStr, remainder, ok := strings.Cut(line, " ")
	if !ok {
		return 0, "", "", "", fmt.Errorf("malformed goroutine header: %q", line)
	}
	id, err = strconv.Atoi(idStr)
	if err != nil {
		return 0, "", "", "", fmt.Errorf("malformed goroutine id: %q", idStr)
	}
	rest := strings.TrimSpace(remainder)
	if !strings.HasPrefix(rest, "[") {
		return 0, "", "", "", fmt.Errorf("malformed goroutine header: %q", line)
	}
	rest = strings.TrimSuffix(strings.TrimPrefix(rest, "["), "]:")
	parts := strings.Split(rest, ", ")
	state = parts[0]
	if len(parts) > 1 {
		for _, p := range parts[1:] {
			if strings.HasSuffix(p, "seconds") || strings.HasSuffix(p, "minutes") || strings.HasSuffix(p, "second") || strings.HasSuffix(p, "minute") || strings.HasSuffix(p, "hours") {
				wait = p
			} else {
				if extra != "" {
					extra += ", "
				}
				extra += p
			}
		}
	}
	return id, state, wait, extra, nil
}

// parseLoc parses a location line, e.g. "\t/usr/local/go/src/runtime/proc.go:398 +0x1a"
func parseLoc(line string) (file string, ln int) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", 0
	}
	// drop trailing " +0x1a"
	if idx := strings.LastIndex(line, " +0x"); idx >= 0 {
		line = line[:idx]
	}
	colon := strings.LastIndexByte(line, ':')
	if colon < 0 {
		return line, 0
	}
	file = line[:colon]
	n, err := strconv.Atoi(line[colon+1:])
	if err != nil {
		return line, 0
	}
	return file, n
}
