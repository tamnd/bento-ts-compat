package suite

import (
	"os"
	"path/filepath"
	"strings"
)

// Baseline locates the vendored oracle and diagnostics artifacts a case is
// checked against. The .js baseline is the Go compiler's own emit for the case,
// vendored from the front-end port's testdata, and the offline oracle generator
// turns it into the framed oracle the runtime tier compares live output to. The
// .errors.txt baseline is the checker's expected diagnostics, read by the
// diagnostics tier. Both are optional: a case with neither is in scope only for
// the accept and emit tiers.
//
// This type resolves paths and reports presence. It does not run Node and does
// not parse the .js, that is the offline generator's job at C3. Wiring the
// resolver now lets the accept tier record which cases will have an oracle
// without depending on the oracle data being vendored yet.
type Baseline struct {
	// JS is the absolute path the case's .js baseline would live at, whether or
	// not it exists on disk yet.
	JS string
	// Errors is the absolute path the case's .errors.txt diagnostics baseline
	// would live at.
	Errors string
}

// ResolveBaseline maps a case id to where its baselines live under root. The
// baseline tree mirrors the cases tree: a case at conformance/types/enum/foo.ts
// has its baseline at <root>/conformance/types/enum/foo.js, the same layout the
// front-end port writes under testdata/baselines/reference/submodule. The names
// are derived, not read, so this is cheap and does no I/O.
func ResolveBaseline(root, caseID string) Baseline {
	rel := strings.TrimSuffix(caseID, filepath.Ext(caseID))
	base := filepath.Join(root, filepath.FromSlash(rel))
	return Baseline{
		JS:     base + ".js",
		Errors: base + ".errors.txt",
	}
}

// HasJS reports whether the case's .js baseline is vendored. A case with a .js
// baseline is a candidate for the runtime tier once its oracle is generated; one
// without is not, because there is no ground truth to run its output against.
func (b Baseline) HasJS() bool { return fileExists(b.JS) }

// HasErrors reports whether the case's .errors.txt diagnostics baseline is
// vendored. A case with one is a candidate for the diagnostics tier, a case
// without it is expected to type-check clean.
func (b Baseline) HasErrors() bool { return fileExists(b.Errors) }

// fileExists reports whether path names an existing regular file. A directory or
// a missing path both read as absent, which is what every caller wants: they ask
// about an artifact, and only a real file is one.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
