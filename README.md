# gshow

A toolkit for watching what the goroutines in a running Go process are
actually doing — right now, as it happens (goroutines spawned per HTTP
request, workers blocked on a channel, leaks that never exit, etc), or over
a short precise window if you need finer detail than a snapshot gives.

Two complementary tools:

- **`gshow`** — a live terminal dashboard. Polls a target for a goroutine
  dump on an interval, groups goroutines doing the identical thing, and
  shows what changed between polls.
- **`gshow-trace`** — a one-shot execution-trace timeline. Captures a short
  window (seconds) of Go's execution tracer and renders a per-goroutine bar
  chart of exactly when each one was running, runnable, blocked, or in a
  syscall.

Plus `probe` / `gshow-build` for getting either tool into a target process
that doesn't already expose the data they need.

## Quick start

A demo server is included if you don't have a target handy — it exposes
`net/http/pprof` and has two endpoints worth poking at: `/work` spawns one
short-lived goroutine per request (the classic "fire and forget" pattern),
and `/leak` spawns one that never exits.

```sh
go run ./cmd/demo
```

In another terminal:

```sh
go run ./cmd/gshow
```

Then generate some traffic and watch it show up live:

```sh
for i in $(seq 1 6); do curl -s http://localhost:6060/work >/dev/null & done
curl -s http://localhost:6060/leak >/dev/null
```

## `gshow`: the live dashboard

It polls a goroutine dump every second (configurable), groups goroutines
that share the identical state and call stack, and logs which groups
gained or lost members since the last poll — so a burst of per-request
goroutines shows up as a `+N started` / `-N finished` line as they come and
go, instead of getting lost in a wall of individual stack traces.

### Sample output

Captured against the demo server, seconds after firing 6 `/work` requests
and 1 `/leak` request:

```
  gshow    target http://localhost:6060/debug/pprof/goroutine?debug=2   goroutines 14 (-1)   updated 18:05:07
7 distinct stacks
 STATE             COUNT  WAIT        CREATED BY                    TOP FRAME
───────────────────────────────────────────────────────────────────────────────────────────────────
 sleep             5      -           main.handleWork               time.Sleep
 chan receive      4      -           main.startWorkerPool          main.startWorkerPool.func1
 IO wait           1      -           net/http.(*connReader).sta…   internal/poll.runtime_pollWait
 running           1      -           net/http.(*Server).Serve      runtime/pprof.writeGoroutine…
 chan receive      1      -           main.handleLeak               main.handleLeak.func1
recent activity
18:05:07 -1 finished  created by net/http.(*connReader).startBackgroundRead
18:05:07 -1 finished  created by main.handleWork
18:05:07 +1 started  created by net/http.(*connReader).startBackgroundRead
18:05:06 -1 finished  created by net/http.(*connReader).startBackgroundRead
18:05:06 +1 started  created by net/http.(*connReader).startBackgroundRead
18:05:06 +6 started  created by main.handleWork
18:05:06 +1 started  created by main.handleLeak
18:05:05 -1 finished  created by net/http.(*connReader).startBackgroundRead
18:05:05 +1 started  created by net/http.(*connReader).startBackgroundRead
↑/↓ select · enter stack detail · / filter · r refresh now · q quit
```

Reading this: 5 `main.handleWork` goroutines are mid-`time.Sleep` (the
simulated work), 4 idle workers sit on a channel receive, and the "recent
activity" log below shows exactly what just happened - 6 `handleWork`
goroutines started together (the burst of `/work` requests), one already
finished, and one `handleLeak` goroutine started and will now stay in the
`chan receive` group forever, since it never exits - that's the leak,
visibly accumulating if you keep hitting `/leak`.

Press `enter` on a row to drill into the full stack trace of one of its
goroutines:

```
4 goroutine(s) — state: chan receive
created by main.startWorkerPool (/home/user/gshow/cmd/demo/main.go:73)
members: #8(-), #9(-), #10(-), #11(-)

stack (goroutine 8)
goroutine 8 [chan receive]:
main.startWorkerPool.func1()
	/home/user/gshow/cmd/demo/main.go:74 +0x45
created by main.startWorkerPool in goroutine 1
	/home/user/gshow/cmd/demo/main.go:73 +0x3a
```

