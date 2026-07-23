package suite

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the build-check's go builds at a dedicated build cache kept
// apart from the developer's shared GOCACHE, and bounds it. The accept tier
// compiles a package per accepted case, and over a full corpus that is more than
// a thousand one-shot builds whose compiled packages land in the cache. On a
// machine that builds every day they never age out of Go's retention, so sweep
// after sweep leaves the cache growing without bound. A dedicated cache keeps
// bento's value package and the stdlib warm across runs so the build-check stays
// fast, and the teardown drops the whole cache once it grows past a cap, so the
// churn is bounded and the developer's own cache never sees it.
func TestMain(m *testing.M) {
	cache := filepath.Join(os.TempDir(), "bento-ts-compat-gocache")
	if err := os.Setenv("GOCACHE", cache); err != nil {
		panic(err)
	}
	code := m.Run()
	// A cache past this size is mostly the churn of one-shot accepted-case builds,
	// so drop it rather than let it grow; the next run rewarms the warm set into a
	// fresh one.
	const maxCache = 3 << 30 // 3 GiB
	if dirSize(cache) > maxCache {
		_ = os.RemoveAll(cache)
	}
	// The classification cache holds one verdict per case content, at most the whole
	// corpus once for the current toolchain, since a fingerprint change prunes the
	// prior run's directory at open. That is bounded, but a stale fingerprint left by
	// a run that was the last on its toolchain, plus the emitted Go each pass entry
	// carries, still accretes on disk. Cap the whole classify-cache root and drop it
	// once it grows past the cap, the same bounded-churn contract the build cache
	// keeps; the next run refills only the entries it actually reads.
	classifyCacheRoot := filepath.Join(os.TempDir(), cacheRootName)
	const maxClassifyCache = 512 << 20 // 512 MiB
	if dirSize(classifyCacheRoot) > maxClassifyCache {
		_ = os.RemoveAll(classifyCacheRoot)
	}
	os.Exit(code)
}

// dirSize sums the bytes of every file under root, the check TestMain uses to
// decide whether the build cache has grown past its cap. A missing root sums to
// zero.
func dirSize(root string) int64 {
	var total int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}
