#!/usr/bin/env bash
#
# vendor-baselines.sh refreshes corpus/baselines from the front-end port's
# reference baselines, the TypeScript compiler's own baselines for each case. It
# vendors two flavors, each the ground truth of one tier:
#
#   .js         the compiler's emitted JavaScript, which the runtime tier's
#               oracle generator turns into the output a case's compiled Go is
#               checked against (tier T2).
#   .errors.txt the compiler's diagnostics, present only for a case TypeScript
#               rejects. The diagnostics tier reads its presence to route the
#               case to T3, where bento must refuse the program too (tier T3).
#
# The two flavors have different scopes on purpose.
#
# The .js baseline is vendored only for accepted cases, not the whole upstream
# set. The upstream reference tree is a few hundred megabytes, and the runtime
# tier can only use a baseline for a case bento accepts, so vendoring the accepted
# subset keeps the tree small while covering everything the tier runs. This is a
# deliberate narrowing of doc 01's wholesale copy. The accepted set is read off
# status/ledger.txt, a case being accepted exactly when it has no non-pass line
# there.
#
# The .errors.txt baseline is vendored for every case that has one upstream,
# accepted or not, because the diagnostics tier's scope is the whole error-case
# set: a handback error case is a T3 pass that still counts toward coverage, so
# its presence must be recorded, not just the accepted ones. These are text and
# small, so the whole set is a modest addition.
#
# Re-run this after a coverage change to pick up newly accepted cases' .js
# baselines and any error baselines a corpus refresh added or dropped.
#
# The port stores its baselines flat by basename under
# testdata/baselines/reference/submodule/{compiler,conformance}, while the corpus
# mirrors the case tree, so this maps each case id to its flat source and its
# mirrored destination. The corpus has no duplicate case basenames, so the
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

# Map each case to its baselines: the source is flat by basename under the case's
# top segment, and the destination mirrors the case path. A .js is copied only for
# an accepted case (no non-pass line in the ledger); an .errors.txt is copied for
# any case that has one upstream, accepted or not.
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
js_copied = js_missing = err_copied = 0
for dp, _, files in os.walk(cases):
    for f in files:
        if not f.endswith((".ts", ".tsx")):
            continue
        cid = os.path.relpath(os.path.join(dp, f), cases)
        seg = cid.split("/")[0]
        base = os.path.splitext(os.path.basename(cid))[0]
        stem = os.path.splitext(cid)[0]
        # .js: accepted cases only, the subset the runtime tier can run.
        if cid not in nonpass:
            sp = os.path.join(src, seg, base + ".js")
            if os.path.exists(sp):
                dpth = os.path.join(dst, stem + ".js")
                os.makedirs(os.path.dirname(dpth), exist_ok=True)
                shutil.copy2(sp, dpth)
                js_copied += 1
            else:
                js_missing += 1
        # .errors.txt: every error case, the diagnostics tier's whole scope.
        ep = os.path.join(src, seg, base + ".errors.txt")
        if os.path.exists(ep):
            dpth = os.path.join(dst, stem + ".errors.txt")
            os.makedirs(os.path.dirname(dpth), exist_ok=True)
            shutil.copy2(ep, dpth)
            err_copied += 1
print(f"vendored {js_copied} .js baselines ({js_missing} accepted cases have none), "
      f"{err_copied} .errors.txt baselines")
PY

echo "update the baselines line in corpus/PIN if this commit changed"
