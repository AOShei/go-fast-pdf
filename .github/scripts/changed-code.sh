#!/usr/bin/env bash
# Says whether a change touches anything but documents: prints "true" or
# "false". A pull request that changes only documents and benchmark
# records (a release: the changelog, the README, bench/vX.txt, bench/BASE)
# sits on code that passed every check as the change before it, and the
# tests and the fuzzing need not run a second time on the same code.
#
#   changed-code.sh <base> <head>
#
# Anything that is not known to be a document is code, and so is a change
# whose files cannot be listed: when in doubt, everything runs.
set -uo pipefail
base=${1:-} head=${2:-HEAD}
if [ -z "$base" ] || ! git cat-file -e "$base^{commit}" 2>/dev/null || ! git cat-file -e "$head^{commit}" 2>/dev/null; then
	echo true
	exit 0
fi
if ! files=$(git diff --name-only "$base" "$head" 2>/dev/null) || [ -z "$files" ]; then
	echo true
	exit 0
fi
while IFS= read -r f; do
	case $f in
	*.md | bench/*.txt | bench/BASE | LICENSE | NOTICE) ;;
	*)
		echo true
		exit 0
		;;
	esac
done <<<"$files"
echo false
