#!/usr/bin/env bash
# Downloads the public test corpus into a cache outside the repository.
#
# Nothing fetched here is committed. testdata/corpus-public.json lists the
# files by SHA-256, so a test knows whether the file it finds in the cache
# is the one that was scored. Sources are pinned to a commit where the
# source has commits, and to a hash where it is a single archive.
#
#   testdata/fetch.sh            fetch the corpus that is scored
#   testdata/fetch.sh pdfjs      fetch one source (pdfjs, pdfium, govdocs)
#   testdata/fetch.sh soak       fetch the files that only the soak test reads
#
# Cache: $GOFASTPDF_CACHE, or ~/.cache/gofastpdf-corpus.
# An archive that is already in $GOFASTPDF_ARCHIVES is taken from there
# and not downloaded; its hash is checked all the same.
# Needs: git, curl, tar, unzip, sha256sum.
#
# Every program is run through "contained" (contain.sh): a time limit, a
# limit on the size of any file it writes, and no process left behind.
# Archives are checked for their unpacked size before they are unpacked,
# and work directories are removed however the script ends.
set -euo pipefail
. "$(dirname "$0")/contain.sh"

cache=${GOFASTPDF_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/gofastpdf-corpus}
case $cache in
/*/*) ;;
*)
	echo "the cache directory must be an absolute path below another directory, not '$cache'" >&2
	exit 1
	;;
esac
mkdir -p "$cache"
agent="gofastpdf-corpus-fetch"

# Work directories live beside the cache, not in the system's temporary
# directory, which on many machines is held in memory. Whatever happens,
# they are removed when the script ends.
remove_work() { find "${cache:?}" -maxdepth 1 -type d -name '.work.*' -exec rm -rf {} +; }
trap remove_work EXIT
workdir() { mktemp -d "$cache/.work.XXXXXX"; }

# replace_dir <dir>: empty a directory of the cache before refilling it.
replace_dir() {
	case $1 in
	"$cache"/?*) ;;
	*)
		echo "refusing to replace '$1': it is not inside the cache" >&2
		exit 1
		;;
	esac
	rm -rf -- "$1"
	mkdir -p "$1"
}

# The largest download is under 500 MB and the corpus unpacks to 300 MB.
max_download_mb=1024
max_unpacked_bytes=$((2 * 1024 * 1024 * 1024))

# download <url> <file>: an archive that is at hand is copied.
download() {
	local have=${GOFASTPDF_ARCHIVES:-}/${1##*/}
	if [ -n "${GOFASTPDF_ARCHIVES:-}" ] && [ -f "$have" ]; then
		cp "$have" "$2"
		return
	fi
	fetch "$@"
}
fetch() {
	contained 1800 "$max_download_mb" curl -fsSL -A "$agent" --max-time 1700 \
		--max-filesize $((max_download_mb * 1024 * 1024)) -o "$2" "$1"
}

# check_size <bytes> <what>: refuse an archive that would unpack too large.
check_size() {
	if [ "$1" -gt "$max_unpacked_bytes" ]; then
		echo "$2 would unpack to $1 bytes, more than the limit of $max_unpacked_bytes" >&2
		exit 1
	fi
}

