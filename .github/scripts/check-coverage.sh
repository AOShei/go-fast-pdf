#!/usr/bin/env bash
# Fails if the tests run too small a share of the statements
# (IMPLEMENTATION_PLAN.md, Phase 8). The profile is one made with
# -coverpkg=./..., in which a statement appears once for every test
# binary; it counts as run if any of them ran it.
#
#   .github/scripts/check-coverage.sh cover.out
set -euo pipefail
profile=${1:?usage: check-coverage.sh <profile>}

awk -v module="$(go list -m)" '
BEGIN {
	# The least share, in percent: of the packages that read the file,
	# and of the whole module.
	# The plan asks for 85 and 75. These are two points under what CI
	# measures for the lowest of them, where the files of the system
	# that some tests read are not there.
	least["internal/syntax"] = 88
	least["internal/file"] = 88
	least["internal/font"] = 88
	least["all"] = 85
}
NR == 1 { next } # mode: ...
{
	block = $1
	if (!(block in stmts)) { stmts[block] = $2 }
	if ($3 > 0) { run[block] = 1 }
}
END {
	for (block in stmts) {
		pkg = block
		sub(/\/[^\/]*$/, "", pkg)       # the file name
		sub("^" module "/?", "", pkg)
		if (pkg == "") { pkg = "." }
		total[pkg] += stmts[block]; total["all"] += stmts[block]
		if (block in run) { covered[pkg] += stmts[block]; covered["all"] += stmts[block] }
	}
	bad = 0
	for (pkg in total) {
		share = 100 * covered[pkg] / total[pkg]
		note = ""
		if (pkg in least) {
			note = sprintf("  (at least %d)", least[pkg])
			if (share < least[pkg]) { note = note "  TOO LOW"; bad = 1 }
		}
		printf "%-22s %5.1f%% of %5d statements%s\n", pkg, share, total[pkg], note
	}
	exit bad
}' "$profile" | sort
