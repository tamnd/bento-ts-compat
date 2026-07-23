package suite

import (
	"flag"
	"os"
	"os/exec"
	"path"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// casesRoot is where the vendored corpus lives relative to this package. The
// tree under it mirrors the upstream tests/cases layout, compiler/ and
// conformance/, so a case id is its path beneath this root.
const casesRoot = "../corpus/cases"

// ledgerPath is the committed classification baseline, relative to this package.
const ledgerPath = "../status/ledger.txt"

// filter, set by `go test -filter conformance/types/enum`, narrows a run to
// cases whose id matches the pattern, by substring or by glob. It is the
// incremental seam: working on one lowering path reruns only the cases whose
// path names it, without the cost of driving all twelve thousand. Empty runs the
// whole corpus. The name is not -run, which the testing package already owns for
// subtest names, so the two compose.
var filter = flag.String("filter", "", "run only cases whose id matches this substring or glob")

// jobs, set by `go test -jobs N`, caps how many cases the suite drives at once.
// Each case spins a front-end checker and, on a pass, a go build, both heavy, and
// an unbounded fan-out over twelve thousand of them exhausts the machine, the
// failure the corpus README warns about. The default tracks the machine's
// parallelism but leaves headroom rather than pinning every core. A bounded pool,
// not t.Parallel over the whole corpus, is the deliberate shape.
var jobs = flag.Int("jobs", defaultJobs(), "number of cases to classify at once")

// updateLedger, set by `go test -run TestLedger -update-ledger`, rewrites
// status/ledger.txt from the current classification instead of checking against
// the committed one. It is the one supported way to move the baseline, so a
// coverage gain or a new handback lands as a reviewed diff, never a silent drift.
var updateLedger = flag.Bool("update-ledger", false, "rewrite status/ledger.txt from the current classification")

// updateGoldens, set by `go test -run TestEmitGolden -update-goldens`, rewrites the
// emit goldens from the current lowering instead of checking against the committed
// ones, and on a full run prunes a golden whose case no longer accepts. It is the
// one supported way to move a golden, so an emit change lands as a reviewed diff.
var updateGoldens = flag.Bool("update-goldens", false, "rewrite goldens/ from the current emit")

// updateOracles, set by `go test -run TestGenOracle -update-oracles`, drives the
// accepted cases' vendored .js baselines through Node under the non-determinism
// and environment screens and freezes the survivors as oracles/<id>.txt. It is
// the one path that needs Node, and the only supported way to move the oracle
// set, so a coverage change in the oracles lands as a reviewed diff. Without it
// the generator does nothing, because the runtime tier reads the frozen oracles
// and never needs Node itself.
var updateOracles = flag.Bool("update-oracles", false, "regenerate oracles/ from the .js baselines on Node")

// pruneOracles, set by `go test -run TestPruneOracles -prune-oracles`, removes the
// oracles whose case no longer passes the accept tier, the orphans a coverage
// change leaves behind. It is the Node-free half of -update-oracles: regenerating
// survivors needs Node to run the baselines, but dropping an orphan is only a
// deletion, so a coverage shrink can refreeze the oracle set without Node while
// leaving every surviving oracle's frozen output byte-for-byte untouched.
var pruneOracles = flag.Bool("prune-oracles", false, "remove oracles/ entries whose case no longer passes the accept tier, no Node needed")

// updateRuntime, set by `go test -run TestRuntime -update-runtime`, rewrites the
// runtime-wrong ratchet status/runtime.txt from the current runtime comparison
// instead of checking against the committed one. It is the one supported way to
// move that baseline, so a fixed miscompile leaves the ratchet as a reviewed diff.
var updateRuntime = flag.Bool("update-runtime", false, "rewrite status/runtime.txt from the current runtime comparison")

// updateDiagnostics, set by `go test -run TestDiagnostics -update-diagnostics`,
// rewrites the diagnostics-wrong ratchet status/diagnostics.txt from the current
// classification of the error cases instead of checking against the committed
// one. It is the one supported way to move that baseline, so a case bento learns
// to refuse leaves the ratchet as a reviewed diff.
var updateDiagnostics = flag.Bool("update-diagnostics", false, "rewrite status/diagnostics.txt from the current error-case classification")

// shard, set by `go test -shard i/N`, runs only the i-th of N even slices of the
// selected cases, counting from zero. It is how the runtime tier's heavy go-run
// pass is split across parallel CI jobs, each job taking one shard, so no single
// job compiles and runs the whole set. Empty runs everything. The slicing is by
// position in the sorted case list, so the shards are disjoint and cover the set.
var shard = flag.String("shard", "", "run only shard i of N, as i/N, counting from zero")

// defaultJobs picks a worker count that keeps the machine responsive: half its
// logical CPUs, at least one. The per-case work is memory heavy, so leaving cores
// idle is the point, it caps how many checkers and builds live at once.
func defaultJobs() int {
	return max(runtime.NumCPU()/2, 1)
}

// matchFilter reports whether a case id is selected by the -filter pattern. An
// empty pattern selects everything. A non-empty pattern matches as a substring
// or as a path glob, so both `enum` and `conformance/types/*` select the enum
// family, whichever a developer reaches for.
func matchFilter(id, pattern string) bool {
	if pattern == "" {
		return true
	}
	if strings.Contains(id, pattern) {
		return true
	}
	ok, err := path.Match(pattern, id)
	return err == nil && ok
}

// moduleRoot returns the directory the build-check writes its scratch programs
// into. It is this package's directory, which sits inside the module tree, so an
// emitted program's import of bento's value package resolves from the module's
// requirements with no separate go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}

// selectCases discovers the corpus and applies -filter. A discovery error or an
// empty selection is fatal, since a run that silently checks nothing passes for
// the wrong reason and hides a broken vendor or an over-narrow filter.
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
		if matchFilter(c.ID, *filter) {
			kept = append(kept, c)
		}
	}
	if len(kept) == 0 {
		t.Fatalf("no cases match -filter %q", *filter)
	}
	return kept
}

