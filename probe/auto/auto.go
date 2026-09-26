// Package auto starts a gshow probe as a side effect of being imported,
// the same way net/http/pprof works. It exists so gshow-toolexec (see
// cmd/gshow-toolexec) has something to blank-import into a target binary
// without needing to touch that binary's source: a build with
// -toolexec=gshow-toolexec injects "import _ ..." for this package into
// the main package being compiled.
//
// You can also import it by hand instead of using -toolexec, which comes
// to the same thing:
//
//	import _ "github.com/hiroyukim/gshow/probe/auto"
//
// The listen address comes from the GSHOW_ADDR environment variable,
// defaulting to "localhost:6061". A failure to listen (e.g. the port is
// already in use) is logged, not fatal - it must never take down the host
// program.
package auto

import (
	"log"
	"os"

	"github.com/hiroyukim/gshow/probe"
)

func init() {
	addr := os.Getenv("GSHOW_ADDR")
	if addr == "" {
		addr = "localhost:6061"
	}
	go func() {
		if err := probe.ListenAndServe(addr); err != nil {
			log.Printf("gshow probe: %v (goroutine observation disabled)", err)
		}
	}()
}
