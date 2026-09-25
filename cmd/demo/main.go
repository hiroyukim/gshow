// Command demo is a throwaway HTTP server used to try gshow against
// something real. It exposes net/http/pprof and spawns goroutines in a
// few different, easy-to-recognize patterns:
//
//	GET /work    spawns one short-lived goroutine per request (the
//	             classic "fire and forget" pattern) and returns immediately.
//	GET /leak    spawns a goroutine that never exits, so you can watch
//	             its group's count climb every time you hit the endpoint.
//
// A small idle worker pool and a ticker run in the background the whole
// time so there's always something to look at even with no traffic.
//
// Run it, then in another terminal: go run ./cmd/gshow
package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"time"
)

const addr = "localhost:6060"

func main() {
	startWorkerPool(4)
	go ticker()

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/work", handleWork)
	http.HandleFunc("/leak", handleLeak)

	log.Printf("demo server listening on http://%s", addr)
	log.Printf("goroutine profile:    http://%s/debug/pprof/goroutine?debug=2", addr)
	log.Printf("try:  go run ./cmd/gshow -addr %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "gshow demo server. try GET /work or GET /leak, then watch gshow.")
}

// handleWork spawns a per-request goroutine that does a bit of fake work
// and exits, then responds right away without waiting for it - the pattern
// gshow's "recent activity" panel is built to surface.
func handleWork(w http.ResponseWriter, r *http.Request) {
	go doWork()
	fmt.Fprintln(w, "work started in the background")
}

func doWork() {
	d := time.Duration(1+rand.Intn(4)) * time.Second
	time.Sleep(d)
}

// handleLeak spawns a goroutine that blocks forever - an intentional leak
// so you can see a group's count climb with every hit.
func handleLeak(w http.ResponseWriter, r *http.Request) {
	go func() {
		block := make(chan struct{})
		<-block // never sent to: this goroutine never returns
	}()
	fmt.Fprintln(w, "leaked one goroutine on purpose")
}

// startWorkerPool starts n goroutines that sit idle on a channel receive,
// so gshow always has a steady "chan receive" group to show.
func startWorkerPool(n int) {
	jobs := make(chan func())
	for range n {
		go func() {
			for job := range jobs {
				job()
			}
		}()
	}
}

// ticker just sits in a select on a timer, forever, to show what a "select"
// state with a wait duration looks like.
func ticker() {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for range t.C {
	}
}
