package suite

import (
	"os"
	"path/filepath"
	"strings"
)

// goldensRoot is where the frozen emit goldens live relative to this package. The
// tree under it mirrors the corpus layout, so a golden sits at the case's own path
// with a .go suffix and a reviewer reads the two side by side.
const goldensRoot = "../goldens"

// goldenPath returns the golden file for a case id. The id keeps its source
// extension, so foo.ts and foo.tsx never collide, and .go is appended so the file
// is Go the tooling skips on sight of its generated header.
func goldenPath(id string) string {
	return filepath.Join(goldensRoot, id+".go")
}

// existingGoldens returns the set of case ids that have a committed golden today.
// A golden is <id>.go under the root, so the id is its path beneath the root with
// the .go trimmed. The emit tier uses this to find a golden left behind by a case
// that no longer accepts, an orphan the goldens tree must not keep. A missing root
// is an empty set, the state before the first freeze.
func existingGoldens() (map[string]bool, error) {
	ids := map[string]bool{}
	err := filepath.Walk(goldensRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(goldensRoot, path)
		if err != nil {
			return err
		}
		ids[strings.TrimSuffix(rel, ".go")] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// writeGolden freezes one accepted case's emitted Go, creating the parent dirs the
// case's path implies. It is the write half of the emit tier's -update path.
func writeGolden(id, goSrc string) error {
	p := goldenPath(id)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(goSrc), 0o644)
}
