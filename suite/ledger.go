package suite

import (
	"fmt"
	"sort"
	"strings"
)

// ledgerHeader is the comment block at the top of status/ledger.txt. It names the
// file's shape and the one command that regenerates it, so the ledger is
// self-documenting and no one hand-edits it.
const ledgerHeader = "# ts-compat status ledger.\n" +
	"# One \"<status> <case id>\" line per non-passing case.\n" +
	"# Regenerate with `go test ./suite -run TestLedger -update-ledger`.\n"

// FormatLedger renders the non-passing classifications as the committed ledger.
// A pass is not listed: the ledger is the complement of the pass set, so it
// shrinks as coverage grows and its length is the non-passing count at a glance.
// Lines are sorted by case id so the file is stable across runs and a change is a
// clean diff, and the status is padded to a fixed column so the ids line up. A
// wrong line must never reach a committed ledger, but it is rendered when present
// so a regression shows up as exactly the line that changed.
func FormatLedger(results map[string]Result) string {
	type line struct{ id, status string }
	var lines []line
	for id, r := range results {
		if r.Outcome == Pass {
			continue
		}
		lines = append(lines, line{id: id, status: r.Outcome.String()})
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].id < lines[j].id })

	var b strings.Builder
	b.WriteString(ledgerHeader)
	for _, l := range lines {
		fmt.Fprintf(&b, "%-9s %s\n", l.status, l.id)
	}
	return b.String()
}

// LedgerCounts is the summary the baseline reports: how the corpus classified.
type LedgerCounts struct {
	Total    int
	Pass     int
	Handback int
	Wrong    int
}

// Count tallies a classification map into the baseline summary.
func Count(results map[string]Result) LedgerCounts {
	c := LedgerCounts{Total: len(results)}
	for _, r := range results {
		switch r.Outcome {
		case Pass:
			c.Pass++
		case Handback:
			c.Handback++
		case Wrong:
			c.Wrong++
		}
	}
	return c
}

// String renders the summary for a test log and the baseline record.
func (c LedgerCounts) String() string {
	frac := 0.0
	if c.Total > 0 {
		frac = float64(c.Pass) / float64(c.Total)
	}
	return fmt.Sprintf("%d cases: %d pass, %d handback, %d wrong (accept fraction %.3f)",
		c.Total, c.Pass, c.Handback, c.Wrong, frac)
}
