#!/usr/bin/env bash
# Prints the fuzz targets of .github/fuzz-targets.txt as the JSON list
# that a workflow's matrix takes: [{"pkg":"./internal/syntax","target":"FuzzParser"},...]
set -euo pipefail
cd "$(dirname "$0")/../.."
{ grep -Ev '^(#|[[:space:]]*$)' .github/fuzz-targets.txt || true; } |
	awk 'BEGIN { printf "[" } { printf "%s{\"pkg\":\"%s\",\"target\":\"%s\"}", (n++ ? "," : ""), $1, $2 } END { printf "]\n" }'
