package suite

import "testing"

func TestSplitFilesSingle(t *testing.T) {
	// A case with no @filename marker is one virtual file with an empty name
	// holding the whole source.
	files := SplitFiles("const x = 1\nconst y = 2\n")
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files[0].Name != "" {
		t.Errorf("default file name = %q, want empty", files[0].Name)
	}
}

func TestSplitFilesMulti(t *testing.T) {
	src := "// @filename: a.ts\nexport const a = 1\n" +
		"// @filename: b.ts\nimport { a } from './a'\n"
	files := SplitFiles(src)
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	if files[0].Name != "a.ts" || files[1].Name != "b.ts" {
		t.Errorf("names = %q, %q, want a.ts, b.ts", files[0].Name, files[1].Name)
	}
}

func TestIsMultiFile(t *testing.T) {
	// Two real files is multi-file.
	multi := Parse("// @filename: a.ts\nexport const a = 1\n// @filename: b.ts\nexport const b = 2\n")
	if !multi.IsMultiFile() {
		t.Error("two real files should be multi-file")
	}
	// A single @filename marker over one body is not multi-file: the marker is an
	// inert comment and the physical file compiles as one program.
	single := Parse("// @filename: only.ts\nconst x = 1\n")
	if single.IsMultiFile() {
		t.Error("one real file under a single marker is not multi-file")
	}
	// A second section that holds only directives and blank lines is not a real
	// file, so the case stays single.
	directivesOnly := Parse("// @filename: a.ts\nconst x = 1\n// @filename: b.ts\n// @strict: true\n")
	if directivesOnly.IsMultiFile() {
		t.Error("a directives-only second section is not a real file")
	}
}