### Usage

```sh
go run ./cmd/gshow -addr <host:port> -interval 1s
```

- `-addr` — the target's HTTP pprof-style address, e.g. `localhost:6060`, or
  a full `.../debug/pprof/goroutine` URL. Default `localhost:6060` if
  neither `-socket` nor `-file` is given.
- `-socket` — path to a `probe.ListenAndServeUnix` Unix domain socket.
- `-file` — path to a saved dump (`-` for stdin) instead of a live target.
- `-interval` — how often to poll. Default `1s`.
- `-timeout` — HTTP timeout per poll. Default `5s`.

Only one of `-addr`, `-socket`, `-file` may be given.

Keys: `↑`/`↓` select, `enter` view the full stack for a group, `esc` back,
`/` filter by state / creator / function, `r` refresh now, `q` quit.

## Attaching to a target

Pick whichever fits the process you want to observe — the goal is that
`gshow` can get into *any* Go program, whether or not it already runs an
HTTP server:

| Target already has... | Do this | Point `gshow` at it with |
| --- | --- | --- |
| `net/http/pprof` registered | nothing | `-addr host:port` |
| its own HTTP mux, no pprof | `mux.Handle(probe.Path, probe.Handler())` | `-addr host:port` |
| no HTTP server at all | `go probe.ListenAndServe(":6061")` | `-addr host:port` |
| no HTTP server, no open port wanted | `go probe.ListenAndServeUnix("/tmp/x.sock")` | `-socket /tmp/x.sock` |
| a saved dump, or one piped in | nothing | `-file dump.txt` / `-file -` |
| can't touch the source at all | build it with `gshow-build` instead of `go build` | `-addr` / `-socket` per above |

`probe` (this module's `probe` package) serves the same wire format as
`net/http/pprof`'s `/debug/pprof/goroutine?debug=2` **and**
`/debug/pprof/trace`, so both `gshow` and `gshow-trace` work against either
one unmodified — it's a drop-in for programs that don't want to pull in the
whole `net/http/pprof` package or share their default mux just to be
observable.

### Zero-edit builds: `gshow-build`

If you can't or don't want to add an import at all, `gshow-build` wraps
`go build`/`run`/`install`/`test` and injects the probe via `-overlay` at
build time — the target's source is never touched:

```sh
go get github.com/hiroyukim/gshow   # once, in the target module
gshow-build build ./cmd/yourapp
```

The listening address is read from `GSHOW_ADDR` at run time (default
`localhost:6061`).

Without the dependency added, `gshow-build` isn't a silent no-op — the
underlying `go build` fails with its own error naming exactly what to run:

```
$ gshow-build build ./cmd/yourapp
gshow_probe_inject.go:3:8: no required module provides package github.com/hiroyukim/gshow/probe/auto; to add it:
	go get github.com/hiroyukim/gshow/probe/auto
```

A `require` line alone is enough (nothing needs to statically import the
package), but that also means a bare `go mod tidy` will prune it right back
out, since nothing appears to use it. Pin it the standard way Go projects
pin build-time-only tool dependencies, so `tidy` leaves it alone:

```go
//go:build tools

package tools

import _ "github.com/hiroyukim/gshow/probe/auto"
```

(An earlier version of this tried to do the injection purely through `go
build -toolexec`, wrapping the compiler invocation after the fact. That
doesn't work: by the time a `-toolexec` wrapper runs, `go build` has
already fixed the package's dependency graph from the real source files, so
a brand new import injected at that point can't resolve — the compiler's
`-importcfg` simply won't list it. `-overlay` adds the new source file
*before* that graph gets computed, which is what actually makes this
possible - confirmed by hand against both approaches before settling on
this one.)

## `gshow-trace`: execution-trace timeline

`gshow`'s dashboard is built on periodic snapshots (a full stack dump every
second or so) - great for "what's piling up right now", but it can't show
you the exact moment a goroutine blocked, or how long it actually spent
running versus waiting between two snapshots. `gshow-trace` captures Go's
execution trace instead (the same mechanism behind `go tool trace`), which
records every scheduling event with a nanosecond timestamp, and renders it
as a per-goroutine timeline directly in the terminal.

