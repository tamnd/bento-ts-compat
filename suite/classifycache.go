package suite

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// classifyCache memoizes the whole-corpus T0 judgement on disk so a re-run only
// re-emits and re-builds the cases whose input actually changed. Classifying the
// corpus is a checker and, on a pass, a go build per case over twelve thousand
// cases, minutes of work that is identical run to run when neither bento nor the
// case moved. The cache turns the second and later runs into a content-hash
// lookup for the unchanged majority, so iterating on a bento fix reclassifies
// only the handful of cases the fix touched plus whatever the run selects fresh.
//
// Correctness rests on two keys. The fingerprint keys the whole cache on the
// toolchain that produced the verdicts: the test binary (every linked change to
// the suite logic or bento's lowerer), the module's go.sum (every dependency
// re-pin, which is how a bento fix reaches here and how the value runtime the
// build-check links moves), and the Go version (the compiler the build-check
// runs). Any of these changing lands the run in a fresh fingerprint directory, so
// a stale verdict from an older toolchain is never read. The per-case key is the
// case file's content hash, so editing a case reclassifies exactly that case.
//
// A miss falls through to the live judgement and writes the verdict back, so the
// cache fills as the corpus is classified and a partial run seeds the entries a
// later full run reuses. The cache is advisory: any read or write error degrades
// to computing live, never to a wrong verdict.
type classifyCache struct {
	dir     string
	enabled bool
}

// cacheEntry is the on-disk form of a Result. It carries the same three fields,
// gob-encoded, so a hit reconstructs the exact verdict a live classification
// would have returned, including the emitted Go the golden and runtime tiers read.
type cacheEntry struct {
	Outcome int
	Go      string
	Reason  string
}

// cacheRootName is the directory under the OS temp dir that holds every
// fingerprint's cache. It sits beside the build cache TestMain manages, kept out
// of the module tree so it is never discovered as a case or committed.
const cacheRootName = "bento-ts-compat-classify-cache"

var (
	cacheOnce   sync.Once
	sharedCache *classifyCache
)

// newClassifyCache opens the cache for the current toolchain, computing the
// fingerprint once per process and pruning sibling fingerprint directories so a
// toolchain change does not leave the old run's entries growing on disk without
// bound. Setting BENTO_TS_COMPAT_NO_CACHE disables it, the clean-room switch for
// a run that must recompute every verdict from scratch. Any failure to establish
// the cache directory disables the cache rather than failing the run.
func newClassifyCache(moduleRoot string) *classifyCache {
	cacheOnce.Do(func() {
		sharedCache = openClassifyCache(moduleRoot)
	})
	return sharedCache
}

func openClassifyCache(moduleRoot string) *classifyCache {
	if os.Getenv("BENTO_TS_COMPAT_NO_CACHE") != "" {
		return &classifyCache{enabled: false}
	}
	fp, err := toolchainFingerprint(moduleRoot)
	if err != nil {
		return &classifyCache{enabled: false}
	}
	root := filepath.Join(os.TempDir(), cacheRootName)
	dir := filepath.Join(root, fp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &classifyCache{enabled: false}
	}
	pruneStaleFingerprints(root, fp)
	return &classifyCache{dir: dir, enabled: true}
}

// toolchainFingerprint hashes the inputs that decide a verdict: the running test
// binary, the module's go.sum, and the Go version. The binary covers the suite's
// own classification logic and bento's lowerer, both linked in; go.sum covers
// every pinned dependency including the value runtime the emitted Go links at
// build-check time; the Go version covers the compiler that build-check invokes.
// The result is a short hex prefix used as the cache subdirectory name.
func toolchainFingerprint(moduleRoot string) (string, error) {
	h := sha256.New()
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exeBytes, err := os.ReadFile(exe)
	if err != nil {
		return "", err
	}
	h.Write(exeBytes)
	// go.sum may be absent in an unusual layout; its absence is not fatal, the
	// binary hash still keys the cache on the linked lowerer.
	if sum, err := os.ReadFile(filepath.Join(moduleRoot, "..", "go.sum")); err == nil {
		h.Write(sum)
	} else if sum, err := os.ReadFile(filepath.Join(moduleRoot, "go.sum")); err == nil {
		h.Write(sum)
	}
	h.Write([]byte(runtime.Version()))
	return hex.EncodeToString(h.Sum(nil))[:24], nil
}

// pruneStaleFingerprints removes every fingerprint directory under root except
// the current one, so a toolchain change reclaims the previous run's entries
// instead of accumulating a directory per bento revision ever built. A prune
// error is ignored: the cache still works, it just keeps the stale bytes.
func pruneStaleFingerprints(root, keep string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != keep {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

// caseKey hashes a case file's contents into the per-case cache key, so the same
// bytes hit the same entry and an edit misses. A read failure returns ok=false,
// which the caller reads as a cache bypass for that case, computing it live.
func caseKey(file string) (string, bool) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), true
}

// get returns the cached verdict for a case and whether one was present. A
// decode failure is treated as a miss so a corrupt or half-written entry is
// simply recomputed and overwritten, never read as a bad verdict.
func (c *classifyCache) get(file string) (Result, bool) {
	if c == nil || !c.enabled {
		return Result{}, false
	}
	key, ok := caseKey(file)
	if !ok {
		return Result{}, false
	}
	data, err := os.ReadFile(filepath.Join(c.dir, key+".gob"))
	if err != nil {
		return Result{}, false
	}
	var e cacheEntry
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&e); err != nil {
		return Result{}, false
	}
	return Result{Outcome: Outcome(e.Outcome), Go: e.Go, Reason: e.Reason}, true
}

// put writes a verdict for a case. The write is atomic, a temp file renamed into
// place, so a concurrent reader never sees a half-written entry and two workers
// that hash to the same key (identical case content) race only on the rename,
// which is idempotent. A write error is ignored: the verdict was already computed
// live, the cache just misses it next time.
func (c *classifyCache) put(file string, r Result) {
	if c == nil || !c.enabled {
		return
	}
	key, ok := caseKey(file)
	if !ok {
		return
	}
	var buf bytes.Buffer
	e := cacheEntry{Outcome: int(r.Outcome), Go: r.Go, Reason: r.Reason}
	if err := gob.NewEncoder(&buf).Encode(e); err != nil {
		return
	}
	tmp, err := os.CreateTemp(c.dir, key+".*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, filepath.Join(c.dir, key+".gob")); err != nil {
		_ = os.Remove(tmpName)
	}
}

// classifyBuiltCached is classifyBuilt with the disk cache in front: a hit
// returns the frozen verdict, a miss classifies live and writes the verdict back.
// It is the single chokepoint the corpus driver calls, so every tier that shares
// the whole-corpus classification shares the cache.
func classifyBuiltCached(cache *classifyCache, moduleRoot string, c Case) Result {
	if r, ok := cache.get(c.File); ok {
		return r
	}
	r := classifyBuilt(moduleRoot, c)
	cache.put(c.File, r)
	return r
}
