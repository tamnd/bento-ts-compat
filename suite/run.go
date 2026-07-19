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

// goRunTimeout bounds compiling and running one case's emitted Go. A lowering
// bug can turn a finite program into a spinning one, and the runtime tier must
// not hang on it. A timed-out run is reported with a non-zero exit, which the
// tier reads as a mismatch against an oracle that expects a clean exit.
const goRunTimeout = 60 * time.Second

// runGo compiles a case's emitted Go and runs it, capturing what it printed, how
// it exited, and the first line of any panic. It writes the Go into a scratch
// directory under the module tree, the same placement the build-check uses, so
// the program's import of bento's value package resolves from this module's
// requirements with no separate go.mod. It uses go run rather than a separate
// build and exec, so there is one scratch program and the toolchain manages the
// binary.
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
	ctx, cancel := context.WithTimeout(context.Background(), goRunTimeout)
	defer cancel()
	// -buildvcs=false for the same reason the build-check sets it: the scratch dir
	// sits inside this repo, and VCS stamping would shell out to git for every run
	// and contend on the repo's index lock.
	cmd := exec.CommandContext(ctx, "go", "run", "-buildvcs=false", ".")
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
