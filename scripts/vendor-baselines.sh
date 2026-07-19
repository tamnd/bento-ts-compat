#!/usr/bin/env bash
#
# vendor-baselines.sh refreshes corpus/baselines from the front-end port's
# reference baselines, the TypeScript compiler's own .js emit for each case. The
# runtime tier's oracle generator turns a case's .js baseline into the ground
# truth it runs the compiled Go against, so this is the reproducible record of
# where that ground truth comes from.
#
# Only the accepted cases' baselines are vendored, not the whole upstream set.
# The upstream reference tree is a few hundred megabytes, and the runtime tier can
# only use a baseline for a case bento accepts, so vendoring the accepted subset
# keeps the tree a few megabytes while covering everything the tier runs. This is
# a deliberate narrowing of doc 01's wholesale copy, recorded here so it is a
# choice and not a drift: re-run this after a coverage change to pick up the
# baselines of newly accepted cases. The accepted set is read off status/ledger.txt,
# a case being accepted exactly when it has no non-pass line there.
#
# The port stores its baselines flat by basename under
# testdata/baselines/reference/submodule/{compiler,conformance}, while the corpus
# mirrors the case tree, so this maps each accepted case id to its flat source and
# its mirrored destination. The corpus has no duplicate case basenames, so the
# flat-to-mirrored map is unambiguous.
#
# Usage:
#   scripts/vendor-baselines.sh <port-commit>
#
# The commit is the github.com/tamnd/typescript revision on the `baselines` line
# of corpus/PIN, the same port revision the front-end pin names.
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: scripts/vendor-baselines.sh <port-commit>" >&2
  exit 2
fi
commit="$1"

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# A sparse, blobless clone of just the two reference roots at the pinned commit,
# the same shape vendor.sh uses for the cases, keeps the fetch small.
git clone --filter=blob:none --no-checkout https://github.com/tamnd/typescript.git "$work/ts"
git -C "$work/ts" sparse-checkout init --cone
git -C "$work/ts" sparse-checkout set \
  testdata/baselines/reference/submodule/compiler \
  testdata/baselines/reference/submodule/conformance
git -C "$work/ts" checkout "$commit"

src="$work/ts/testdata/baselines/reference/submodule"
dst="$repo_root/corpus/baselines"
rm -rf "$dst"

# Map each accepted case to its baseline: accepted is any .ts or .tsx under
# corpus/cases with no non-pass line in the ledger, its source baseline is flat by
# basename under the case's top segment, and its destination mirrors the case path.
python3 - "$repo_root" "$src" "$dst" <<'PY'
import os, shutil, sys
repo, src, dst = sys.argv[1], sys.argv[2], sys.argv[3]
nonpass = set()
with open(os.path.join(repo, "status", "ledger.txt")) as f:
    for line in f:
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split(None, 1)
        if len(parts) == 2:
            nonpass.add(parts[1])
cases = os.path.join(repo, "corpus", "cases")
copied = missing = 0
for dp, _, files in os.walk(cases):
    for f in files:
        if not f.endswith((".ts", ".tsx")):
            continue
        cid = os.path.relpath(os.path.join(dp, f), cases)
        if cid in nonpass:
            continue
        seg = cid.split("/")[0]
        js = os.path.splitext(os.path.basename(cid))[0] + ".js"
        sp = os.path.join(src, seg, js)
        if not os.path.exists(sp):
            missing += 1
            continue
        dpth = os.path.join(dst, os.path.splitext(cid)[0] + ".js")
        os.makedirs(os.path.dirname(dpth), exist_ok=True)
        shutil.copy2(sp, dpth)
        copied += 1
print(f"vendored {copied} baselines, {missing} accepted cases have no .js baseline")
PY

echo "update the baselines line in corpus/PIN if this commit changed"
