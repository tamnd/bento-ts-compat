#!/usr/bin/env bash
#
# vendor.sh refreshes corpus/cases from the TypeScript test suite the bento front
# end pins. It is the reproducible record of how the vendored corpus was built, so
# a corpus refresh is a reviewable diff rather than an opaque directory drop.
#
# The corpus is derived, not authored. bento pins microsoft/typescript-go through
# a go.mod replace, that port pins microsoft/TypeScript as a submodule at a fixed
# commit, and the cases live in that submodule under tests/cases. This script
# clones the submodule at the pinned commit and copies the two case roots the
# suite runs, compiler and conformance, into corpus/cases. See corpus/PIN for the
# exact revisions and 01_corpus_and_vendoring.md for the rationale.
#
# Usage:
#   scripts/vendor.sh <typescript-commit>
#
# The commit is the microsoft/TypeScript revision recorded on the `cases` line of
# corpus/PIN. Passing it explicitly keeps the pin and the fetch from drifting
# apart: the value that lands in the tree is the value the caller names.
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: scripts/vendor.sh <typescript-commit>" >&2
  exit 2
fi
commit="$1"

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cases_dir="$repo_root/corpus/cases"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# A sparse, blobless clone of just the two case roots keeps the fetch small: the
# full TypeScript history and its many baselines are not needed to copy a few
# thousand .ts files at one commit.
git clone --filter=blob:none --no-checkout https://github.com/microsoft/TypeScript.git "$work/ts"
git -C "$work/ts" sparse-checkout init --cone
git -C "$work/ts" sparse-checkout set tests/cases/compiler tests/cases/conformance
git -C "$work/ts" checkout "$commit"

# Replace the vendored roots wholesale so a case deleted upstream disappears here
# too, rather than lingering as a stale file a refresh would silently keep.
rm -rf "$cases_dir/compiler" "$cases_dir/conformance"
mkdir -p "$cases_dir"
cp -R "$work/ts/tests/cases/compiler" "$cases_dir/compiler"
cp -R "$work/ts/tests/cases/conformance" "$cases_dir/conformance"

ts_count=$(find "$cases_dir" -type f \( -name '*.ts' -o -name '*.tsx' \) | wc -l | tr -d ' ')
echo "vendored $ts_count case files from microsoft/TypeScript @ $commit"
echo "update the cases line in corpus/PIN if this commit changed"
