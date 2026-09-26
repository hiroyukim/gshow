# gshow

A live terminal dashboard for watching what the goroutines in a running Go
process are actually doing — right now, as it happens (goroutines spawned
per HTTP request, workers blocked on a channel, leaks that never exit, etc).

It polls a target for a goroutine dump on an interval, groups goroutines
that are doing the identical thing (same state, same call stack), and shows
what changed between polls in a "recent activity" log.

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
`net/http/pprof`'s `/debug/pprof/goroutine?debug=2`, so `gshow`'s HTTP
source works against either one unmodified — it's a drop-in for programs
that don't want to pull in the whole `net/http/pprof` package or share
their default mux just to be observable.

### Zero-edit builds: `gshow-build`

If you can't or don't want to add an import at all, `gshow-build` wraps
`go build`/`run`/`install`/`test` and injects the probe via `-overlay` at
build time — the target's source is never touched:

```sh
go get github.com/hiroyukim/gshow   # once, in the target module
gshow-build build ./cmd/yourapp
```

The listening address is read from `GSHOW_ADDR` at run time (default
`localhost:6061`). Without the dependency added, the underlying `go build`
just fails with its own "no required module provides package ..." error
telling you what to run — it's not a silent no-op.

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
possible.)

## Try it

A demo server is included if you don't have a target handy:

```sh
go run ./cmd/demo
```

In another terminal:

```sh
go run ./cmd/gshow
```

Then hit the demo server to watch goroutines come and go:

```sh
curl http://localhost:6060/work   # spawns one short-lived goroutine per request
curl http://localhost:6060/leak   # spawns a goroutine that never exits
```

## Usage

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

## How it works

- `probe` / `probe/auto` make a target observable: a handler, or a couple
  of one-line servers, that write a goroutine dump in the same format
  `net/http/pprof` uses.
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
