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

// nodeRun is one execution of a baseline on Node: what it printed, how it
// exited, and the first line of any uncaught error. threw is true when the run
// did not exit cleanly, which the generator treats as a case with no fair oracle
// rather than as an exception to freeze.
type nodeRun struct {
	stdout string
	exit   int
	threw  bool
	errLn  string
}

// nodeTimeout bounds a single baseline run. A handful of corpus cases loop
// forever when run as a program, and without a bound the generator would hang on
// them. A timed-out run is reported as threw, which routes the case to a skip.
const nodeTimeout = 8 * time.Second

// runNode writes the assembled baseline into dir under the extension its module
// form needs and runs it on Node once, capturing stdout, exit status and the
// first error line. env is appended to a cleaned copy of the process
// environment, which is how the environment screen perturbs TZ and locale
// without disturbing PATH and the rest. A launch or setup failure, as opposed to
// the program exiting non-zero, is returned as an error: that is the machine's
// problem, not the case's.
func runNode(nodeBin, dir, js string, module jsModule, env []string) (nodeRun, error) {
	prog := filepath.Join(dir, "prog"+module.ext())
	if err := os.WriteFile(prog, []byte(js), 0o644); err != nil {
		return nodeRun{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), nodeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, nodeBin, prog)
	cmd.Dir = dir
	cmd.Env = append(cleanEnv(env), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	run := nodeRun{stdout: stdout.String()}
	if err == nil {
		return run, nil
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return nodeRun{}, err
	}
	run.threw = true
	run.exit = exitErr.ExitCode()
	run.errLn = firstErrorLine(stderr.String())
	return run, nil
}

// cleanEnv returns the process environment with any keys overridden in perturb
// removed, so the perturbing values appended after it are the ones Node sees. It
// keeps everything else, so Node still finds its own runtime.
func cleanEnv(perturb []string) []string {
	if len(perturb) == 0 {
		return os.Environ()
	}
	drop := map[string]bool{}
	for _, kv := range perturb {
		if k, _, ok := strings.Cut(kv, "="); ok {
			drop[k] = true
		}
	}
	var kept []string
	for _, kv := range os.Environ() {
		if k, _, ok := strings.Cut(kv, "="); ok && drop[k] {
			continue
		}
		kept = append(kept, kv)
	}
	return kept
}

// firstErrorLine returns the first stderr line that names an error, the reason a
// skipped case carries in the runtime ledger. Node prints the stack after the
// message, so the first Error line is the useful one.
func firstErrorLine(stderr string) string {
	for line := range strings.SplitSeq(stderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Error") {
			return line
		}
	}
	return "non-zero exit"
}

// envPerturb is the environment the environment screen runs a baseline under: a
// distinctive timezone and a fixed locale. A case whose output moves between the
// base run and this one depends on the machine it runs on, so its output is not
// a stable oracle and the case is skipped.
var envPerturb = []string{"TZ=Asia/Kolkata", "LANG=C", "LC_ALL=C"}

// generateOracle runs a case's baseline on Node under the two screens the oracle
// data has to pass and returns either the frozen oracle or the reason the case
// was skipped. A skip is not a failure: it means the baseline is not a fair
// ground truth for the compiled Go, so the runtime tier leaves the case to the
// tiers below.
//
// The order is a funnel. Three base runs must agree, or the program is
// non-deterministic and no single output is the truth. A run that throws is
// skipped rather than frozen as an exception: on this corpus a throw is almost
// always a reference to an ambient declaration Node does not have, which the
// compiled Go, treating the declaration as a real binding, will not reproduce,
// so the throw is an artifact of running the bare .js and not the case's meaning.
// A run whose output changes under a perturbed timezone and locale is
// environment-sensitive and skipped. What survives is a deterministic,
// self-contained, clean exit, frozen as an oracle of its stdout and exit 0.
func generateOracle(nodeBin, dir, js string, module jsModule) (o Oracle, skip string, err error) {
	base, err := runNode(nodeBin, dir, js, module, nil)
	if err != nil {
		return Oracle{}, "", err
	}
	for range 2 {
		again, err := runNode(nodeBin, dir, js, module, nil)
		if err != nil {
			return Oracle{}, "", err
		}
		if again.stdout != base.stdout || again.exit != base.exit || again.threw != base.threw {
			return Oracle{}, "non-deterministic across three runs", nil
		}
	}
	if base.threw {
		return Oracle{}, "baseline throws on node (" + base.errLn + ")", nil
	}
	perturbed, err := runNode(nodeBin, dir, js, module, envPerturb)
	if err != nil {
		return Oracle{}, "", err
	}
	if perturbed.stdout != base.stdout {
		return Oracle{}, "output depends on timezone or locale", nil
	}
	return Oracle{Exit: 0, Stdout: base.stdout}, "", nil
}
