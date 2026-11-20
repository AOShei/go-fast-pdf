#!/usr/bin/env bash
# Compares the speed of this tree with that of a base commit, both
# measured here and now, and fails if a benchmark got more than 10
# percent slower (IMPLEMENTATION_PLAN.md, Phase 8).
#
#   bench/compare.sh            against the commit named in bench/BASE
#   bench/compare.sh <commit>   against another one
#
# Numbers from two machines, or from one machine on two days, cannot be
# compared that closely. So the base is checked out beside this tree and
# the two are run in turns, several rounds; of each benchmark the best
# round counts, which is the one that other work on the machine
# disturbed least. Where the processor's instruction counter can be read
# (Linux, on hardware that lets it) the instructions per run are compared
# and not the time: they are the same on a busy machine.
#
# Only the benchmarks that run on one thread are compared. Those with
# workers depend on what else the machine's processors are doing.
#
# Environment: ROUNDS (default 6), BENCHTIME (default 1s), LIMIT (percent,
# default 10), OUT (default bench-compare.txt).
set -euo pipefail
cd "$(dirname "$0")/.."
base=${1:-$(grep -v '^#' bench/BASE | head -1)}
rounds=${ROUNDS:-6}
benchtime=${BENCHTIME:-1s}
limit=${LIMIT:-10}
out=${OUT:-bench-compare.txt}
pattern='^(BenchmarkExtract$|BenchmarkExtractFiles$|BenchmarkScanner$|BenchmarkResolve$|BenchmarkZlib$)'

work=$(mktemp -d)
cleanup() {
	git worktree remove --force "$work/base" >/dev/null 2>&1 || true
	case $work in
	/*/*) rm -rf -- "$work" ;;
	esac
}
trap cleanup EXIT
git worktree add --detach "$work/base" "$base" >/dev/null

run() { # run <directory> <file to append to>
	(cd "$1" && GOWORK=off go test -run '^$' -bench "$pattern" -benchtime "$benchtime" -count 1 ./... 2>/dev/null) |
		grep -E '^Benchmark' | grep -v 'workers=8\|compress-zlib' >>"$2" || true
}
: >"$work/base.txt"
: >"$work/head.txt"
for round in $(seq "$rounds"); do
	echo "round $round of $rounds" >&2
	run "$work/base" "$work/base.txt"
	run . "$work/head.txt"
done

{
	echo "# base: $(git rev-parse --short "$base")  head: $(git rev-parse --short HEAD)$(git diff --quiet HEAD 2>/dev/null || echo ' (modified)')"
	echo "# $(go version)"
	echo "# cpu: $(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2- | sed 's/^ //' || sysctl -n machdep.cpu.brand_string 2>/dev/null || echo unknown)"
	echo "# best of $rounds rounds of $benchtime each, the two trees in turns; a change of more than $limit percent for the worse fails"
	awk -v limit="$limit" '
	# The measure of a line: instructions if they are there, else
	# processor time, else time on the clock.
	function measure(   i, v, unit) {
		v = ""; unit = ""
		for (i = 3; i < NF; i += 2) {
			if ($(i + 1) == "instr/op") { return $i " instr/op" }
			if ($(i + 1) == "cpu-ns/op") { v = $i; unit = "cpu-ns/op" }
			if ($(i + 1) == "ns/op" && unit == "") { v = $i; unit = "ns/op" }
		}
		return v " " unit
	}
	{
		name = $1; sub(/-[0-9]+$/, "", name)
		split(measure(), m, " ")
		if (m[1] == "") { next }
		key = name SUBSEP m[2]
		if (FILENAME ~ /base\.txt$/) {
			if (!(key in b) || m[1] < b[key]) { b[key] = m[1] }
		} else {
			if (!(key in h) || m[1] < h[key]) { h[key] = m[1] }
			if (!(name in unit)) { unit[name] = m[2]; order[++n] = name }
		}
	}
	END {
		bad = 0
		printf "%-62s %14s %14s %8s\n", "benchmark", "base", "head", "change"
		for (i = 1; i <= n; i++) {
			name = order[i]; key = name SUBSEP unit[name]
			if (!(key in b)) { printf "%-62s %14s %14.0f %8s\n", name, "-", h[key], "new"; continue }
			change = 100 * (h[key] - b[key]) / b[key]
			flag = ""
			if (change > limit) { flag = "  SLOWER"; bad = 1 }
			printf "%-62s %14.0f %14.0f %+7.1f%%%s  %s\n", name, b[key], h[key], change, flag, unit[name]
		}
		exit bad
	}' "$work/base.txt" "$work/head.txt"
} | tee "$out"