// classifyConcurrent runs the full T0 judgement over cases through a bounded
// worker pool and returns the results keyed by case id. The bound is the point:
// each worker holds a checker and a build in memory, so the pool width, not the
// case count, sets the peak footprint.
func classifyConcurrent(root string, cases []Case, jobs int) map[string]Result {
	results := make(map[string]Result, len(cases))
	cache := newClassifyCache(root)
	var mu sync.Mutex
	work := make(chan Case)
	var wg sync.WaitGroup
	for range jobs {
		wg.Go(func() {
			for c := range work {
				r := classifyBuiltCached(cache, root, c)
				mu.Lock()
				results[c.ID] = r
				mu.Unlock()
			}
		})
	}
	for _, c := range cases {
		work <- c
	}
	close(work)
	wg.Wait()
	return results
}

// The whole-corpus classification is expensive, a checker and a build per case,
// and both TestAccept on a full run and TestLedger need it. Compute it once per
// test binary and share it, so an unfiltered `go test ./suite` pays for the
// corpus a single time.
var (
	corpusOnce    sync.Once
	corpusResults map[string]Result
)

// requireFleetForFullRun skips a full-corpus run on a local laptop, so the heavy
// pass only runs where it belongs. A full run drives a checker and, on a pass, a
// go build over twelve thousand cases, minutes of memory-heavy work; the local
// iteration loop is a -filter or -shard subset instead. The Linux test fleet
// (server3) and CI run Linux, and the developer laptop is macOS, so a full run on
// darwin skips with a pointer to the fleet, while a Linux run always proceeds. CI
// therefore needs no extra configuration and the ledger, golden, and diagnostics
// gates stay live there. Setting BENTO_TS_COMPAT_LARGE forces the full run
// locally for the rare case a developer wants it.
func requireFleetForFullRun(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" || os.Getenv("BENTO_TS_COMPAT_LARGE") != "" {
		return
	}
	t.Skip("a full-corpus run is heavy and runs on the Linux test fleet (server3) or CI, not a local laptop; " +
		"run a -filter or -shard subset here, or set BENTO_TS_COMPAT_LARGE=1 to force the full run")
}

