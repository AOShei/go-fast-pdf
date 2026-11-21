#!/usr/bin/env bash
# Runs the Go benchmarks and writes the result, with the machine and
# toolchain it was measured on, to the file given (default: stdout).
#
#   bench/run.sh bench/baseline.txt     record a baseline
#   bench/run.sh /tmp/new.txt           measure a change, then compare with
#                                       benchstat or by eye
#
# bench/baseline.txt holds the numbers of v0.1.1. Each phase that changes
# performance records its own file beside it (bench/phase1.txt, ...).
set -euo pipefail
cd "$(dirname "$0")/.."
out=${1:-/dev/stdout}
count=${BENCH_COUNT:-5}
{
	echo "# commit: $(git rev-parse --short HEAD)$(git diff --quiet HEAD -- ':(glob)*.go' internal cmd go.mod go.sum 2>/dev/null || echo ' (library code modified)')"
	echo "# date: $(date -u +%Y-%m-%d)"
	echo "# $(go version)"
	echo "# cpu: $(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2- | sed 's/^ //' || sysctl -n machdep.cpu.brand_string)"
	echo "# count: $count"
	go test -run '^$' -bench . -benchmem -count "$count" ./... | grep -E '^(Benchmark|goos|goarch|pkg|cpu)'
} >"$out"
