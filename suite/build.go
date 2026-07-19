package suite

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// buildCheck compiles a passing case's emitted Go to confirm it is well formed.
// An accepted emit that does not build is the lowerer producing garbage, so a
// failed build is wrong, not a pass. It writes the Go into a scratch directory
// under the module tree and runs go build, not go run, because T0 asks only
// whether the code compiles, not what it prints. The scratch directory sits
// inside the module so the emitted program's import of bento's value package
// resolves from this module's requirements with no separate go.mod, the same way
// bento's own build compiles a program inside its module tree.
//
// It returns an empty string when the Go builds and the compiler's output when it
// does not, so the caller can record why an emit was rejected. A failure to set
// up or launch the build, as opposed to a compile error, is returned as an error:
// that is an environment problem, not a verdict on the case.
func buildCheck(moduleRoot, goSrc string) (buildErr string, setupErr error) {
	dir, err := os.MkdirTemp(moduleRoot, "buildcheck-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goSrc), 0o644); err != nil {
		return "", err
	}
	// Discard the linked object: T0 only needs to know the package compiles, and
	// writing every accepted case's binary to disk would be gigabytes of dead
	// output. The build still populates the shared package cache, which TestMain
	// bounds, so repeated runs stay fast. -buildvcs=false because the scratch dir
	// sits inside this git repo, and the default VCS stamping would shell out to
	// git for every one of a thousand builds, contending for the repo's index lock
	// and adding nothing T0 cares about.
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", os.DevNull, ".")
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// A non-zero exit is a compile failure, the verdict we came for.
			return strings.TrimSpace(out.String()), nil
		}
		// The toolchain did not run at all, an environment problem, not a verdict.
		return "", err
	}
	return "", nil
}

// classifyBuilt is the full T0 judgement: classify the emit, and for a pass,
// compile the emitted Go and demote it to wrong if it does not build. It is the
// function the accept tier and the ledger both run, so a case is judged the same
// way wherever it is measured. A setup error from the build is not a verdict on
// the case, so it is surfaced as a wrong with an explanatory reason rather than
// silently passing the case, since a run that cannot build its output has not
// proven the output good.
func classifyBuilt(moduleRoot string, c Case) Result {
	r := Classify(c)
	if r.Outcome != Pass {
		return r
	}
	buildErr, setupErr := buildCheck(moduleRoot, r.Go)
	if setupErr != nil {
		return Result{Outcome: Wrong, Go: r.Go, Reason: "build-check could not run: " + setupErr.Error()}
	}
	if buildErr != "" {
		return Result{Outcome: Wrong, Go: r.Go, Reason: "emitted Go does not compile:\n" + buildErr}
	}
	return r
}
