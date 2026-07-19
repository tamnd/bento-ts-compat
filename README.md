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
- **emit golden**: the emitted Go for a pass is frozen under `goldens/` and checked byte for byte, so a lowering change is a reviewable diff before it lands. Live today. This is a no-drift claim, not a correctness one: a case can pass here with Go that computes the wrong answer as long as that Go is stable, and correctness is the runtime tier's job.
- **runtime**: the emitted Go is compiled and run, and its output is checked against an oracle derived from the TypeScript compiler's own `.js` baseline for the case.
- **diagnostics**: a case TypeScript rejects must not produce a running program. This tier is soundness only and grows with bento's typed checker.

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
- `-update-goldens` rewrite `goldens/` from the current emit, and on a full run prune a golden whose case no longer accepts, the one supported way to move a golden
- `-run NAME` the standard Go test filter, to pick `TestStructure`, `TestAccept`, `TestLedger`, `TestEmitGolden`, or the format-layer unit tests

For example, to run the accept tier over just the enum conformance cases:

```
go test ./suite/ -run TestAccept -filter conformance/enums/ -v
```

## The ledger

`status/ledger.txt` records every non-passing case, one `<status> <case id>` line, sorted by id.
A pass is not listed, so the ledger is the complement of the pass set: it shrinks as bento's lowering grows, and its length is the non-passing count at a glance.
It is generated, never hand-edited.
`TestLedger` regenerates the classification over the whole corpus and checks it against the committed file, so a case that was a clean handback and is now wrong changes its line and fails the run.
A coverage gain that turns a handback into a pass also changes the file, which is the point: it is a reviewable diff, and a developer refreezes the shrunk ledger with `-update-ledger`.
The `wrong` lines are the known-wrong debt, the cases where bento emits Go that does not compile today.
They are the burn-down list: each is a bento bug, and the count only goes down as bento is fixed.

## The goldens

`goldens/<id>.go` is the frozen Go bento emits for each accepted case, mirroring the corpus layout so a golden sits beside the case it comes from.
`TestEmitGolden` re-emits every accepted case and byte-checks it against its golden, so a lowering change that alters an emit is a diff a reviewer sees before it lands, the same review surface the bento benchmark goldens give.
The header names the corpus, not the bento build, so a golden does not churn when bento's version string moves.
It is a determinism claim only: a golden can encode Go that computes the wrong answer, and the runtime tier is what catches that.
The goldens tree is exactly the accepted set, so an accepted case with no golden and an orphan golden with no case both fail, and `-update-goldens` on a full run writes the one and prunes the other.

## Layout

- `suite/` the harness: corpus discovery, the case format layer, the emit classifier, and the tier tests
- `corpus/cases/` the vendored TypeScript cases, mirroring the upstream `compiler` and `conformance` roots
- `corpus/PIN` the pinned corpus revisions
- `goldens/` the frozen emit for every accepted case, mirroring the corpus layout
- `status/ledger.txt` the classification baseline and known-wrong debt
- `scripts/vendor.sh` refresh the corpus from a pinned commit

## License

MIT.
The vendored corpus under `corpus/` is from microsoft/TypeScript and microsoft/typescript-go, both Apache-2.0, credited in `NOTICE`.
