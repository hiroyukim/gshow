# gshow

A live terminal dashboard for watching what the goroutines in a running Go
process are actually doing — right now, as it happens (goroutines spawned
per HTTP request, workers blocked on a channel, leaks that never exit, etc).

It polls the target's `net/http/pprof` goroutine endpoint on an interval,
groups goroutines that are doing the identical thing (same state, same call
stack), and shows what changed between polls in a "recent activity" log.

## Try it

The target process just needs `net/http/pprof` registered, which most Go
servers already have:

```go
import _ "net/http/pprof"
```

A demo server is included if you don't have one handy:

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

- `-addr` — the target's pprof address, e.g. `localhost:6060`, or a full
  `.../debug/pprof/goroutine` URL. Defaults to `localhost:6060`.
- `-interval` — how often to poll. Defaults to `1s`.
- `-timeout` — HTTP timeout per poll. Defaults to `5s`.

Keys: `↑`/`↓` select, `enter` view the full stack for a group, `esc` back,
`/` filter by state / creator / function, `r` refresh now, `q` quit.

## How it works

- `internal/goroutine` parses `/debug/pprof/goroutine?debug=2` dumps and
  groups goroutines by a signature of their state and call stack (ignoring
  argument values, which are raw addresses that differ even between
  goroutines running identical code).
- `internal/fetch` polls a target over HTTP.
- `internal/tui` diffs each poll against the previous one to report which
  goroutines started and finished, grouped by where they were created.
