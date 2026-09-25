package goroutine

import (
	"strings"
	"testing"
)

const sampleDump = `goroutine 1 [chan receive]:
main.main()
	/home/user/app/main.go:20 +0x105

goroutine 18 [IO wait, 5 minutes]:
internal/poll.runtime_pollWait(0x7f1, 0x72)
	/usr/local/go/src/runtime/netpoll.go:343 +0x85
internal/poll.(*pollDesc).wait(0xc000102000, 0x72, 0x0)
	/usr/local/go/src/internal/poll/fd_poll_runtime.go:84 +0x27
net.(*netFD).Read(0xc000102000, {0xc0001100, 0x1000, 0x1000})
	/usr/local/go/src/net/fd_posix.go:55 +0x25
main.handleConn(...)
	/home/user/app/server.go:40
created by main.serve in goroutine 1
	/home/user/app/server.go:30 +0x65

goroutine 19 [IO wait, 5 minutes]:
internal/poll.runtime_pollWait(0x7f2, 0x72)
	/usr/local/go/src/runtime/netpoll.go:343 +0x85
internal/poll.(*pollDesc).wait(0xc000102100, 0x72, 0x0)
	/usr/local/go/src/internal/poll/fd_poll_runtime.go:84 +0x27
net.(*netFD).Read(0xc000102100, {0xc0001200, 0x1000, 0x1000})
	/usr/local/go/src/net/fd_posix.go:55 +0x25
main.handleConn(...)
	/home/user/app/server.go:40
created by main.serve in goroutine 1
	/home/user/app/server.go:30 +0x65

goroutine 25 [select, 2 minutes, locked to thread]:
main.ticker(0xc000010018)
	/home/user/app/worker.go:60 +0x145
created by main.main in goroutine 1
	/home/user/app/main.go:22 +0x90
`

func TestParse(t *testing.T) {
	gs, err := Parse(strings.NewReader(sampleDump))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(gs) != 4 {
		t.Fatalf("got %d goroutines, want 4", len(gs))
	}

	g0 := gs[0]
	if g0.ID != 1 || g0.State != "chan receive" {
		t.Errorf("g0 = %+v", g0)
	}
	if len(g0.Stack) != 1 || g0.Stack[0].Func != "main.main()" || g0.Stack[0].Line != 20 {
		t.Errorf("g0.Stack = %+v", g0.Stack)
	}

	g1 := gs[1]
	if g1.ID != 18 || g1.State != "IO wait" || g1.Wait != "5 minutes" {
		t.Errorf("g1 = %+v", g1)
	}
	if len(g1.Stack) != 4 {
		t.Fatalf("g1.Stack len = %d, want 4", len(g1.Stack))
	}
	if g1.CreatedBy.Func != "main.serve" || g1.CreatedBy.Line != 30 {
		t.Errorf("g1.CreatedBy = %+v", g1.CreatedBy)
	}
	if g1.InGor != 1 {
		t.Errorf("g1.InGor = %d, want 1", g1.InGor)
	}

	g3 := gs[3]
	if g3.State != "select" || g3.Wait != "2 minutes" || g3.Extra != "locked to thread" {
		t.Errorf("g3 = %+v", g3)
	}
}

func TestGroupBySignature(t *testing.T) {
	gs, err := Parse(strings.NewReader(sampleDump))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	groups := GroupBySignature(gs)
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	// The two identical IO-wait goroutines (18, 19) should collapse into one
	// group of 2, sorted first since it's the largest.
	if groups[0].Count() != 2 {
		t.Errorf("groups[0].Count() = %d, want 2", groups[0].Count())
	}
	if groups[0].Members[0].ID != 18 || groups[0].Members[1].ID != 19 {
		t.Errorf("groups[0].Members = %+v", groups[0].Members)
	}
}

func TestFrameFuncName(t *testing.T) {
	cases := map[string]string{
		"main.handleConn(...)":                                               "main.handleConn",
		"internal/poll.runtime_pollWait(0x7f1, 0x72)":                        "internal/poll.runtime_pollWait",
		"net/http.(*Server).Serve(0x1dbc033e000, {0x9f7768, 0x1dbc02662c0})": "net/http.(*Server).Serve",
		"main.main()":    "main.main",
		"runtime.goexit": "runtime.goexit",
	}
	for in, want := range cases {
		f := Frame{Func: in}
		if got := f.FuncName(); got != want {
			t.Errorf("FuncName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseHeaderMalformed(t *testing.T) {
	if _, err := parseBlock("not a goroutine header\n"); err == nil {
		t.Error("expected error for malformed header")
	}
}
