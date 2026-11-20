#!/usr/bin/env bash
# Generates the fixtures of the tests of text written from right to left,
# testdata/fixtures/rtl-*.pdf, from testdata/src/rtl-*.txt: paragraphs of
# Hebrew and of Arabic, with numbers, Latin words and brackets in them,
# and English paragraphs with Hebrew and Arabic in them, each set by
# LibreOffice, and the Hebrew by Firefox. rtl_fixtures_test.go compares what the
# library reads with the source, in the order it is written.
#
# The output is committed. Run this only to add a fixture or after
# changing a source text.
#
# Needs: libreoffice, firefox, python3, and fonts for Hebrew and Arabic
# (Noto Sans Hebrew and Noto Sans Arabic were used).
set -euo pipefail
cd "$(dirname "$0")"
. ./contain.sh

out=fixtures
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
for tool in soffice firefox python3 sha256sum; do
	command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 1; }
done
export SOURCE_DATE_EPOCH=1763424000

# page <name> <lang> <dir>: the first paragraph a heading.
page() {
	{
		printf '<!doctype html>\n<html lang="%s" dir="%s"><head><meta charset="utf-8"><title>%s</title>\n<style>\n' "$2" "$3" "$1"
		printf 'body { font-family: "Noto Sans", "Noto Sans Hebrew", "Noto Sans Arabic", sans-serif; font-size: 12pt; line-height: 1.4; margin: 0 }\n'
		printf 'h1 { font-size: 16pt; margin: 0 0 12pt 0 }\np { margin: 0 0 9pt 0 }\n</style></head><body>\n'
		awk 'BEGIN { RS = ""; FS = "\n" } NR == 1 { print "<h1>" $0 "</h1>"; next } { print "<p>" $0 "</p>" }' "src/$1.txt"
		printf '</body></html>\n'
	} >"$tmp/$1.html"
}
for spec in "rtl-he he rtl" "rtl-ar ar rtl" "rtl-mixed en ltr"; do
	set -- $spec
	page "$1" "$2" "$3"
	contained 300 256 soffice --headless --norestore -env:UserInstallation="file://$tmp/lo-profile" \
		--convert-to pdf --outdir "$tmp" "$tmp/$1.html" >/dev/null
	cp "$tmp/$1.pdf" "$out/$1-libreoffice.pdf"
done
# Firefox sets Hebrew only: the font subsets it embeds for Arabic give
# most shaped glyphs no character (pdftotext reads none of them either).
contained 180 512 python3 -I ./browser-print.py "$tmp/rtl-he.html" "$out/rtl-he-firefox.pdf" "$tmp/firefox-rtl-he"

(cd "$out" && sha256sum -- *.pdf >MANIFEST.sha256)
ls -l "$out"/rtl-*
