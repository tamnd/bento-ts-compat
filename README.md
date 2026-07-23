# bento-ts-compat

Compatibility suite for [bento](https://github.com/tamnd/bento), the TypeScript runtime built in Go, run against Microsoft's own TypeScript test corpus.

bento compiles TypeScript two ways.
One path type-strips and interprets.
The other, the ahead-of-time path, type-checks a program through the typescript-go front end, lowers it to Go source, and compiles that.
This suite exercises only the ahead-of-time path.
The unit under test is the Go that `build.EmitGo` produces, and only that Go.
Nothing here touches the interpreted run path.

## The three outcomes

Every case resolves one of three ways.

- **pass**: bento emitted Go for the case.
- **handback**: bento declined the case with a named reason and emitted nothing. This is the expected outcome for the large part of the corpus that leans on checker features or module shapes the ahead-of-time path does not lower yet. A handback is a clean decline, not a failure.
- **wrong**: bento produced Go that must not exist. It panicked while lowering, or it emitted Go that does not compile, or that runs to the wrong answer.

A handback is fine and expected, and the pass count climbs as bento's lowering grows.
The suite exists to drive the wrong count to zero and hold it there.
The contract is that bento says no and emits nothing before it ever says yes and emits something incorrect, so every wrong case is a bento bug to fix.

The wrong count is not yet zero.
Turning on the build-check surfaced a batch of cases where bento emits Go that does not compile, each a real lowering bug.
Those cases are recorded in the ledger as known wrong, so the suite stays green and every case is measured while the bugs are burned down in bento.
The recorded set is a ratchet: it may only shrink, a fresh miscompile that is not already in the ledger fails the run, and the end state is an empty wrong set reached by fixing bento to emit correct Go, never by making it hand back.

## The tiers

The suite checks a case at increasing depth.

- **accept**: drive the case through `build.EmitGo` and classify it. A pass must not panic, and its emitted Go is compiled with `go build` to confirm it is well formed. Live today.
- **emit golden**: the emitted Go for a pass is frozen under `suite/testdata/goldens/` and checked byte for byte, so a lowering change is a reviewable diff before it lands. Live today. This is a no-drift claim, not a correctness one: a case can pass here with Go that computes the wrong answer as long as that Go is stable, and correctness is the runtime tier's job.
- **runtime**: the emitted Go is compiled and run, and its output is checked against an oracle frozen from the TypeScript compiler's own `.js` baseline for the case. Live today. This corpus is a type-checking suite, so most cases print nothing and the oracle mostly asserts a clean exit, which is what catches a lowering bug that compiles but panics at run time.
- **diagnostics**: a case TypeScript rejects must not produce a running program. Live today. This tier is soundness only: bento passes by refusing the case, its checker reporting an error or its lowerer handing back, and it never has to report the same error code TypeScript did. Closing the cases where bento still runs a rejected program is gated on bento's typed checker, so the diagnostics-wrong set starts non-empty and the tier enforces from day one only that it never grows.

## The corpus

The cases under `corpus/cases` are vendored from the TypeScript test suite that bento's front end pins, not authored here.
bento pins microsoft/typescript-go, that port pins microsoft/TypeScript at a fixed commit, and its `tests/cases/compiler` and `tests/cases/conformance` are the two roots this suite runs.
`corpus/PIN` records the exact revisions, and `scripts/vendor.sh` is the reproducible record of how the tree was built.
Because the corpus is pinned to the same front end bento ships, the checker that accepts or declines a case here is the exact one bento uses.

## Running it

You need Go.
bento is a pinned module dependency, so nothing else has to be installed and no network runtime is involved.

```
go test ./suite/
```

The accept tier drives every case through a checker, which is memory heavy, so the run bounds its fan-out rather than spawning one goroutine per case.

Useful flags:

- `-filter PATTERN` run only cases whose id matches the pattern, by substring or by path glob, the incremental seam when working on one lowering path
- `-jobs N` how many cases to classify at once, default half the machine's CPUs
- `-update-ledger` rewrite `status/ledger.txt` from the current classification, the one supported way to move the baseline
- `-update-goldens` rewrite `suite/testdata/goldens/` from the current emit, and on a full run prune a golden whose case no longer accepts, the one supported way to move a golden
- `-update-oracles` regenerate the runtime oracles from the `.js` baselines on Node, the one step that needs Node and the one supported way to move the oracle set
- `-update-runtime` rewrite `status/runtime.txt` from the current runtime comparison, the one supported way to move the runtime-wrong ratchet
- `-update-diagnostics` rewrite `status/diagnostics.txt` from the current error-case classification, the one supported way to move the diagnostics-wrong ratchet
- `-shard i/N` run only the i-th of N even slices of the selected cases, how the runtime tier's go-run pass is split across parallel CI jobs
- `-run NAME` the standard Go test filter, to pick `TestStructure`, `TestAccept`, `TestLedger`, `TestEmitGolden`, `TestRuntime`, `TestDiagnostics`, or the format-layer unit tests

For example, to run the accept tier over just the enum conformance cases:

```
go test ./suite/ -run TestAccept -filter conformance/enums/ -v
```

### Where the full run belongs

A full-corpus run drives a checker and, on a pass, a `go build` over twelve thousand cases, minutes of memory-heavy work.
It runs on the Linux test fleet and in CI, not on a local laptop.
The suite enforces this: a full run on macOS, the developer machine, skips with a pointer to the fleet, while a Linux run always proceeds, so CI needs no extra configuration and the ledger, golden, and diagnostics gates stay live there.
The local iteration loop is a `-filter` or `-shard` subset, which runs anywhere.
Set `BENTO_TS_COMPAT_LARGE=1` to force the full run on the laptop for the rare case you want it.

### The classification cache

A run memoizes each case's verdict on disk under the OS temp dir, so a re-run only re-checks and re-builds the cases whose input actually changed.
The cache is keyed on the toolchain that produced the verdicts, the test binary, the module's `go.sum`, and the Go version, so a bento re-pin or a suite change lands in a fresh cache and a stale verdict is never read; within a toolchain it is keyed on each case file's content, so editing a case re-checks exactly that case.
A re-run of an unchanged subset is a content-hash lookup rather than a rebuild, several times faster.
The cache is bounded: a toolchain change prunes the prior toolchain's directory, and the harness drops the whole cache once it grows past a cap, the same bounded-churn contract the dedicated `go build` cache keeps, so neither grows without end and the developer's own `GOCACHE` never sees the churn.
The cache is advisory, any read or write error degrades to computing live, and `BENTO_TS_COMPAT_NO_CACHE=1` disables it for a clean-room run that recomputes every verdict.

## The ledger

`status/ledger.txt` records every non-passing case, one `<status> <case id>` line, sorted by id.
A pass is not listed, so the ledger is the complement of the pass set: it shrinks as bento's lowering grows, and its length is the non-passing count at a glance.
It is generated, never hand-edited.
`TestLedger` regenerates the classification over the whole corpus and checks it against the committed file, so a case that was a clean handback and is now wrong changes its line and fails the run.
A coverage gain that turns a handback into a pass also changes the file, which is the point: it is a reviewable diff, and a developer refreezes the shrunk ledger with `-update-ledger`.
The `wrong` lines are the known-wrong debt, the cases where bento emits Go that does not compile today.
They are the burn-down list: each is a bento bug, and the count only goes down as bento is fixed.

## The goldens

`suite/testdata/goldens/<id>.go` is the frozen Go bento emits for each accepted case, mirroring the corpus layout so a golden sits beside the case it comes from.
`TestEmitGolden` re-emits every accepted case and byte-checks it against its golden, so a lowering change that alters an emit is a diff a reviewer sees before it lands, the same review surface the bento benchmark goldens give.
The tree lives under `testdata` so the go tool skips it when it expands `./...`: each golden is a standalone `package main` the runtime tier compiles in isolation, not a package of this module.
The header names the corpus, not the bento build, so a golden does not churn when bento's version string moves.
It is a determinism claim only: a golden can encode Go that computes the wrong answer, and the runtime tier is what catches that.
The goldens tree is exactly the accepted set, so an accepted case with no golden and an orphan golden with no case both fail, and `-update-goldens` on a full run writes the one and prunes the other.

## The oracles and the runtime tier

`suite/testdata/oracles/<id>.txt` is the frozen expected result of running a case: its stdout and exit, in the same framed format bento's own conformance corpus uses.
An oracle is generated offline from the case's vendored `.js` baseline, the TypeScript compiler's own emit, run on a pinned Node under two screens.
A baseline whose output changes across three runs is non-deterministic, and one whose output changes under a perturbed timezone and locale is environment-sensitive, and both are dropped rather than frozen.
A baseline that throws is also dropped, because on this corpus a throw is almost always a reference to an ambient declaration Node does not have, which the compiled Go, treating it as a real binding, does not reproduce, so the throw is an artifact of running the bare `.js` and not the case's meaning.
What survives is a deterministic, self-contained, clean exit, and `-update-oracles` freezes it. That step is the only one that needs Node.

`TestRuntime` compiles and runs each case's golden Go and checks its stdout and exit against the frozen oracle, so it needs no Node, only the Go toolchain.
Number text is compared byte for byte, since bento's `value` package must print a number exactly as Node does, and only trailing newlines and CRLF are normalized away.
A case whose Go runs but disagrees, wrong stdout or a wrong exit or a panic, is runtime-wrong, and `status/runtime.txt` is the ratchet of those known runtime bugs, the same shrink-only debt model the accept ledger uses.
The go-run pass is heavy, so it is split with `-shard i/N`: a smoke shard runs on every push and the full set runs sharded on the weekly schedule.

## The diagnostics tier

A case TypeScript rejects ships an `.errors.txt` baseline, the compiler's diagnostics for it, and the presence of that file is the signal that a case is an error case.
`corpus/baselines/<id>.errors.txt` is vendored for every error case, not just the accepted ones, because the diagnostics tier's scope is the whole error-case set: a handback error case is a pass that still counts toward coverage.
`TestDiagnostics` routes every error case out of the runtime tier and checks soundness: bento must refuse the program, its checker reporting an error or its lowerer handing back, and a case bento accepts and builds anyway is diagnostics-wrong, because it runs a program TypeScript refused to compile.
The tier is soundness only, it never checks that bento reports the same `TS####` code, so it reads the code from the baseline for one purpose only, to record beside a case in the ratchet why TypeScript rejected it.
`status/diagnostics.txt` is the ratchet of those known-unsound cases, the same shrink-only debt model the accept and runtime ratchets use, and it starts non-empty because closing it waits on the typed checker learning to reject these programs.
An error case never reaches the runtime tier: bento must not run what TypeScript rejected, so the oracle generator withholds an oracle from an error case even when it also emits a `.js`, and the runtime tier drops any stale one it finds.

## Layout

- `suite/` the harness: corpus discovery, the case format layer, the emit classifier, and the tier tests
- `suite/testdata/goldens/` the frozen emit for every accepted case, mirroring the corpus layout
- `suite/testdata/oracles/` the frozen runtime oracles, one per case the runtime tier checks
- `corpus/cases/` the vendored TypeScript cases, mirroring the upstream `compiler` and `conformance` roots
- `corpus/baselines/` the vendored baselines: `.js` for the accepted cases, the oracle generator's input, and `.errors.txt` for every error case, the diagnostics tier's routing signal
- `corpus/PIN` the pinned corpus revisions
- `status/ledger.txt` the classification baseline and known-wrong debt
- `status/runtime.txt` the runtime-wrong ratchet
- `status/diagnostics.txt` the diagnostics-wrong ratchet
- `scripts/vendor.sh` refresh the corpus, `scripts/vendor-baselines.sh` refresh the baselines, both from a pinned commit

## License

MIT.
The vendored corpus under `corpus/` is from microsoft/TypeScript and microsoft/typescript-go, both Apache-2.0, credited in `NOTICE`.
