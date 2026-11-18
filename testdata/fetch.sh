#!/usr/bin/env bash
# Downloads the public test corpus into a cache outside the repository.
#
# Nothing fetched here is committed. testdata/corpus-public.json lists the
# files by SHA-256, so a test knows whether the file it finds in the cache
# is the one that was scored. Sources are pinned to a commit where the
# source has commits.
#
#   testdata/fetch.sh            fetch everything
#   testdata/fetch.sh pdfjs      fetch one source (pdfjs, pdfium, govdocs)
#
# Cache: $GOFASTPDF_CACHE, or ~/.cache/gofastpdf-corpus.
# Needs: git, curl, tar, unzip.
set -euo pipefail

cache=${GOFASTPDF_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/gofastpdf-corpus}
mkdir -p "$cache"
agent="gofastpdf-corpus-fetch"

# Mozilla pdf.js regression files: about a thousand small PDFs, each added
# for a parser or rendering bug. Good for robustness, not typical documents.
# Terms: the pdf.js repository is Apache-2.0; its test files come from bug
# reports and are of mixed origin. Used for testing only, not redistributed.
pdfjs_commit=74b5f3eed80e1449356758c6153f7de788bc5c47
fetch_pdfjs() {
	local dest=$cache/pdfjs
	[ -f "$dest/.done-$pdfjs_commit" ] && return
	local work
	work=$(mktemp -d)
	git -C "$work" init -q
	git -C "$work" remote add origin https://github.com/mozilla/pdf.js
	git -C "$work" sparse-checkout set --no-cone 'test/pdfs/*.pdf'
	git -C "$work" fetch -q --depth 1 --filter=blob:none origin "$pdfjs_commit"
	git -C "$work" checkout -q FETCH_HEAD
	rm -rf "$dest" && mkdir -p "$dest"
	cp "$work"/test/pdfs/*.pdf "$dest"/
	rm -rf "$work"
	touch "$dest/.done-$pdfjs_commit"
}

# PDFium test resources: forms, annotations, encryption, damaged files.
# Terms: BSD-3-Clause (PDFium). Used for testing only, not redistributed.
pdfium_commit=68f43efa71ac66ba843ba286bb76d075aa1c49df
fetch_pdfium() {
	local dest=$cache/pdfium
	[ -f "$dest/.done-$pdfium_commit" ] && return
	local work
	work=$(mktemp -d)
	curl -fsSL -A "$agent" -o "$work/resources.tar.gz" \
		"https://pdfium.googlesource.com/pdfium/+archive/$pdfium_commit/testing/resources.tar.gz"
	mkdir "$work/x" && tar -xzf "$work/resources.tar.gz" -C "$work/x"
	rm -rf "$dest" && mkdir -p "$dest"
	# Flatten: directory separators become double underscores.
	(cd "$work/x" && find . -type f -iname '*.pdf' -print0) | while IFS= read -r -d '' f; do
		f=${f#./}
		cp "$work/x/$f" "$dest/${f//\//__}"
	done
	rm -rf "$work"
	touch "$dest/.done-$pdfium_commit"
}

# GovDocs1, thread 000: about a thousand files collected from .gov sites,
# of which roughly a quarter are PDF. Ordinary documents from many
# producers: reports, forms, slides, scans.
# Terms: public documents of United States government sites, published by
# Digital Corpora for research. Used for testing only, not redistributed.
fetch_govdocs() {
	local dest=$cache/govdocs1-000
	[ -f "$dest/.done" ] && return
	local work
	work=$(mktemp -d)
	curl -fsSL -A "$agent" -o "$work/000.zip" \
		"https://downloads.digitalcorpora.org/corpora/files/govdocs1/zipfiles/000.zip"
	rm -rf "$dest" && mkdir -p "$dest"
	unzip -q -j "$work/000.zip" '*.pdf' -d "$dest"
	rm -rf "$work"
	touch "$dest/.done"
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
