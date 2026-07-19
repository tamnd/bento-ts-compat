package suite

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// baselinesRoot is where the vendored .js baselines live relative to this
// package. Only the accepted cases' baselines are vendored, the subset the
// runtime tier can turn into oracles, so the tree is a few megabytes rather than
// the whole upstream baseline set.
const baselinesRoot = "../corpus/baselines"

// oraclesRoot is where the frozen runtime oracles live relative to this package.
// It sits under testdata beside the goldens, the fixtures the runtime tier reads.
// The tree under it mirrors the corpus layout, so an oracle sits at the case's
// path with a .txt suffix. The set of files here is the runtime-checked set: a
// case has an oracle exactly when its baseline survived the generator's screens.
const oraclesRoot = "testdata/oracles"

// runtimeLedgerPath is the committed runtime-wrong ratchet, the runtime tier's
// analogue of the accept tier's wrong section. It lists the cases whose compiled
// Go runs but disagrees with its oracle, each a bento lowering bug that compiles
// clean but computes or crashes wrong. Like the accept ledger's wrong set it may
// only shrink, and it is emptied by fixing bento, never by dropping the oracle.
const runtimeLedgerPath = "../status/runtime.txt"

// oraclePath returns the oracle file for a case id, the id keeping its source
// extension so foo.ts and foo.tsx do not collide, with .txt appended.
func oraclePath(id string) string {
	return filepath.Join(oraclesRoot, id+".txt")
}

// existingOracles returns the set of case ids that have a frozen oracle today,
// which is the set the runtime tier checks. A missing root is an empty set, the
// state before the first generation.
func existingOracles() (map[string]bool, error) {
	ids := map[string]bool{}
	err := filepath.Walk(oraclesRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		rel, err := filepath.Rel(oraclesRoot, path)
		if err != nil {
			return err
		}
		ids[strings.TrimSuffix(rel, ".txt")] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// writeOracle freezes one case's oracle, creating the parent dirs its path
// implies. It is the write half of the generator's -update-oracles path.
func writeOracle(id string, o Oracle) error {
	p := oraclePath(id)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(o.Format()), 0o644)
}

// removeOracle drops a stale oracle, used when regenerating drops a case that no
// longer survives the screens, so the oracle tree stays exactly the checked set.
func removeOracle(id string) error {
	return os.Remove(oraclePath(id))
}

// normalizeOutput folds the two differences the runtime tier does not treat as
// real: it rewrites CRLF to LF so a Windows-run oracle and a Unix-run program
// agree, and trims trailing newlines so a program that ends its last line and
// one that does not are the same. It deliberately does nothing else. In
// particular it does not touch number text: bento's value package must print a
// number exactly as Node does, so a difference there is a real mismatch the tier
// must catch, not normalize away.
func normalizeOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimRight(s, "\n")
}

// oracleMatch reports whether a compiled run matches its oracle, and on a
// mismatch the reason. Stdout is compared after normalization, exit codes
// exactly. The exception is not compared as a field: the oracle set is the clean
// exit cases, so a panic in the Go shows up as a non-zero exit, and its message
// rides along in the reason.
func oracleMatch(want, got Oracle) (bool, string) {
	if normalizeOutput(want.Stdout) != normalizeOutput(got.Stdout) {
		return false, fmt.Sprintf("stdout differs\nwant: %q\ngot:  %q", want.Stdout, got.Stdout)
	}
	if want.Exit != got.Exit {
		reason := fmt.Sprintf("exit differs: want %d, got %d", want.Exit, got.Exit)
		if got.Exception != "" {
			reason += " (" + got.Exception + ")"
		}
		return false, reason
	}
	return true, ""
}

// runtimeHeader is the comment block at the top of status/runtime.txt, so a
// reader opening the file learns what it is and how it moves without leaving it.
const runtimeHeader = `# Runtime-wrong ratchet for the ts-compat suite, tier T2.
#
# Each line is a case whose emitted Go compiles and runs but disagrees with its
# frozen oracle: wrong stdout, a wrong exit, or a panic. Every line is a bento
# lowering bug that builds clean, to be fixed by making bento emit correct Go,
# never by dropping the oracle. This set may only shrink.
#
# Regenerate with: go test ./suite -run TestRuntime -update-runtime
`

// formatRuntimeLedger renders the runtime-wrong ids as the committed ratchet
// file: the header, then one `wrong <id>` line per mismatching case, sorted by
// id so the file is stable across runs.
func formatRuntimeLedger(wrong []string) string {
	sorted := append([]string(nil), wrong...)
	sort.Strings(sorted)
	var b strings.Builder
	b.WriteString(runtimeHeader)
	for _, id := range sorted {
		fmt.Fprintf(&b, "wrong %s\n", id)
	}
	return b.String()
}

// parseRuntimeLedger reads the committed runtime ratchet into the set of known
// runtime-wrong ids. Comment and blank lines are skipped, so the header does not
// leak into the set.
func parseRuntimeLedger(content string) map[string]bool {
	known := map[string]bool{}
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "wrong" {
			known[fields[1]] = true
		}
	}
	return known
}
