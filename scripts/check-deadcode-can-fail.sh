#!/usr/bin/env bash
# Proves `make lint-deadcode` can fail (ADR-0094, .github#160).
#
# A guard that cannot fail is worse than none. This script runs the gate on the
# real tree, which must pass and print its count. It then copies the tracked
# tree twice. The first copy gets one function nothing calls, and the second
# gets one allowlist entry that names nothing. Each run must exit non-zero and
# name the symbol, so a gate that read no code, or one that tolerates a stale
# exemption, is caught here and not on main.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

gate() {
	# $1: module directory. Prints the gate output; the exit code is the gate's.
	make -s -C "$1" lint-deadcode 2>&1
}

count_of() {
	sed -n 's/^deadcode: \([0-9][0-9]*\) unreachable.*/\1/p' <<<"$1" | head -n1
}

real_out="$(gate "$ROOT")" || {
	printf '%s\n' "$real_out"
	echo "FAIL: the real tree does not pass the gate" >&2
	exit 1
}
real_count="$(count_of "$real_out")"
if [ -z "$real_count" ]; then
	printf '%s\n' "$real_out"
	echo "FAIL: the gate passed the real tree but printed no count" >&2
	exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

copy_tree() {
	# $1: destination. Copies the tracked tree only, plus the allowlist under
	# test, which is the one in the working tree, committed or not.
	mkdir -p "$1"
	git -C "$ROOT" ls-files -z | tar --null -C "$ROOT" -cf - --files-from=- | tar -C "$1" -xf -
	mkdir -p "$1/scripts"
	cp "$ROOT/scripts/deadcode-allow.txt" "$1/scripts/deadcode-allow.txt"
}

# 1. A function nothing calls fails, and is named.
copy_tree "$tmp/planted"
cat > "$tmp/planted/unwired/zz_deadcode_probe.go" <<'GO'
package unwired

// deadcodeProbe is called by nothing. The selftest expects the gate to report it.
func deadcodeProbe() string { return "unreachable" }
GO
set +e
out="$(gate "$tmp/planted")"
rc=$?
set -e
printf '%s\n' "$out"
if [ "$rc" -eq 0 ]; then
	echo "FAIL: the gate accepted a function nothing calls (exit 0)" >&2
	exit 1
fi
if ! grep -q 'unreachable func: deadcodeProbe' <<<"$out"; then
	echo "FAIL: the gate failed but did not name deadcodeProbe" >&2
	exit 1
fi

# 2. An allowlist entry that names nothing fails, and is named.
copy_tree "$tmp/stale"
printf '# planted by the selftest\nGoneLongAgo\n' >>"$tmp/stale/scripts/deadcode-allow.txt"
set +e
out="$(gate "$tmp/stale")"
rc=$?
set -e
printf '%s\n' "$out"
if [ "$rc" -eq 0 ]; then
	echo "FAIL: the gate accepted an allowlist entry that names nothing (exit 0)" >&2
	exit 1
fi
if ! grep -q 'GoneLongAgo' <<<"$out"; then
	echo "FAIL: the gate failed but did not name the stale entry GoneLongAgo" >&2
	exit 1
fi

echo "ok  lint-deadcode can fail: a planted function and a stale entry each fail (real tree: ${real_count} unreachable, all allowlisted)"
