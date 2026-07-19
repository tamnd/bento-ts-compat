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

The whole suite exists to keep the wrong count at zero.
A handback is fine and expected, and the pass count climbs as bento's lowering grows.
Wrong is a hard zero on every run, because the contract is that bento says no and emits nothing before it ever says yes and emits something incorrect.

## The tiers

The suite checks a case at increasing depth.

- **accept**: drive the case through `build.EmitGo` and classify it. A pass must not panic, and its emitted Go must build. This is the tier that is live today.
- **emit golden**: the emitted Go for a pass is frozen as a golden and checked byte for byte, so a lowering change is a reviewable diff.
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

- `-run-filter SUBSTRING` run only cases whose id contains the substring, the incremental seam when working on one lowering path
- `-jobs N` how many cases to drive through the front end at once, default half the machine's CPUs
- `-run NAME` the standard Go test filter, to pick `TestStructure`, `TestAccept`, or the format-layer unit tests

For example, to run the accept tier over just the enum conformance cases:

```
go test ./suite/ -run TestAccept -run-filter conformance/enums/ -v
```

## Layout

- `suite/` the harness: corpus discovery, the case format layer, the emit classifier, and the tier tests
- `corpus/cases/` the vendored TypeScript cases, mirroring the upstream `compiler` and `conformance` roots
- `corpus/PIN` the pinned corpus revisions
- `scripts/vendor.sh` refresh the corpus from a pinned commit

## License

MIT.
The vendored corpus under `corpus/` is from microsoft/TypeScript and microsoft/typescript-go, both Apache-2.0, credited in `NOTICE`.
