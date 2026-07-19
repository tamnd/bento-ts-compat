package suite

import (
	"flag"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// casesRoot is where the vendored corpus lives relative to this package. The
// tree under it mirrors the upstream tests/cases layout, compiler/ and
// conformance/, so a case id is its path beneath this root.
const casesRoot = "../corpus/cases"

// filter, set by `go test -run-filter substring`, narrows a run to cases whose
// id contains the substring. It is the incremental seam of the accept tier:
// working on one lowering path reruns only the cases whose path names it, without
// the cost of driving all twelve thousand. Empty runs the whole corpus. The flag
// is deliberately not named -run, which the testing package already owns for
// subtest names, so the two filters compose.
var filter = flag.String("run-filter", "", "run only cases whose id contains this substring")

// jobs, set by `go test -jobs N`, caps how many cases the accept tier drives at
// once. Each case spins a front-end checker, which is memory heavy, and an
// unbounded fan-out over twelve thousand of them exhausts memory and reboots the
// machine, the failure the corpus README warns about. The default tracks the
// machine's parallelism but leaves headroom rather than pinning every core. A
// bounded pool, not t.Parallel over the whole corpus, is the deliberate shape.
var jobs = flag.Int("jobs", defaultJobs(), "number of cases to drive through the front end at once")

// defaultJobs picks a worker count that keeps the machine responsive: half its
// logical CPUs, at least one. The front end is the memory bottleneck, not the
// CPU, so leaving cores idle is the point, it caps how many checkers live at
// once.
func defaultJobs() int {
	n := runtime.NumCPU() / 2
	if n < 1 {
		n = 1
	}
	return n
}

// selectCases discovers the corpus and applies -run-filter. A discovery error or
// an empty selection is fatal, since a run that silently checks nothing passes
// for the wrong reason and hides a broken vendor or an over-narrow filter.
func selectCases(t *testing.T) []Case {
	t.Helper()
	all, err := Discover(casesRoot)
	if err != nil {
		t.Fatalf("discover cases: %v", err)
	}
	if len(all) == 0 {
		t.Fatalf("no cases found under %s, is the corpus vendored", casesRoot)
	}
	if *filter == "" {
		return all
	}
	var kept []Case
	for _, c := range all {
		if strings.Contains(c.ID, *filter) {
			kept = append(kept, c)
		}
	}
	if len(kept) == 0 {
		t.Fatalf("no cases match -run-filter %q", *filter)
	}
	return kept
}

// TestStructure proves the corpus is well formed before any tier reads a case's
// content. The vendored tree must be discoverable and non-empty, and every
// discovered case must be a readable file with a non-empty id, so a broken sparse
// checkout or a stray directory entry fails here rather than as a confusing
// mid-run panic later.
func TestStructure(t *testing.T) {
	cases := selectCases(t)
	seen := map[string]bool{}
	for _, c := range cases {
		if c.ID == "" {
			t.Errorf("case with empty id at %s", c.File)
		}
		if seen[c.ID] {
			t.Errorf("duplicate case id %s", c.ID)
		}
		seen[c.ID] = true
		if !fileExists(c.File) {
			t.Errorf("case %s does not resolve to a file: %s", c.ID, c.File)
		}
	}
}

// TestAccept drives every selected case through the emit step and asserts the one
// invariant the whole suite is built on: no case is ever wrong. A wrong outcome
// is a panic in the front end or, once C1 wires the compile check, Go that does
// not build. Passes and handbacks are both acceptable here and are only counted,
// not asserted on, because which cases lower today is a moving line the ledger
// tracks, while wrong is a hard zero that never moves.
//
// The fan-out is a bounded worker pool sized by -jobs, not t.Parallel over the
// corpus, because each case holds a front-end checker in memory and an unbounded
// twelve-thousand-wide fan-out exhausts the machine. Every wrong case is reported
// with its reason so a regression names itself.
func TestAccept(t *testing.T) {
	cases := selectCases(t)

	var passes, handbacks, wrongs atomic.Int64
	work := make(chan Case)
	var wg sync.WaitGroup
	for range *jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				switch r := Classify(c); r.Outcome {
				case Pass:
					passes.Add(1)
				case Handback:
					handbacks.Add(1)
				case Wrong:
					wrongs.Add(1)
					t.Errorf("WRONG %s: %s", c.ID, r.Reason)
				}
			}
		}()
	}
	for _, c := range cases {
		work <- c
	}
	close(work)
	wg.Wait()

	t.Logf("accept tier over %d cases: %d pass, %d handback, %d wrong",
		len(cases), passes.Load(), handbacks.Load(), wrongs.Load())
	if w := wrongs.Load(); w > 0 {
		t.Fatalf("%d cases produced wrong output, this count must be zero", w)
	}
}
