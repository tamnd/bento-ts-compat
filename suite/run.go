package suite

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// goRunTimeout bounds running one case's compiled binary. A lowering bug can
// turn a finite program into a spinning one, and the runtime tier must not hang
// on it. A timed-out run is reported with a non-zero exit, which the tier reads
// as a mismatch against an oracle that expects a clean exit. It bounds execution
// only: the compile that produces the binary is timed separately and generously,
// because a pathological case such as one giant binary expression is slow for the
// Go compiler in a way that says nothing about the program and, under a loaded
// build host, can dwarf the run itself.
const goRunTimeout = 60 * time.Second

// goBuildTimeout bounds compiling one case's emitted Go into a binary. It is far
// looser than the run budget because compile time is not a verdict on the case:
// a several-thousand-line stress file with a single deeply nested expression can
// take the Go compiler tens of seconds, and a build host under heavy external
// load stretches that further. The bound exists only so a truly stuck toolchain
// cannot wedge the tier, not to judge a slow compile as wrong.
const goBuildTimeout = 5 * time.Minute

// runGo compiles a case's emitted Go and runs it, capturing what it printed, how
// it exited, and the first line of any panic. It writes the Go into a scratch
// directory under the module tree, the same placement the build-check uses, so
// the program's import of bento's value package resolves from this module's
// requirements with no separate go.mod.
//
// It compiles to a binary and then runs the binary, rather than using go run,
// so the two costs are bounded separately: the compile gets a generous budget
// because its duration reflects the emitted code's size, not its correctness,
// while the run gets the tight budget that catches a spinning program. Folding
// both into one go run budget made a stress case whose compile alone ran long
// under a loaded host report a false non-zero exit against an oracle it matches
// the moment it is given room to build.
//
// The result is shaped as an Oracle so it compares directly against the frozen
// one: stdout as printed, the process exit code, and on a panic the panic's
// first line as the exception. A failure to launch the toolchain, as opposed to
// the program exiting non-zero, is an environment problem and returned as an
// error, not a verdict on the case.
func runGo(moduleRoot, goSrc string) (Oracle, error) {
	dir, err := os.MkdirTemp(moduleRoot, "runtime-")
	if err != nil {
		return Oracle{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goSrc), 0o644); err != nil {
		return Oracle{}, err
	}

	// Compile first, under the generous build budget. -buildvcs=false for the same
	// reason the build-check sets it: the scratch dir sits inside this repo, and VCS
	// stamping would shell out to git for every build and contend on the index lock.
	bin := filepath.Join(dir, "prog")
	buildCtx, buildCancel := context.WithTimeout(context.Background(), goBuildTimeout)
	defer buildCancel()
	build := exec.CommandContext(buildCtx, "go", "build", "-buildvcs=false", "-o", bin, ".")
	build.Dir = dir
	var buildOut bytes.Buffer
	build.Stdout = &buildOut
	build.Stderr = &buildOut
	if err := build.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			// The toolchain did not run at all, an environment problem, not a verdict.
			return Oracle{}, err
		}
		// A non-zero build exit is the emitted Go failing to compile. The accept tier
		// owns that verdict; here it surfaces as a non-zero exit with the compiler's
		// first line, so a runtime run of a case the accept tier would have caught does
		// not read as a clean pass.
		return Oracle{Exit: exitErr.ExitCode(), Exception: firstPanicLine(buildOut.String())}, nil
	}

	// Run the compiled binary under the tight run budget.
	runCtx, runCancel := context.WithTimeout(context.Background(), goRunTimeout)
	defer runCancel()
	cmd := exec.CommandContext(runCtx, bin)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	o := Oracle{Stdout: stdout.String()}
	if err == nil {
		return o, nil
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return Oracle{}, err
	}
	o.Exit = exitErr.ExitCode()
	o.Exception = firstPanicLine(stderr.String())
	return o, nil
}

// firstPanicLine returns the panic message from a crashed run's stderr, the
// first line after the `panic:` marker, so a runtime mismatch reports why the Go
// crashed rather than the whole stack. A non-zero exit with no panic marker,
// such as a compile failure go run surfaces, returns the first stderr line.
func firstPanicLine(stderr string) string {
	lines := strings.Split(stderr, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "panic:") {
			return strings.TrimSpace(line)
		}
	}
	for _, line := range lines {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return "non-zero exit"
}
