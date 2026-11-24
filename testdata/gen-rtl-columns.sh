#!/usr/bin/env bash
# Generates the fixtures of the tests of columns of text written from
# right to left, testdata/fixtures/rtl-*-columns-*.pdf, from the texts of
# gen-rtl.sh (testdata/src/rtl-he.txt, rtl-ar.txt and rtl-mixed.txt): the
# first paragraph a heading across the page, the others in two columns,
# the first column on the right; for the English with Hebrew and Arabic
# in it, on the left. LibreOffice sets them (its <multicol>, as a section
# of two columns), and Firefox the Hebrew and the Arabic too (CSS
# columns; the Arabic in Noto Sans Arabic). rtl_fixtures_test.go reads
# them in the order the source is written.
#
# The output is committed. Run this only to add a fixture or after
# changing a source text.
#
# Needs: libreoffice, firefox, python3, and the fonts of gen-rtl.sh.
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

# page <source> <lang> <dir> <html name> <columns: multicol or css> [<font>]
page() {
	local fonts=${6:-'"Noto Sans", "Noto Sans Hebrew", "Noto Sans Arabic", sans-serif'}
	{
		printf '<!doctype html>\n<html lang="%s" dir="%s"><head><meta charset="utf-8"><title>%s</title>\n<style>\n' "$2" "$3" "$4"
		printf 'body { font-family: %s; font-size: 12pt; line-height: 1.4; margin: 0 }\n' "$fonts"
		printf 'h1 { font-size: 16pt; margin: 0 0 12pt 0 }\np { margin: 0 0 9pt 0; break-inside: avoid }\n'
		printf '.cols { column-count: 2; column-gap: 24pt }\n</style></head><body>\n'
		awk -v m="$5" 'BEGIN { RS = ""; FS = "\n" }
			NR == 1 { print "<h1>" $0 "</h1>"; print (m == "multicol" ? "<multicol cols=\"2\" gutter=\"32\">" : "<div class=\"cols\">"); next }
			{ print "<p>" $0 "</p>" }
			END { print (m == "multicol" ? "</multicol>" : "</div>") }' "src/$1.txt"
		printf '</body></html>\n'
	} >"$tmp/$4.html"
}
for spec in "rtl-he he rtl" "rtl-ar ar rtl" "rtl-mixed en ltr"; do
	set -- $spec
	page "$1" "$2" "$3" "$1-columns" multicol
	contained 300 256 soffice --headless --norestore -env:UserInstallation="file://$tmp/lo-profile" \
		--convert-to pdf --outdir "$tmp" "$tmp/$1-columns.html" >/dev/null
	cp "$tmp/$1-columns.pdf" "$out/$1-columns-libreoffice.pdf"
done
page rtl-he he rtl rtl-he-columns-ff css
contained 180 512 python3 -I ./browser-print.py "$tmp/rtl-he-columns-ff.html" "$out/rtl-he-columns-firefox.pdf" "$tmp/firefox-he"
page rtl-ar ar rtl rtl-ar-columns-ff css '"Noto Sans Arabic", sans-serif'
contained 180 512 python3 -I ./browser-print.py "$tmp/rtl-ar-columns-ff.html" "$out/rtl-ar-columns-firefox.pdf" "$tmp/firefox-ar"

(cd "$out" && sha256sum -- *.pdf >MANIFEST.sha256)
ls -l "$out"/rtl-*-columns-*
