#!/usr/bin/env bash
# Runs the Go benchmarks and writes the result, with the machine and
# toolchain it was measured on, to the file given (default: stdout).
#
#   bench/run.sh bench/baseline.txt     record a baseline
#   bench/run.sh /tmp/new.txt           measure a change, then compare with
#                                       benchstat or by eye
#
# The numbers in bench/baseline.txt are those of v0.1.1 and are what
# Phase 1 of IMPLEMENTATION_PLAN.md is measured against.
set -euo pipefail
cd "$(dirname "$0")/.."
out=${1:-/dev/stdout}
count=${BENCH_COUNT:-5}
{
	echo "# commit: $(git rev-parse --short HEAD)$(git diff --quiet HEAD -- pkg cmd 2>/dev/null || echo ' (library code modified)')"
	echo "# date: $(date -u +%Y-%m-%d)"
	echo "# $(go version)"
	echo "# cpu: $(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2- | sed 's/^ //' || sysctl -n machdep.cpu.brand_string)"
	echo "# count: $count"
	go test -run '^$' -bench . -benchmem -count "$count" . ./pkg/pdf | grep -E '^(Benchmark|goos|goarch|pkg|cpu)'
} >"$out"
