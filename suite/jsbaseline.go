package suite

import (
	"regexp"
	"strings"
)

// A vendored .js baseline is the TypeScript compiler's own emit for a case, in
// its framed multi-section form. The top of the file is a header naming the
// source, then each output artifact follows under its own header: the compiled
// .js, and where the case asks for them a .d.ts and a .js.map. Only the compiled
// .js sections are a runnable program, so the oracle generator pulls those out
// and drops the rest.
//
// A section header is `//// [name]` on its own line. The one header that names
// the whole case ends in an extra ` ////`, so it is told apart from the artifact
// headers, which are bare. This is the same framing bento's own front-end port
// writes under testdata/baselines, so the parser here matches what that port
// emits.
var baselineHeader = regexp.MustCompile(`^//// \[(.+?)\](\s*////)?\s*$`)

// esmSignal matches a top-level export or import statement, the mark of an ES
// module baseline. The module form the TypeScript compiler emitted decides how
// Node must run the output: an ES module under a .mjs entry, a CommonJS script
// under .cjs. Guessing wrong turns a clean run into a spurious SyntaxError, so
// the generator reads the form off the code rather than assuming one.
var esmSignal = regexp.MustCompile(`(?m)^\s*(export|import)\b`)

// jsModule is which module system a baseline's compiled output is written in, so
// the Node runner can give it the right entry extension.
type jsModule int

const (
	// moduleCommonJS is the default: a plain script or CommonJS output, run under
	// a .cjs entry.
	moduleCommonJS jsModule = iota
	// moduleESM is ES module output with top-level import or export, run under a
	// .mjs entry so Node parses the module syntax rather than rejecting it.
	moduleESM
)

// ext returns the Node entry extension that makes Node parse the baseline in the
// module system it was written in.
func (m jsModule) ext() string {
	if m == moduleESM {
		return ".mjs"
	}
	return ".cjs"
}

// extractJSBaseline pulls the runnable program out of a framed .js baseline: it
// concatenates every compiled .js output section in order and reports the module
// system the code is written in. Source .ts sections, declaration .d.ts sections
// and source maps are dropped, since only the compiled .js is a program to run.
// The module form is read off the assembled code, so a case whose output uses
// import or export is run as an ES module and everything else as a CommonJS
// script.
func extractJSBaseline(content string) (js string, module jsModule) {
	var keep []string
	inJS := false
	for line := range strings.SplitSeq(content, "\n") {
		if m := baselineHeader.FindStringSubmatch(line); m != nil {
			// The case header carries the trailing ////; the artifact headers are
			// bare. Only a bare header that names a .js file opens an output section.
			topLevel := strings.TrimSpace(m[2]) != ""
			inJS = !topLevel && strings.HasSuffix(m[1], ".js")
			continue
		}
		if inJS {
			keep = append(keep, line)
		}
	}
	js = strings.Join(keep, "\n")
	module = moduleCommonJS
	if esmSignal.MatchString(js) {
		module = moduleESM
	}
	return js, module
}