```sh
go run ./cmd/gshow-trace -addr localhost:6060 -seconds 3
```

### Sample output

Captured against the demo server during a burst of `/work` and one `/leak`
request:

```
capturing a 3s execution trace from localhost:6060...
captured 3.001s across 19 goroutines (most active first, showing up to 12) - 20 runtime/GC housekeeping goroutines hidden, pass -all to show them

g97      - (select)                ·······························································································
g12      main.ticker (chan receiv…                                 ·······························································
g4       - (system goroutine wait) ·······························································································
g26      main.doWork                                               ···························································
g84      main.doWork                                               ···························································
g90      -                                                                                                                        
g102     -                                                                                                                        
g49      -                                                                                                                        
g96      main.handleLeak.func1                                     ·······························································
g93      main.doWork                                               ·······························································
g8       main.startWorkerPool.fun…                                 ·······························································
g9       main.startWorkerPool.fun…                                 ·······························································

█ running   ▒ runnable  ▓ syscall   · waiting     not alive
```

(In a real terminal each glyph is colored per the legend; this is the plain
text.) Each row is one goroutine, labeled by the function it was spawned
from; the bar spans the capture window, one character per time bucket.

### Reading the gap at the start of every bar

Notice every bar here starts with blank space before the dots begin, even
for goroutines that plainly already existed the whole time (the worker
pool, `g8`/`g9`). That's not a bug in this tool - it's how Go's execution
tracer works: a goroutine that isn't actively being scheduled doesn't emit
any event at all until the tracer's next periodic full-state sync (this
trace format batches state into "generations" roughly once a second), so
for the first ~1s of *any* capture, an idle goroutine that existed before
you started tracing is invisible - there's genuinely no data yet, not "not
running". A longer `-seconds` window makes this proportionally smaller but
never removes it. If you need the authoritative picture, save the raw trace
and open it with the real tool:

```sh
gshow-trace -addr localhost:6060 -seconds 3 -out trace.out
go tool trace trace.out
```

### Usage

```sh
go run ./cmd/gshow-trace -addr <host:port> -seconds 2
```

- `-addr` — the target's pprof address. Default `localhost:6060`.
- `-seconds` — how long to capture. Default `2`.
- `-out` — also save the raw trace to this path, for `go tool trace`.
- `-width` — output width in columns. Default `160`.
- `-rows` — max goroutines to show, most active (most state changes) first.
  Default `40`.
- `-all` — also show goroutines that look like GC/trace runtime machinery
  (hidden by default - see below).

Goroutines whose creating function is in `runtime`, `runtime/trace`, or
`runtime/pprof` are hidden by default: GC workers and the tracer's own
bookkeeping goroutines show up in every capture regardless of target and
just add noise. Pass `-all` to see them anyway.

There's no live/streaming mode for this one - a capture is inherently a
bounded window (`trace.Start` / `trace.Stop`), so it's a separate one-shot
command rather than a mode of the live dashboard.

## How it works

- `probe` / `probe/auto` make a target observable: handlers (or a couple of
  one-line servers) for both a goroutine dump and an execution trace, in
  the same wire formats `net/http/pprof` uses.
- `cmd/gshow-build` wraps `go build` and friends with a `-overlay` that
  blank-imports `probe/auto` into the target package, for when you'd rather
  not add even that one import line by hand.
- `internal/goroutine` parses goroutine dumps and groups goroutines by a
  signature of their state and call stack (ignoring argument values, which
  are raw addresses that differ even between goroutines running identical
  code).
- `internal/fetch` polls a target over HTTP (TCP or Unix socket) or reads a
  saved dump.
- `internal/tui` diffs each poll against the previous one to report which
  goroutines started and finished, grouped by where they were created.
- `internal/xtrace` parses an execution trace (via `golang.org/x/exp/trace`)
  into per-goroutine state timelines and renders them as text.
