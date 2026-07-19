package suite

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Oracle is the expected result of running a case's compiled Go, the ground
// truth the runtime tier checks against. Its on-disk form is the same framed
// format bento's own pkg/lower/conformance uses, so a developer who knows one
// corpus knows this one. A missing exit section defaults to 0, a missing stdout
// section to empty, and an empty exception section means the program is expected
// to run to completion.
type Oracle struct {
	Exit      int
	Stdout    string
	Exception string
}

// oracleHeader matches a section header line, `== name:` with optional
// surrounding space, capturing the section name.
var oracleHeader = regexp.MustCompile(`^==\s*([a-z]+)\s*:\s*$`)

// ParseOracle reads the framed sections of an oracle file. Section content is
// the lines between one header and the next, with a single trailing newline
// trimmed so a one-line expected value does not fight the file's own final
// newline. An unknown section name or a malformed exit value is an error, so a
// typo in an oracle surfaces as a failure rather than a silently ignored
// expectation.
func ParseOracle(content string) (Oracle, error) {
	o := Oracle{Exit: 0}
	var (
		section         string
		buf             []string
		haveExitSection bool
		rawExit         string
	)
	flush := func() error {
		if section == "" {
			return nil
		}
		body := strings.TrimSuffix(strings.Join(buf, "\n"), "\n")
		switch section {
		case "stdout":
			o.Stdout = body
		case "exception":
			o.Exception = strings.TrimSpace(body)
		case "exit":
			haveExitSection = true
			rawExit = strings.TrimSpace(body)
		default:
			return fmt.Errorf("unknown oracle section %q", section)
		}
		return nil
	}
	for line := range strings.SplitSeq(content, "\n") {
		if m := oracleHeader.FindStringSubmatch(line); m != nil {
			if err := flush(); err != nil {
				return Oracle{}, err
			}
			section = m[1]
			buf = buf[:0]
			continue
		}
		if section != "" {
			buf = append(buf, line)
		}
	}
	if err := flush(); err != nil {
		return Oracle{}, err
	}
	if haveExitSection {
		code, err := strconv.Atoi(rawExit)
		if err != nil {
			return Oracle{}, fmt.Errorf("exit section is not an integer: %q", rawExit)
		}
		o.Exit = code
	}
	return o, nil
}

// Format renders an Oracle back to the framed on-disk form, the inverse of
// ParseOracle, used by the offline oracle generator to write oracles/<id>.txt.
func (o Oracle) Format() string {
	var b strings.Builder
	b.WriteString("== stdout:\n")
	b.WriteString(o.Stdout)
	if !strings.HasSuffix(o.Stdout, "\n") {
		b.WriteByte('\n')
	}
	if o.Exception != "" {
		b.WriteString("== exception:\n")
		b.WriteString(o.Exception)
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "== exit:\n%d\n", o.Exit)
	return b.String()
}
