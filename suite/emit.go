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
const stamp = "ts-compat"

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
	// A case whose one real file is named with a JavaScript extension through its
	// `// @filename` marker is a JavaScript case: the checker reads it as JS, so the
	// baseline carries the errors TypeScript reports only in a JS file (a type alias
	// in a .js, an incompatible JSDoc overload). bento's ahead-of-time path compiles
	// TypeScript, not JavaScript, so it declines a .js entry by shape here rather than
	// lowering the .ts spelling the corpus stored and missing the JS-only rejection.
	if ext := javaScriptEntryExt(p); ext != "" {
		return "JavaScript case: the ahead-of-time path compiles TypeScript, not a " + ext + " entry"
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
	// A noUnusedLocals, noUnusedParameters, or noUnusedTypeParameters case asks the
	// checker to flag a binding, parameter, or type parameter that is declared and
	// never read. That unused-symbol analysis is a checker lint, not a lowering: the
	// program is legal without the flag, so the ahead-of-time path emits running Go
	// for it, where TypeScript rejects it. Modeling the analysis is checker work a
	// later slice owns, so decline the case by name rather than run a program the
	// checker refuses.
	if p.Directives.Bool("noUnusedLocals") ||
		p.Directives.Bool("noUnusedParameters") ||
		p.Directives.Bool("noUnusedTypeParameters") {
		return "noUnused* case: the unused-symbol lint is a checker feature the ahead-of-time path does not perform"
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

// javaScriptEntryExt returns the JavaScript extension of the case's entry file
// when its one real file is named with a JavaScript extension by a `// @filename`
// marker, and the empty string otherwise. Only a single-file case reaches this,
// so the entry is that file: a marker naming it foo.js makes the whole case a
// JavaScript unit. A case with no marker, or one naming a .ts file, is TypeScript
// and returns the empty string.
func javaScriptEntryExt(p Parsed) string {
	for _, f := range p.Files {
		if f.Name == "" || !hasCode(f.Content) {
			continue
		}
		switch ext := strings.ToLower(filepath.Ext(f.Name)); ext {
		case ".js", ".mjs", ".cjs", ".jsx":
			return ext
		}
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
	return classifyEmit(c.File, emitOptions(parsed.Directives))
}

// emitOptions maps a case's compiler directives to the project configuration
// bento honors while it checks and gates the case, so the AOT path checks the
// case under the same options TypeScript did rather than under bento's fixed
// defaults. Only the settings that change whether a diagnostic gates are carried;
// the rest of the tsconfig surface does not reach the emit decision.
func emitOptions(d Directives) build.EmitOptions {
	opts := build.EmitOptions{
		NoImplicitAny: noImplicitAny(d),
		ImportHelpers: d.Bool("importHelpers"),
	}
	if v, ok := d.Get("target"); ok {
		opts.Target = strings.TrimSpace(v)
	}
	if v, ok := d.Get("allowUnreachableCode"); ok {
		allow := strings.EqualFold(strings.TrimSpace(v), "true")
		opts.AllowUnreachableCode = &allow
	}
	// A case that turns strict off while keeping noImplicitAny on is checked under
	// exactly that pair rather than under bento's forced-strict default. Forced strict
	// keeps strictNullChecks on, under which undefined and null stay their own types
	// instead of widening to any, so a noImplicitAny report the case's real options
	// would raise on a widened form is masked, the wideningTuples shape. Only this pair
	// flips strict off, because forced strict is otherwise sound: it only ever rejects
	// more than a case's own options would. A plain non-strict case keeps bento's
	// strict checking for the precise types the lowerer wants.
	if opts.NoImplicitAny && strictExplicitlyFalse(d) {
		off := false
		opts.Strict = &off
	}
	return opts
}

// strictExplicitlyFalse reports whether the case turned strict off without turning
// strictNullChecks back on. That pair is the one where bento's forced-strict default
// diverges from the case: strict off widens undefined and null to any, and a case
// that re-enabled strictNullChecks would keep them narrow, so it is not the widening
// shape and stays under the strict default.
func strictExplicitlyFalse(d Directives) bool {
	v, ok := d.Get("strict")
	if !ok || !strings.EqualFold(strings.TrimSpace(v), "false") {
		return false
	}
	if snc, ok := d.Get("strictNullChecks"); ok && strings.EqualFold(strings.TrimSpace(snc), "true") {
		return false
	}
	return true
}

// noImplicitAny reports the case's effective noImplicitAny setting. An explicit
// noImplicitAny directive wins; absent one, strict implies it, matching how the
// checker derives the flag. A case that sets noImplicitAny:false while strict is
// on, as the widening cases do, is honored as off.
func noImplicitAny(d Directives) bool {
	if v, ok := d.Get("noImplicitAny"); ok {
		return strings.EqualFold(strings.TrimSpace(v), "true")
	}
	return d.Bool("strict")
}

// classifyEmit runs build.EmitGo for a single-entry case under a panic recover
// and maps the three ways it can end to the three outcomes. It is split from
// Classify so the recover covers only the front-end call and nothing in the
// screening around it.
func classifyEmit(file string, opts build.EmitOptions) (r Result) {
	defer func() {
		if p := recover(); p != nil {
			r = Result{Outcome: Wrong, Reason: fmt.Sprintf("panic lowering %s: %v", filepath.Base(file), p)}
		}
	}()
	code, err := build.EmitGoWithOptions(file, stamp, opts)
	if err != nil {
		return Result{Outcome: Handback, Reason: strings.TrimSpace(err.Error())}
	}
	return Result{Outcome: Pass, Go: code}
}
