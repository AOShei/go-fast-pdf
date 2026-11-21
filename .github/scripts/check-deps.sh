#!/usr/bin/env bash
# Fails if the module depends on anything outside the standard library and
# go-fast-core, the one module the libraries of the series share.
set -euo pipefail
module=$(go list -m)
allowed='^github\.com/AOShei/go-fast-core(/|$)'

bad=$(go list -deps -test -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./... |
	grep -v "^$module" | grep -Ev "$allowed" | grep -v '\.test$' | sort -u || true)
if [ -n "$bad" ]; then
	echo "packages outside the standard library and go-fast-core:" >&2
	echo "$bad" >&2
	exit 1
fi

mods=$(go list -m all | awk '{print $1}' | grep -v "^$module\$" | grep -Ev "$allowed" || true)
if [ -n "$mods" ]; then
	echo "go.mod requires modules other than go-fast-core:" >&2
	echo "$mods" >&2
	exit 1
fi
echo "dependencies: standard library and go-fast-core only"
