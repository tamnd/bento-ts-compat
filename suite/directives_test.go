package suite

import "testing"

func TestParseDirectives(t *testing.T) {
	src := "// @target: es2020\n" +
		"//@strict:true\n" +
		"// @Module: commonjs\n" +
		"const x = 1\n" +
		"// @strict: false\n"
	d := ParseDirectives(src)
	if got, _ := d.Get("target"); got != "es2020" {
		t.Errorf("target = %q, want es2020", got)
	}
	// A key is matched case-insensitively and stored lowercased.
	if got, ok := d.Get("module"); !ok || got != "commonjs" {
		t.Errorf("module = %q present=%v, want commonjs true", got, ok)
	}
	// A later occurrence of a key wins, matching the upstream override rule.
	if d.Bool("strict") {
		t.Error("strict should read as false: the later @strict: false wins")
	}
}

func TestDirectivesBool(t *testing.T) {
	d := ParseDirectives("// @declaration: true\n// @noEmit: FALSE\n")
	if !d.Bool("declaration") {
		t.Error("declaration should be true")
	}
	if d.Bool("noEmit") {
		t.Error("noEmit: FALSE should read as false")
	}
	if d.Bool("missing") {
		t.Error("an absent directive is not true")
	}
}

func TestParseDirectivesIgnoresFilename(t *testing.T) {
	// A @filename marker drives the file split, not the option map, so it is not
	// stored as a directive.
	d := ParseDirectives("// @filename: a.ts\nconst x = 1\n")
	if _, ok := d.Get("filename"); ok {
		t.Error("filename should not be stored as a directive")
	}
}