// fullCorpus classifies every case once and caches the result for the run. It
// ignores -filter on purpose: the ledger is only coherent over the whole corpus,
// and a full TestAccept wants the same shared pass.
func fullCorpus(t *testing.T) map[string]Result {
	t.Helper()
	requireFleetForFullRun(t)
	root := moduleRoot(t)
	corpusOnce.Do(func() {
		all, err := Discover(casesRoot)
		if err != nil {
			t.Fatalf("discover cases: %v", err)
		}
		corpusResults = classifyConcurrent(root, all, *jobs)
	})
	return corpusResults
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

// wrongBaseline reads the committed ledger and returns the set of case ids already
// recorded as wrong. This is the known-wrong debt: cases where bento today emits Go
// that does not compile, each one a bento lowering bug still to be fixed. The suite
// stays green against this baseline so the infrastructure can land and every new
// case is measured, but the set is a ratchet, it may only shrink. A wrong case not
// in the set is a fresh regression and fails the run; a case in the set that now
// builds is a fix, and the ledger diff records it when a developer refreezes. The
// end state is an empty set, reached by fixing bento, never by handing back.
func wrongBaseline(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("read ledger for wrong baseline: %v", err)
	}
	known := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == Wrong.String() {
			known[fields[1]] = true
		}
	}
	return known
}

// TestAccept drives every selected case through the full T0 judgement, emit then
// build-check, and guards the invariant the whole suite is built on: bento never
// emits Go that is wrong in a way the committed ledger did not already record. A
// wrong outcome is a panic in the front end, or an accepted emit that does not
// compile. Passes and handbacks are both acceptable and only counted, because
// which cases lower today is a moving line the ledger tracks. Wrong is the debt
// line: it is allowed only where the ledger already carries it, and it may only
// shrink, so a fresh miscompile fails here and names itself while the known debt
// is reported and burned down by fixing bento.
//
// An unfiltered run shares the cached whole-corpus classification with the ledger
// so the corpus is classified once. A filtered run, the iteration loop, classifies
// just its subset. Every unexpected wrong case is reported with its reason.
func TestAccept(t *testing.T) {
	var results map[string]Result
	if *filter == "" {
		results = fullCorpus(t)
	} else {
		results = classifyConcurrent(moduleRoot(t), selectCases(t), *jobs)
	}
	known := wrongBaseline(t)
	fresh := 0
	for id, r := range results {
		if r.Outcome == Wrong && !known[id] {
			fresh++
			t.Errorf("NEW WRONG %s: %s", id, r.Reason)
		}
	}
	counts := Count(results)
	t.Logf("accept tier: %s (%d wrong are known debt in the ledger)", counts, counts.Wrong-fresh)
	if fresh > 0 {
		t.Fatalf("%d cases produced fresh wrong output not in the ledger, this must be zero", fresh)
	}
}

// TestLedger regenerates the classification ledger over the whole corpus and
// checks it against the committed status/ledger.txt. It is the regression gate:
// a case that was a clean handback and is now wrong changes its line, and the
// byte comparison fails, so a miscompile that slips past a filtered local run is
// caught here. A coverage gain that turns a handback into a pass also changes the
// file, which is intended, a developer reviews the shrinking ledger and refreezes
// it with -update-ledger. It never runs under -filter, because a partial ledger
// is not a baseline.
func TestLedger(t *testing.T) {
	if *filter != "" {
		t.Skip("the ledger is only coherent over the whole corpus, run without -filter")
	}
	results := fullCorpus(t)
	got := FormatLedger(results)
	t.Logf("ledger baseline: %s", Count(results))

	if *updateLedger {
		if err := os.WriteFile(ledgerPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write ledger: %v", err)
		}
		return
	}
	want, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("read ledger (run with -update-ledger to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("classification ledger has drifted from status/ledger.txt\n"+
			"run `go test ./suite -run TestLedger -update-ledger` after reviewing the change\n"+
			"--- got ---\n%s", got)
	}
}

