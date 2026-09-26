// Package probe is a minimal, dependency-free way to make any Go program
// observable with gshow, whether or not it already runs an HTTP server.
//
// It serves the exact same wire format as net/http/pprof's
// /debug/pprof/goroutine?debug=2 endpoint (gshow's HTTP source already
// speaks that format), so you don't need to pull in net/http/pprof or hand
// gshow access to your whole default mux just to look at goroutines. Pick
// whichever of the three fits the target program:
//
// Program already has an HTTP server and mux:
//
//	mux.Handle(probe.Path, probe.Handler())
//
// Program has no HTTP server at all (a worker, CLI daemon, batch job, ...):
//
//	go probe.ListenAndServe(":6061")
//
// Program shouldn't open a network port (containers, shared hosts, ...):
//
//	go probe.ListenAndServeUnix("/tmp/myapp.gshow.sock")
//	// then: gshow -socket /tmp/myapp.gshow.sock
package probe

import (
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"

	runtimetrace "runtime/trace"
)

// Path is the HTTP path the goroutine dump is served on, matching
// net/http/pprof's convention so gshow's HTTP source works against either
// one unmodified.
const Path = "/debug/pprof/goroutine"

// TracePath is the HTTP path the execution trace is served on, matching
// net/http/pprof's convention (including its "seconds" query parameter) so
// gshow's trace capture works against either one unmodified.
const TracePath = "/debug/pprof/trace"

// Handler returns an http.Handler that writes a full goroutine dump, in the
// same text format as net/http/pprof's debug=2 goroutine profile. Mount it
// on your own mux at Path.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writeDump(w)
	})
}

// WriteDump writes a full goroutine dump to w in the same text format as
// net/http/pprof's debug=2 goroutine profile. Use this directly if you want
// to trigger a dump some other way, e.g. on a signal, instead of over HTTP.
func WriteDump(w io.Writer) error {
	return writeDump(w)
}

func writeDump(w io.Writer) error {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			_, err := w.Write(buf[:n])
			return err
		}
		buf = make([]byte, 2*len(buf))
	}
}

// TraceHandler returns an http.Handler that captures a runtime/trace
// execution trace and writes it in its native binary format - the same
// format net/http/pprof's /debug/pprof/trace produces, and that
// `go tool trace` and gshow's own trace capture both expect. Tracing lasts
// for the duration given by the "seconds" query parameter (default 1s, as
// with net/http/pprof). Mount it on your own mux at TracePath.
func TraceHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sec, err := strconv.ParseFloat(r.FormValue("seconds"), 64)
		if err != nil || sec <= 0 {
			sec = 1
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		if err := runtimetrace.Start(w); err != nil {
			http.Error(w, "could not start trace: "+err.Error(), http.StatusInternalServerError)
			return
		}
		time.Sleep(time.Duration(sec * float64(time.Second)))
		runtimetrace.Stop()
	})
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle(Path, Handler())
	mux.Handle(TracePath, TraceHandler())
	return mux
}

// ListenAndServe starts a minimal HTTP server on addr that serves nothing
// but the goroutine dump and execution trace endpoints, for programs that
// don't already run an HTTP server. It blocks, so run it in its own
// goroutine.
func ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, newMux())
}

// ListenAndServeUnix does the same as ListenAndServe, but over a Unix
// domain socket at path instead of a TCP port. Useful when you'd rather not
// expose a network port at all, e.g. attaching a sidecar in a container. A
// stale socket file left over from a previous run at the same path is
// removed first. It blocks, so run it in its own goroutine.
func ListenAndServeUnix(path string) error {
	_ = os.Remove(path) // best-effort: clear a stale socket from a previous run
	l, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	return http.Serve(l, newMux())
}
