package suite

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/bento/pkg/build"
)

// Outcome is how a case resolved when driven through the accept tier. It has
// three values and no fourth: a case either produced compilable Go, was declined
// cleanly, or produced something wrong. The whole suite exists to keep the third
// count at zero.
type Outcome int

const (
	// Pass means bento emitted Go for the case without error. At the accept tier
	// that is as far as the claim goes: the emit happened. Whether that Go
	// compiles is the accept tier's own check, and whether it runs correctly is
	// the runtime tier's.
	Pass Outcome = iota
	// Handback means bento declined the case with an error and emitted nothing.
	// This is the expected outcome for the large part of the corpus that leans on
	// checker features or module shapes the ahead-of-time path does not lower yet.
	// A handback is a clean decline, not a failure: the contract is that bento
	// says no and produces no Go, never that it says yes and produces wrong Go.
	Handback
	// Wrong means bento produced Go that must not exist: it panicked while
	// lowering, or (at the accept tier) it emitted Go that does not compile. Wrong
	// is the one outcome the suite forbids. Its count is a gate, not a metric, and
	// it must be zero on every run.
	Wrong
)

// String renders an Outcome for ledger lines and test messages.
func (o Outcome) String() string {
	switch o {
	case Pass:
		return "pass"
	case Handback:
		return "handback"
	case Wrong:
		return "wrong"
	default:
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
}

// Result is the full record of driving one case through the emit step: its
// outcome, the Go it produced on a pass, and the reason it was declined or
// rejected otherwise. Reason is empty on a clean pass, carries the handback
// message on a handback, and carries the panic or compile failure on a wrong.
type Result struct {
	Outcome Outcome
	Go      string
	Reason  string
}

// stamp is the fixed identifier written into every emitted header, in place of
// the version and commit the CLI records. A golden names the corpus, not the
// exact bento build that produced it, so goldens do not churn when that build's
// version string moves. The suite's own pin file records which bento the corpus
// was frozen against.
const stamp = "bento-ts-compat"

// preEmitHandback screens a case for shapes the ahead-of-time path cannot take
// as a single compilable unit before build.EmitGo is ever called, and returns
// the handback reason for the first that matches. Screening here rather than
// leaning on EmitGo to fail keeps the reason specific and stable: a multi-file
// case is declined as multi-file, not as whatever downstream error the extra
// files happen to trigger. An empty return means the case is in scope and the
// emit step should run it for real.
func preEmitHandback(p Parsed) string {
	if p.IsMultiFile() {
		return "multi-file case: the ahead-of-time path compiles one entry module"
	}
	// A declaration-only or no-emit case asks the checker for types or .d.ts
	// output and expects no program, so there is nothing for the runtime tiers to
	// run and lowering it as a program would be measuring the wrong thing. Route it
	// to a clean decline.
	if p.Directives.Bool("declaration") && p.Directives.Bool("emitDeclarationOnly") {
		return "declaration-only case: no program to lower"
	}
	if p.Directives.Bool("noEmit") {
		return "noEmit case: the case asks for checking without a program"
	}
	// An outFile or module-concatenation case describes a bundling layout the
	// single-entry path does not model, so decline it by shape rather than let the
	// emit step stumble over the layout.
	if _, ok := p.Directives.Get("outFile"); ok {
		return "outFile case: bundled output is not a single-entry program"
	}
	if _, ok := p.Directives.Get("out"); ok {
		return "out case: bundled output is not a single-entry program"
	}
	return ""
}

// Classify drives one case through the emit step and returns its Result. It is
// the heart of the accept tier and the shared front end of every tier above it:
// the runtime tier runs the Go a pass produced here, and the ledger records the
// outcome this returns. It never runs the compiled Go and never touches the
// interpreted path.
//
// The order of judgement is deliberate. A pre-emit screen declines out-of-scope
// shapes by name first. Then build.EmitGo runs inside a panic recover, because a
// panic in the front end is a compiler bug the suite must surface as wrong, not
// let abort the run. A returned error is a handback, the front end's own clean
// decline. Success is a pass carrying the emitted Go for the tiers above.
func Classify(c Case) Result {
	source, err := os.ReadFile(c.File)
	if err != nil {
		return Result{Outcome: Wrong, Reason: fmt.Sprintf("read case: %v", err)}
	}
	parsed := Parse(string(source))
	if reason := preEmitHandback(parsed); reason != "" {
		return Result{Outcome: Handback, Reason: reason}
	}
	return classifyEmit(c.File)
}

// classifyEmit runs build.EmitGo for a single-entry case under a panic recover
// and maps the three ways it can end to the three outcomes. It is split from
// Classify so the recover covers only the front-end call and nothing in the
// screening around it.
func classifyEmit(file string) (r Result) {
	defer func() {
		if p := recover(); p != nil {
			r = Result{Outcome: Wrong, Reason: fmt.Sprintf("panic lowering %s: %v", filepath.Base(file), p)}
		}
	}()
	code, err := build.EmitGo(file, stamp)
	if err != nil {
		return Result{Outcome: Handback, Reason: strings.TrimSpace(err.Error())}
	}
	return Result{Outcome: Pass, Go: code}
}
