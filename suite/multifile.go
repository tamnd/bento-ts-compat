package suite

import (
	"regexp"
	"strings"
)

// filenameLine matches a `// @filename: name.ts` marker, which delimits a
// virtual file within a physical case file. The corpus packs several files into
// one case this way to test imports and cross-file resolution.
var filenameLine = regexp.MustCompile(`(?i)^//\s*@filename\s*:\s*(.*?)\s*$`)

// VirtualFile is one file carved out of a physical case. Name is the value of
// its `// @filename` marker, empty for the implicit default file that holds any
// content before the first marker.
type VirtualFile struct {
	Name    string
	Content string
}

// SplitFiles carves a physical case source into its virtual files on the
// `// @filename` markers. Content before the first marker is the implicit
// default file. A single-file case with no marker returns one VirtualFile with
// an empty Name holding the whole source.
func SplitFiles(source string) []VirtualFile {
	var files []VirtualFile
	var name string
	var buf strings.Builder
	flush := func() {
		files = append(files, VirtualFile{Name: name, Content: buf.String()})
		buf.Reset()
	}
	started := false
	for line := range strings.SplitSeq(source, "\n") {
		if m := filenameLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if started {
				flush()
			}
			name = m[1]
			started = true
			continue
		}
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(line)
	}
	flush()
	return files
}

// hasCode reports whether a virtual file carries anything a compiler would act
// on: a non-blank line that is not itself an option directive. A file of only
// directives, blank lines, and comments is not real code. Counting comment-only
// files as real would over-count, which only ever adds a handback, so the check
// errs toward the safe side by treating a line with any non-directive content
// as code.
func hasCode(content string) bool {
	for line := range strings.SplitSeq(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if directiveLine.MatchString(t) {
			continue
		}
		return true
	}
	return false
}

// Parsed is the normalized view of a case the tiers act on: its option
// directives and its virtual files. It is the interface between the faithful,
// judgement-free format layer and the tier layer.
type Parsed struct {
	Directives Directives
	Files      []VirtualFile
	realFiles  int
}

// Parse reads a case source into its normalized form.
func Parse(source string) Parsed {
	files := SplitFiles(source)
	real := 0
	for _, f := range files {
		if hasCode(f.Content) {
			real++
		}
	}
	return Parsed{
		Directives: ParseDirectives(source),
		Files:      files,
		realFiles:  real,
	}
}

// IsMultiFile reports whether the case defines more than one real file, so
// bento's single-entry ahead-of-time path cannot compile it as one unit and it
// routes to a handback until module lowering lands. A case with one real file,
// even one named by a single `// @filename` marker, is not multi-file: the
// marker is an inert comment and the physical file compiles as one program.
func (p Parsed) IsMultiFile() bool { return p.realFiles > 1 }
