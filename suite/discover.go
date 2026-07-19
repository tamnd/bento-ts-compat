// Package suite is the bento-ts-compat harness. It discovers the vendored
// TypeScript corpus, drives each case through bento's ahead-of-time front-end
// (build.EmitGo), and judges the result across the tiers described in
// notes/Spec/2075/ts-compat. Nothing here touches the interpreted run path:
// the unit under test is the compiled Go and only the compiled Go.
package suite

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Case is one corpus entry: a single .ts or .tsx file under the cases root.
type Case struct {
	// ID is the case path relative to the cases root, in forward slashes, for
	// example "conformance/types/enum/enumBasics.ts". It is the stable identity
	// the ledger keys on, a filter matches against, and a golden or oracle is
	// named after. Using the upstream path means a corpus refresh that renames a
	// case shows up as a dropped id and a new id, the reviewable signal a refresh
	// should produce.
	ID string
	// File is the absolute path to the case file on disk.
	File string
}

// Discover walks root and returns every .ts and .tsx file as a Case, sorted by
// ID so a run is deterministic and a shard is stable. It reads no case content
// and emits nothing, so it is cheap enough to run at the top of every tier.
func Discover(root string) ([]Case, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve cases root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("read cases root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("cases root %s is not a directory", abs)
	}
	var cases []Case
	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".ts", ".tsx":
		default:
			return nil
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return err
		}
		cases = append(cases, Case{ID: filepath.ToSlash(rel), File: path})
		return nil
	}
	if err := filepath.WalkDir(abs, walk); err != nil {
		return nil, fmt.Errorf("walk cases root: %w", err)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return cases, nil
}
