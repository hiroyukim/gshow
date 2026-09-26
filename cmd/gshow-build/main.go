// Command gshow-build makes any Go program observable with gshow at build
// time, without touching its source: it wraps `go build` (or run/install/
// test) and uses -overlay to slip a blank import of probe/auto (see the
// probe/auto package) into the target package, which starts a gshow probe
// as a side effect of the program starting.
//
//	gshow-build build ./cmd/yourapp
//	gshow-build run ./cmd/yourapp
//
// (A first version of this tried to do the injection purely through
// -toolexec, wrapping the compiler invocation go build had already decided
// to make. That doesn't work: by the time -toolexec's wrapper runs, go
// build has already fixed the package's dependency graph from its own
// source files, so a brand new import injected at that point can't
// resolve - the compiler's -importcfg simply won't list it. -overlay is
// the flag that actually lets you add a new source file - with a new
// import - before that graph gets computed, which is what this uses.)
//
// This still only works if the target module already depends on
// github.com/hiroyukim/gshow (nothing needs to import it - a require line
// in go.mod is enough, since the injected file itself provides the
// import). Run once, in the target module:
//
//	go get github.com/hiroyukim/gshow
//
// Without that, the underlying `go build` fails with its own "no required
// module provides package ..." error naming exactly what to run.
//
// The resulting binary listens wherever the GSHOW_ADDR environment
// variable says at run time (default localhost:6061); see the probe/auto
// package.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const probeAutoImport = "github.com/hiroyukim/gshow/probe/auto"

var verbs = map[string]bool{"build": true, "run": true, "install": true, "test": true}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 || !verbs[args[0]] {
		fmt.Fprintln(os.Stderr, "usage: gshow-build {build|run|install|test} [go flags] [package]")
		return 2
	}
	verb, rest := args[0], args[1:]

	for _, a := range rest {
		if a == "-overlay" || strings.HasPrefix(a, "-overlay=") {
			fmt.Fprintln(os.Stderr, "gshow-build: can't combine with your own -overlay flag; pass it to `go` directly instead")
			return 2
		}
	}

	pkgDir, pkgName, err := resolvePackage(pkgPattern(rest))
	if err != nil {
		fmt.Fprintln(os.Stderr, "gshow-build:", err)
		return 2
	}

	overlayPath, cleanup, err := writeOverlay(pkgDir, pkgName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gshow-build:", err)
		return 2
	}
	defer cleanup()

	goArgs := append([]string{verb, "-overlay", overlayPath}, rest...)
	cmd := exec.Command("go", goArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "gshow-build:", err)
		return 1
	}
	return 0
}

// pkgPattern picks the package pattern out of a go build/run/install/test
// argument list: the last argument that isn't itself a flag or a flag's
// value. Only a single target package is supported.
func pkgPattern(args []string) string {
	for i, a := range slices.Backward(args) {
		if strings.HasPrefix(a, "-") {
			break
		}
		// A bare value right after a flag (e.g. "-o out") belongs to that
		// flag, not us, unless the flag is self-contained ("-o=out").
		if i > 0 && strings.HasPrefix(args[i-1], "-") && !strings.Contains(args[i-1], "=") {
			continue
		}
		return a
	}
	return "."
}

func resolvePackage(pattern string) (dir, name string, err error) {
	dir, err = goList(pattern, "{{.Dir}}")
	if err != nil {
		return "", "", fmt.Errorf("resolving package %q: %w", pattern, err)
	}
	name, err = goList(pattern, "{{.Name}}")
	if err != nil {
		return "", "", fmt.Errorf("resolving package %q: %w", pattern, err)
	}
	if name != "main" {
		fmt.Fprintf(os.Stderr, "gshow-build: warning: package %q is %q, not \"main\" - it won't produce a runnable probe unless it's built into a main package\n", pattern, name)
	}
	return dir, name, nil
}

func goList(pattern, format string) (string, error) {
	out, err := exec.Command("go", "list", "-f", format, pattern).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// writeOverlay writes a temp source file that blank-imports probe/auto
// into the target package, plus a go build -overlay JSON file mapping it
// into pkgDir under a synthetic name. Both are removed by the returned
// cleanup func.
func writeOverlay(pkgDir, pkgName string) (overlayPath string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "gshow-build-")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { os.RemoveAll(tmpDir) }

	srcPath := filepath.Join(tmpDir, "inject.go")
	src := "package " + pkgName + "\n\nimport _ \"" + probeAutoImport + "\"\n"
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		cleanup()
		return "", nil, err
	}

	virtualPath := filepath.Join(pkgDir, "gshow_probe_inject.go")
	overlay := fmt.Sprintf(`{"Replace": {%q: %q}}`, virtualPath, srcPath)
	overlayPath = filepath.Join(tmpDir, "overlay.json")
	if err := os.WriteFile(overlayPath, []byte(overlay), 0o644); err != nil {
		cleanup()
		return "", nil, err
	}
	return overlayPath, cleanup, nil
}