# Mozilla pdf.js regression files: about a thousand small PDFs, each added
# for a parser or rendering bug. Good for robustness, not typical documents.
# Terms: the pdf.js repository is Apache-2.0; its test files come from bug
# reports and are of mixed origin. Used for testing only, not redistributed.
pdfjs_commit=74b5f3eed80e1449356758c6153f7de788bc5c47
fetch_pdfjs() {
	local dest=$cache/pdfjs
	[ -f "$dest/.done-$pdfjs_commit" ] && return
	local work
	work=$(workdir)
	git -C "$work" init -q
	git -C "$work" remote add origin https://github.com/mozilla/pdf.js
	git -C "$work" sparse-checkout set --no-cone 'test/pdfs/*.pdf'
	contained 1800 "$max_download_mb" git -C "$work" fetch -q --depth 1 --filter=blob:none origin "$pdfjs_commit"
	contained 1800 "$max_download_mb" git -C "$work" checkout -q FETCH_HEAD
	check_size "$(du -sb "$work/test/pdfs" | cut -f1)" "the pdf.js test files"
	replace_dir "$dest"
	cp "$work"/test/pdfs/*.pdf "$dest"/
	touch "$dest/.done-$pdfjs_commit"
}

# PDFium test resources: forms, annotations, encryption, damaged files.
# Terms: BSD-3-Clause (PDFium). Used for testing only, not redistributed.
# The archive is generated per request and its bytes are not stable, so
# it is pinned by commit; each file's hash is in the corpus manifest.
pdfium_commit=68f43efa71ac66ba843ba286bb76d075aa1c49df
fetch_pdfium() {
	local dest=$cache/pdfium
	[ -f "$dest/.done-$pdfium_commit" ] && return
	local work
	work=$(workdir)
	download "https://pdfium.googlesource.com/pdfium/+archive/$pdfium_commit/testing/resources.tar.gz" "$work/resources.tar.gz"
	check_size "$(tar -tzvf "$work/resources.tar.gz" | awk '{ s += $3 } END { print s + 0 }')" "the PDFium archive"
	mkdir "$work/x"
	contained 600 512 tar -xzf "$work/resources.tar.gz" -C "$work/x"
	replace_dir "$dest"
	# Flatten: directory separators become double underscores.
	(cd "$work/x" && find . -type f -iname '*.pdf' -print0) | while IFS= read -r -d '' f; do
		f=${f#./}
		cp "$work/x/$f" "$dest/${f//\//__}"
	done
	touch "$dest/.done-$pdfium_commit"
}

# GovDocs1, thread 000: about a thousand files collected from .gov sites,
# of which roughly a quarter are PDF. Ordinary documents from many
# producers: reports, forms, slides, scans.
# Terms: public documents of United States government sites, published by
# Digital Corpora for research. Used for testing only, not redistributed.
govdocs_sha256=7fb4673ac1905a3ed16a9c5780a70c729c3abc575b06c155b7042b91fa5946aa
fetch_govdocs() { govdocs_thread 000 "$govdocs_sha256" "$cache/govdocs1-000"; }

# govdocs_thread <number> <sha256 of the archive> <directory>
govdocs_thread() {
	local dest=$3
	[ -f "$dest/.done" ] && return
	local work
	work=$(workdir)
	download "https://downloads.digitalcorpora.org/corpora/files/govdocs1/zipfiles/$1.zip" "$work/$1.zip"
	echo "$2  $work/$1.zip" | sha256sum --check --quiet
	check_size "$(unzip -Zt "$work/$1.zip" | awk '{ print $3 }')" "the GovDocs archive $1"
	replace_dir "$dest"
	contained 600 512 unzip -q -j "$work/$1.zip" '*.pdf' -d "$dest"
	# The archive is large and there may be more to come: remove it now
	# and not when the script ends.
	case $work in
	"$cache"/.work.*) rm -rf -- "$work" ;;
	esac
	touch "$dest/.done"
}

# Further threads of GovDocs1, for the soak test alone (soak_test.go).
# Their files are read, each under a time limit and a memory ceiling, and
# are never scored: they are in directories named soak-*, which the
# scored corpus leaves out. About 230 PDFs each.
soak_threads="
001 bb3d9c7a68c00b5da108a5614efd2922b53604a6824324f6ab392282d786e384
002 ebfa5ad237ef8271b92ba043470003ea63464b80f481c1b87e8cc27dfc63de91
003 d51fa1f5a9182639211609e16dd746d36dec9268068cdf277f0741914456bddb
004 a9a217af7e8069ccb5f4f9df4ee97fb4479e4e880602bdfc91a8003065302a23
005 14e83d59c359f4e9fce42a954d5f106993c38f1058070b9f69e7c4b9c5e74a66
006 2f3807411eacfe710c2a13d380c71524266d8a1e98ade13862542cb1174ba342
"
fetch_soak() {
	local n sum
	while read -r n sum; do
		if [ -n "$n" ]; then
			govdocs_thread "$n" "$sum" "$cache/soak-govdocs1-$n"
		fi
	done <<<"$soak_threads"
}

sources=("$@")
[ ${#sources[@]} -eq 0 ] && sources=(pdfjs pdfium govdocs)
for s in "${sources[@]}"; do
	echo "fetching $s into $cache" >&2
	"fetch_$s"
done
for d in "$cache"/*/; do
	printf '%6d files  %s\n' "$(find "$d" -maxdepth 1 -iname '*.pdf' | wc -l)" "$d"
done