// TestEmitGolden is T1, the emit-determinism tier: the Go bento emits for an
// accepted case is exactly the committed golden. It is a no-silent-drift claim,
// not a correctness one, so a lowering change that alters an emit surfaces as a
// reviewable diff before it lands, and a case can pass here with Go that computes
// the wrong answer as long as that Go is stable. Correctness is T2's job.
//
// Under -update-goldens it rewrites the goldens instead of checking, the one
// supported way to move them, and on a full run it prunes a golden whose case no
// longer accepts so the tree is exactly the accepted set. A filtered run touches
// only its subset and never prunes, since it cannot see the whole accepted set. A
// full check also fails on an accepted case with no golden and on an orphan golden
// with no accepting case, so the committed tree and the accepted set stay in step.
func TestEmitGolden(t *testing.T) {
	var results map[string]Result
	full := *filter == ""
	if full {
		results = fullCorpus(t)
	} else {
		results = classifyConcurrent(moduleRoot(t), selectCases(t), *jobs)
	}

	accepted := map[string]string{} // id -> emitted Go
	for id, r := range results {
		if r.Outcome == Pass {
			accepted[id] = r.Go
		}
	}

	if *updateGoldens {
		for id, goSrc := range accepted {
			if err := writeGolden(id, goSrc); err != nil {
				t.Fatalf("write golden %s: %v", id, err)
			}
		}
		if full {
			pruneOrphanGoldens(t, accepted)
		}
		t.Logf("emit goldens: wrote %d accepted cases", len(accepted))
		return
	}

	for id, goSrc := range accepted {
		want, err := os.ReadFile(goldenPath(id))
		if err != nil {
			t.Errorf("accepted case %s has no golden (run -update-goldens): %v", id, err)
			continue
		}
		if goSrc != string(want) {
			t.Errorf("emitted Go for %s has drifted from its golden\n"+
				"run `go test ./suite -run TestEmitGolden -update-goldens` after reviewing the change", id)
		}
	}
	if full {
		checkNoOrphanGoldens(t, accepted)
	}
}

// pruneOrphanGoldens removes a golden whose case no longer accepts, so a coverage
// change that turns a pass into a handback drops the stale golden under -update
// rather than leaving it to rot. It runs only on a full update, the one run that
// knows the whole accepted set.
func pruneOrphanGoldens(t *testing.T, accepted map[string]string) {
	t.Helper()
	have, err := existingGoldens()
	if err != nil {
		t.Fatalf("scan goldens: %v", err)
	}
	for id := range have {
		if _, ok := accepted[id]; !ok {
			if err := os.Remove(goldenPath(id)); err != nil {
				t.Fatalf("prune orphan golden %s: %v", id, err)
			}
		}
	}
}

// checkNoOrphanGoldens fails on a committed golden with no accepting case, the
// mirror of the missing-golden check, so the goldens tree can neither gain a stale
// file nor miss a live one without the tier catching it.
func checkNoOrphanGoldens(t *testing.T, accepted map[string]string) {
	t.Helper()
	have, err := existingGoldens()
	if err != nil {
		t.Fatalf("scan goldens: %v", err)
	}
	for id := range have {
		if _, ok := accepted[id]; !ok {
			t.Errorf("orphan golden %s has no accepting case (run -update-goldens to prune)", id)
		}
	}
}

