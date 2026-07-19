package suite

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// diagnosticsLedgerPath is the committed diagnostics-wrong ratchet, the T3
// analogue of the accept and runtime wrong sets. It lists the error cases, cases
// TypeScript rejects, for which bento nonetheless emits a running program. Each
// line is a soundness violation: bento runs something TypeScript refused to
// compile. Like the other ratchets it may only shrink, and it is emptied by
// making bento refuse the case, its checker reporting the error or its lowerer
// handing back, never by dropping the error baseline.
const diagnosticsLedgerPath = "../status/diagnostics.txt"

// diagnosticsHeader is the comment block at the top of status/diagnostics.txt, so
// a reader opening the file learns what it is and how it moves without leaving it.
const diagnosticsHeader = `# Diagnostics-wrong ratchet for the ts-compat suite, tier T3.
#
# Each line is an error case, one TypeScript rejects with an .errors.txt baseline,
# whose Go bento emits and builds anyway: bento runs a program TypeScript refused
# to compile, which is unsound. The trailing code is the first diagnostic
# TypeScript reported, recording why it rejected the case. Every line is bento
# soundness debt, to be fixed by making bento refuse the case, its checker
# reporting the error or its lowerer handing back, never by dropping the baseline.
# This set may only shrink.
#
# T3 is soundness only: it never checks that bento reports the same code, only
# that bento does not run a rejected program. Coverage over C4 is gated on the
# typed/ checker maturing enough to reject these cases.
#
# Regenerate with: go test ./suite -run TestDiagnostics -update-diagnostics
`

// formatDiagnosticsLedger renders the diagnostics-wrong entries as the committed
// ratchet file: the header, then one `wrong <id> <code>` line per unsound case,
// sorted by id so the file is stable across runs. Each entry arrives as the id
// and the first diagnostic code tab-joined, the shape runtimeWrong uses, so the
// code rides beside the id it explains.
func formatDiagnosticsLedger(wrong []string) string {
	sorted := append([]string(nil), wrong...)
	sort.Strings(sorted)
	var b strings.Builder
	b.WriteString(diagnosticsHeader)
	for _, w := range sorted {
		id, code, _ := strings.Cut(w, "\t")
		if code == "" {
			fmt.Fprintf(&b, "wrong %s\n", id)
			continue
		}
		fmt.Fprintf(&b, "wrong %s %s\n", id, code)
	}
	return b.String()
}

// parseDiagnosticsLedger reads the committed diagnostics ratchet into the set of
// known diagnostics-wrong ids. It keys on the id alone and ignores the trailing
// code, so re-characterizing a case, a corpus refresh changing which diagnostic
// leads, does not read as a fresh regression. Comment and blank lines are skipped.
func parseDiagnosticsLedger(content string) map[string]bool {
	known := map[string]bool{}
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "wrong" {
			known[fields[1]] = true
		}
	}
	return known
}

// readDiagnosticsLedger reads the committed diagnostics ratchet, treating a
// missing file as an empty ratchet so the tier works before the first freeze.
func readDiagnosticsLedger() (string, error) {
	data, err := os.ReadFile(diagnosticsLedgerPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}
