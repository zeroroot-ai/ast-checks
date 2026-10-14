#!/usr/bin/env bash
# unwired-consumers.sh: print the module directories of the first-party
# consumers of ast-checks, comma-separated, for `unwired -consumers`.
#
# ast-checks is test support: the gate tests of other repos call its API, and
# no production file does. The counter credits a consumer test as the read of
# a test-support package (unwired/count.go). Without the consumers loaded,
# every published helper reads as unwired (gibson#807 condition 7).
#
# Each consumer is a public repo, cloned shallow at main into a cache
# directory outside the repository, so the tree guards never walk it. A
# directory that already holds a clone is fetched and reset to origin/main.
#
# The consumer modules, one per line: <repo> <module directory in the repo>.
# A module that requires github.com/zeroroot-ai/ast-checks belongs here.
set -euo pipefail

dest="${UNWIRED_CONSUMERS_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/ast-checks-unwired-consumers}"

consumers='
gibson .
setec .
sdk .
adk gibson
'

mkdir -p "$dest"
out=()
while read -r repo dir; do
  [ -n "$repo" ] || continue
  clone="$dest/$repo"
  if [ -d "$clone/.git" ]; then
    git -C "$clone" fetch --quiet --depth=1 origin main
    git -C "$clone" reset --quiet --hard origin/main
  else
    git clone --quiet --depth=1 --branch main "https://github.com/zeroroot-ai/$repo.git" "$clone"
  fi
  mod="$clone/$dir"
  if [ ! -f "$mod/go.mod" ]; then
    echo "unwired-consumers: $repo/$dir has no go.mod" >&2
    exit 1
  fi
  if ! grep -q 'github.com/zeroroot-ai/ast-checks ' "$mod/go.mod"; then
    echo "unwired-consumers: $repo/$dir no longer requires ast-checks; remove it from this list" >&2
    exit 1
  fi
  out+=("$mod")
done <<< "$consumers"

(IFS=,; echo "${out[*]}")
