#!/usr/bin/env bash
# Fails if the module depends on anything outside the standard library and
# the go-fast modules (IMPLEMENTATION_PLAN.md section 4.4).
set -euo pipefail
module=$(go list -m)
allowed='^github\.com/AOShei/go-fast-[a-z]+(/|$)'

bad=$(go list -deps -test -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./... |
	grep -v "^$module" | grep -Ev "$allowed" | grep -v '\.test$' | sort -u || true)
if [ -n "$bad" ]; then
	echo "packages outside the standard library and go-fast modules:" >&2
	echo "$bad" >&2
	exit 1
fi

mods=$(go list -m all | awk '{print $1}' | grep -v "^$module\$" | grep -Ev "$allowed" || true)
if [ -n "$mods" ]; then
	echo "go.mod requires modules other than go-fast modules:" >&2
	echo "$mods" >&2
	exit 1
fi
echo "dependencies: standard library and go-fast modules only"