// applyShard slices ids to the -shard selection, returning it whole when no
// shard is set. The spec is i/N, and the slice is the i-th of N contiguous even
// parts of the already-sorted ids, so the shards partition the set with no
// overlap and no gap. A malformed spec or an out-of-range index is fatal, since a
// shard that silently selects nothing would pass a CI job for the wrong reason.
func applyShard(t *testing.T, ids []string) []string {
	t.Helper()
	if *shard == "" {
		return ids
	}
	iStr, nStr, ok := strings.Cut(*shard, "/")
	i, err1 := strconv.Atoi(iStr)
	n, err2 := strconv.Atoi(nStr)
	if !ok || err1 != nil || err2 != nil || n < 1 || i < 0 || i >= n {
		t.Fatalf("bad -shard %q, want i/N with 0 <= i < N", *shard)
	}
	lo := len(ids) * i / n
	hi := len(ids) * (i + 1) / n
	return ids[lo:hi]
}

// nodeBin returns the Node binary the oracle generator runs baselines on,
// honoring $NODE and falling back to node on PATH. A run without Node is skipped,
// not failed: generation is a maintainer step that needs Node, while the runtime
// tier that CI runs reads the frozen oracles and does not.
func nodeBin(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("NODE")
	if bin == "" {
		bin = "node"
	}
	if _, err := exec.LookPath(bin); err != nil {
		t.Skipf("node not found (set $NODE or install node to regenerate oracles): %v", err)
	}
	return bin
}

