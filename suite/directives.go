package suite

import (
	"regexp"
	"strings"
)

// directiveLine matches a TypeScript test option comment, `// @key: value`, as
// the upstream test harness does. The key is a word, the value is the rest of
// the line. The whole file is scanned for these, not just the header, because
// the corpus places options at the top by convention but the format does not
// require it, and `// @filename:` markers that split a case appear mid-file.
var directiveLine = regexp.MustCompile(`(?i)^//\s*@(\w+)\s*:\s*(.*?)\s*$`)

// Directives is the parsed set of `// @key: value` options on a case, keys
// lowercased. The filename directive is not stored here: it drives the file
// split in multifile.go, not the option map.
type Directives map[string]string

// ParseDirectives scans source for every option directive and returns them as a
// map with lowercased keys. A later occurrence of a key wins, matching the way
// the upstream harness lets a case override an earlier setting. An unrecognized
// key is kept, not rejected, so a case is never run under a silently dropped
// option and a new upstream directive does not break discovery.
func ParseDirectives(source string) Directives {
	d := Directives{}
	for line := range strings.SplitSeq(source, "\n") {
		m := directiveLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		key := strings.ToLower(m[1])
		if key == "filename" {
			continue
		}
		d[key] = m[2]
	}
	return d
}

// Bool reports whether a directive is present and set to a truthy value. The
// upstream format treats `true` as the on value for a boolean option, and a
// bare presence is not enough, so `@declaration: false` reads as off.
func (d Directives) Bool(key string) bool {
	v, ok := d[strings.ToLower(key)]
	return ok && strings.EqualFold(strings.TrimSpace(v), "true")
}

// Get returns a directive value and whether it was present.
func (d Directives) Get(key string) (string, bool) {
	v, ok := d[strings.ToLower(key)]
	return v, ok
}