// oracleCandidates returns the accepted cases that have a vendored .js baseline
// and are not error cases, the set the generator can try to turn into an oracle.
// A case with no baseline has no ground truth to run against. An error case, one
// with an .errors.txt baseline, is out of the runtime tier's scope by the
// soundness rule: bento must not run a program TypeScript rejected, so it gets no
// oracle and is routed to the diagnostics tier instead, even when it also emits a
// .js. The .errors.txt presence dominates the .js presence, so the check comes
// first.
func oracleCandidates(results map[string]Result) []Case {
	var out []Case
	for id, r := range results {
		if r.Outcome != Pass {
			continue
		}
		b := ResolveBaseline(baselinesRoot, id)
		if b.HasErrors() {
			continue
		}
		if !b.HasJS() {
			continue
		}
		out = append(out, Case{ID: id})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// TestGenOracle regenerates the runtime oracles from the vendored .js baselines,
// the one step that needs Node. For each accepted case with a baseline it runs
// the compiled JS through the non-determinism and environment screens, and
// freezes the survivors as oracles/<id>.txt. A skipped case, one that throws or
// is non-deterministic or environment-sensitive, has any stale oracle removed, so
// the oracle tree stays exactly the set the runtime tier can trust.
//
// It runs only under -update-oracles, because it is a maintainer step with a Node
// dependency, not a gate. Without the flag it is a no-op, and CI checks the
// frozen oracles through TestRuntime instead.
func TestGenOracle(t *testing.T) {
	if !*updateOracles {
		t.Skip("oracle generation runs only under -update-oracles, it needs Node")
	}
	bin := nodeBin(t)
	results := fullCorpus(t)
	candidates := oracleCandidates(results)

	scratch, err := os.MkdirTemp("", "genoracle-")
	if err != nil {
		t.Fatalf("scratch dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	var wrote, skipped int
	for _, c := range candidates {
		content, err := os.ReadFile(ResolveBaseline(baselinesRoot, c.ID).JS)
		if err != nil {
			t.Fatalf("read baseline %s: %v", c.ID, err)
		}
		js, module := extractJSBaseline(string(content))
		if strings.TrimSpace(js) == "" {
			// No compiled output section: nothing to run, so no oracle.
			_ = removeOracle(c.ID)
			skipped++
			continue
		}
		o, skip, err := generateOracle(bin, scratch, js, module)
		if err != nil {
			t.Fatalf("run baseline %s on node: %v", c.ID, err)
		}
		if skip != "" {
			_ = removeOracle(c.ID)
			skipped++
			continue
		}
		if err := writeOracle(c.ID, o); err != nil {
			t.Fatalf("write oracle %s: %v", c.ID, err)
		}
		wrote++
	}
	pruneOrphanOracles(t, candidates)
	t.Logf("oracles: froze %d, skipped %d of %d accepted cases with a baseline", wrote, skipped, len(candidates))
}

// TestPruneOracles drops the oracles a coverage change orphaned, without Node. It
// classifies the whole corpus, takes the oracle candidates that survive, and
// removes every oracle whose case is no longer among them. Regenerating a
// survivor's frozen output needs Node and stays behind -update-oracles, but an
// orphan only needs deleting, so when the accepted set shrinks, as it did when
// bento learned to honor a case's target and library, this refreezes the oracle
// tree to the runnable set while leaving every surviving oracle untouched. Without
// the flag it is a no-op, so an ordinary run never mutates the tree.
func TestPruneOracles(t *testing.T) {
	if !*pruneOracles {
		t.Skip("oracle pruning runs only under -prune-oracles")
	}
	results := fullCorpus(t)
	candidates := oracleCandidates(results)
	before, err := existingOracles()
	if err != nil {
		t.Fatalf("scan oracles: %v", err)
	}
	pruneOrphanOracles(t, candidates)
	after, err := existingOracles()
	if err != nil {
		t.Fatalf("scan oracles: %v", err)
	}
	t.Logf("oracle prune: %d oracles, %d candidates, pruned %d orphans", len(before), len(candidates), len(before)-len(after))
}

// pruneOrphanOracles removes an oracle whose case is no longer an accepted
// candidate, so a coverage change that drops a case from the accepted set drops
// its oracle too. The candidate set already covers every case the generator
// wrote or skipped, so anything else under oracles/ is stale.
func pruneOrphanOracles(t *testing.T, candidates []Case) {
	t.Helper()
	live := map[string]bool{}
	for _, c := range candidates {
		live[c.ID] = true
	}
	have, err := existingOracles()
	if err != nil {
		t.Fatalf("scan oracles: %v", err)
	}
	for id := range have {
		if !live[id] {
			if err := removeOracle(id); err != nil {
				t.Fatalf("prune orphan oracle %s: %v", id, err)
			}
		}
	}
}

// TestRuntime is T2, the runtime tier: it compiles and runs each case's emitted
// Go and checks the result against the case's frozen oracle. It needs no Node,
// only the frozen oracles and the Go toolchain, so CI runs it directly. The
// emitted Go it runs is the committed golden, which the emit tier holds equal to
// the live emit, so the runtime tier measures the same Go a reviewer reads.
//
// A case whose Go runs but disagrees with its oracle, wrong stdout or a wrong
// exit or a panic, is runtime-wrong. Like the accept tier's wrong set this is a
// ratchet the committed status/runtime.txt carries: a fresh runtime-wrong not in
// it fails the run and names itself, while the known debt is reported and burned
// down by fixing bento. -update-runtime refreezes the ratchet after a fix, and
// -shard splits the heavy go-run pass across CI jobs.
func TestRuntime(t *testing.T) {
	have, err := existingOracles()
	if err != nil {
		t.Fatalf("scan oracles: %v", err)
	}
	ids := make([]string, 0, len(have))
	for id := range have {
		// An error case has no business in the runtime tier: bento must not run a
		// program TypeScript rejected. The generator already withholds an oracle
		// from an error case, so this only fires on a stale oracle left behind by a
		// corpus refresh that turned a clean case into an error case, but it keeps
		// the soundness rule enforced at the point the tier reads, not only at the
		// point it writes.
		if ResolveBaseline(baselinesRoot, id).HasErrors() {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if *filter != "" {
		var kept []string
		for _, id := range ids {
			if matchFilter(id, *filter) {
				kept = append(kept, id)
			}
		}
		ids = kept
	}
	// An unfiltered, unsharded run is the whole heavy go-run pass, so it is a full
	// run that belongs on the fleet. A -filter or -shard subset, the local smoke
	// and the CI shards, runs anywhere.
	if *filter == "" && *shard == "" {
		requireFleetForFullRun(t)
	}
	ids = applyShard(t, ids)
	if len(ids) == 0 {
		if *filter != "" || *shard != "" {
			t.Skip("no oracles match the filter or shard")
		}
		t.Skip("no oracles vendored yet, run -update-oracles to generate them")
	}

	root := moduleRoot(t)
	wrong := runtimeWrong(t, root, ids)

	known := parseRuntimeLedger(readRuntimeLedger(t))
	if *updateRuntime && *filter == "" && *shard == "" {
		if err := os.WriteFile(runtimeLedgerPath, []byte(formatRuntimeLedger(wrong)), 0o644); err != nil {
			t.Fatalf("write runtime ledger: %v", err)
		}
		t.Logf("runtime tier: refroze ratchet with %d wrong of %d checked", len(wrong), len(ids))
		return
	}
	fresh := 0
	for _, w := range wrong {
		id, reason, _ := strings.Cut(w, "\t")
		if !known[id] {
			fresh++
			t.Errorf("NEW RUNTIME WRONG %s: %s", id, reason)
		}
	}
	t.Logf("runtime tier: %d checked, %d wrong (%d known debt, %d fresh)", len(ids), len(wrong), len(wrong)-fresh, fresh)
	if fresh > 0 {
		t.Fatalf("%d cases run wrong and are not in status/runtime.txt, this must be zero", fresh)
	}
}

// runtimeWrong runs every selected case's golden Go through a bounded worker pool
// and returns the ids that disagree with their oracle, each tab-joined to the
// reason so a fresh regression can report why. The pool bound matters as much as
// at the accept tier: every worker holds a go run, so the width caps how many
// compiles live at once.
func runtimeWrong(t *testing.T, root string, ids []string) []string {
	t.Helper()
	var (
		mu    sync.Mutex
		wrong []string
	)
	work := make(chan string)
	var wg sync.WaitGroup
	for range *jobs {
		wg.Go(func() {
			for id := range work {
				if reason := checkRuntime(root, id); reason != "" {
					mu.Lock()
					wrong = append(wrong, id+"\t"+reason)
					mu.Unlock()
				}
			}
		})
	}
	for _, id := range ids {
		work <- id
	}
	close(work)
	wg.Wait()
	sort.Strings(wrong)
	return wrong
}

// checkRuntime runs one case's golden Go and compares it to the case's oracle,
// returning the mismatch reason or an empty string on a match. A missing golden
// or oracle, or a toolchain that will not launch, is itself the reason, since a
// case that cannot be run has not been shown to run right.
func checkRuntime(root, id string) string {
	goSrc, err := os.ReadFile(goldenPath(id))
	if err != nil {
		return "read golden: " + err.Error()
	}
	oracleContent, err := os.ReadFile(oraclePath(id))
	if err != nil {
		return "read oracle: " + err.Error()
	}
	want, err := ParseOracle(string(oracleContent))
	if err != nil {
		return "parse oracle: " + err.Error()
	}
	got, err := runGo(root, string(goSrc))
	if err != nil {
		return "run go: " + err.Error()
	}
	ok, why := oracleMatch(want, got)
	if ok {
		return ""
	}
	return why
}

// readRuntimeLedger reads the committed runtime ratchet, treating a missing file
// as an empty ratchet so the tier works before the first freeze.
func readRuntimeLedger(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(runtimeLedgerPath)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read runtime ledger: %v", err)
	}
	return string(data)
}

// TestDiagnostics is T3, the diagnostics tier: over the error cases, the cases
// TypeScript rejects, it proves soundness, that bento does not run a program
// TypeScript refused to compile. An error case is one with a vendored .errors.txt
// baseline, and the claim is deliberately weak: bento passes by refusing the
// case, its checker reporting an error or its lowerer handing back, and never has
// to report the same TS#### code TypeScript did. That code matching is the typed/
// checker's conformance goal, not a soundness one.
//
// A diagnostics-wrong case is an error case bento accepts and builds, so it emits
// a running program for something TypeScript rejected. Like the accept and
// runtime tiers this is a ratchet the committed status/diagnostics.txt carries: a
// fresh one not in it fails the run and names itself, while the known debt is
// reported and burned down by making bento refuse the case. Closing that debt is
// gated on the typed/ checker maturing, so the ratchet starts non-empty and the
// invariant the tier enforces from day one is only that it never grows.
//
// It reuses the shared corpus classification, so it adds no build cost over the
// accept and emit tiers it runs beside, only the error-baseline reads.
func TestDiagnostics(t *testing.T) {
	var results map[string]Result
	full := *filter == ""
	if full {
		results = fullCorpus(t)
	} else {
		results = classifyConcurrent(moduleRoot(t), selectCases(t), *jobs)
	}

	wrong, errorCases, refused := diagnosticsScan(results)

	if *updateDiagnostics {
		if !full {
			t.Fatal("diagnostics ratchet is only coherent over the whole corpus, run without -filter")
		}
		if err := os.WriteFile(diagnosticsLedgerPath, []byte(formatDiagnosticsLedger(wrong)), 0o644); err != nil {
			t.Fatalf("write diagnostics ledger: %v", err)
		}
		t.Logf("diagnostics tier: refroze ratchet with %d wrong of %d error cases", len(wrong), errorCases)
		return
	}

	ledger, err := readDiagnosticsLedger()
	if err != nil {
		t.Fatalf("read diagnostics ledger: %v", err)
	}
	known := parseDiagnosticsLedger(ledger)
	fresh := 0
	for _, w := range wrong {
		id, code, _ := strings.Cut(w, "\t")
		if !known[id] {
			fresh++
			t.Errorf("NEW DIAGNOSTICS WRONG %s: bento runs a program TypeScript rejects (%s)", id, code)
		}
	}
	t.Logf("diagnostics tier: %d error cases, %d refused, %d unsound (%d known debt, %d fresh)",
		errorCases, refused, len(wrong), len(wrong)-fresh, fresh)
	if fresh > 0 {
		t.Fatalf("%d error cases run as bento-compiled Go and are not in status/diagnostics.txt, this must be zero", fresh)
	}
}

// diagnosticsScan splits the error cases out of a classification and reports the
// diagnostics tier's tallies. An error case is one with an .errors.txt baseline.
// It returns the unsound ones, each id tab-joined to the first diagnostic code so
// the ratchet can record why TypeScript rejected it, along with the total error
// case count and how many bento refused.
//
// A case bento accepts and builds (Pass) emits a running program, so it is
// unsound. A handback is a clean refusal and a T3 pass. A case that classified
// wrong at the accept tier, its emitted Go not compiling, is already tracked in
// status/ledger.txt and does not run, so it is neither a fresh soundness
// violation nor a clean refusal, and it is left to the accept ledger rather than
// double-counted here.
func diagnosticsScan(results map[string]Result) (wrong []string, errorCases, refused int) {
	for id, r := range results {
		if !ResolveBaseline(baselinesRoot, id).HasErrors() {
			continue
		}
		errorCases++
		switch r.Outcome {
		case Pass:
			wrong = append(wrong, id+"\t"+firstErrorCode(id))
		case Handback:
			refused++
		}
	}
	sort.Strings(wrong)
	return wrong, errorCases, refused
}

// firstErrorCode reads a case's .errors.txt baseline and returns its leading
// diagnostic code, the reason TypeScript rejected the case, for the ratchet to
// record. A baseline that cannot be read or carries no recognizable code yields
// an empty string, so the tier degrades to recording the id alone rather than
// failing on a malformed baseline.
func firstErrorCode(id string) string {
	data, err := os.ReadFile(ResolveBaseline(baselinesRoot, id).Errors)
	if err != nil {
		return ""
	}
	codes := ErrorCodes(string(data))
	if len(codes) == 0 {
		return ""
	}
	return codes[0]
}
