#!/usr/bin/env bash
# Generates the fixtures of the tests of paragraphs with lines that by
# themselves read in the other direction, from the texts of gen-rtl.sh
# (testdata/src/rtl-he.txt, rtl-ar.txt and rtl-mixed.txt):
#
# rtl-*-narrow-*.pdf are the texts set narrow enough that lines of Hebrew
# and Arabic paragraphs are of Latin words alone or for the most part, a
# bracket's partner is on another line, and a line of the English ones is
# of Hebrew and a comma alone.
#
# rtl-he-between-*.pdf are the second and the third paragraph of
# rtl-he.txt with a paragraph between them that is not of either: a
# sentence of English set from left to right at the right edge (latin),
# a heading of Latin words in another font and a Hebrew word (heading),
# a heading of bold Hebrew with one Hebrew word that is not bold (bold).
#
# rtl_fixtures_test.go reads them in the order the source is written.
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
fonts='"Noto Sans", "Noto Sans Hebrew", "Noto Sans Arabic", sans-serif'

# page <html name> <lang> <dir> <width in points> <space after a paragraph> [<font>]:
# the body is read from standard input. LibreOffice takes the width of
# the page, Firefox that of the body.
page() {
	printf '<!doctype html>\n<html lang="%s" dir="%s"><head><meta charset="utf-8"><title>%s</title>\n<style>\n' "$2" "$3" "$1"
	case $1 in
	*-libreoffice) printf '@page { size: %dpt 900pt; margin: 20pt }\nbody { ' $(($4 + 40)) ;;
	*) printf 'body { width: %dpt; ' "$4" ;;
	esac
	printf 'font-family: %s; font-size: 12pt; line-height: 1.4; margin: 0 }\n' "${6:-$fonts}"
	printf 'h1 { font-size: 16pt; margin: 0 0 12pt 0 }\np { margin: 0 0 %s 0 }\n</style></head><body>\n' "$5"
	cat
	printf '</body></html>\n'
}
# print <name>
print() {
	case $1 in
	*-libreoffice)
		contained 300 256 soffice --headless --norestore -env:UserInstallation="file://$tmp/lo-profile" \
			--convert-to pdf --outdir "$tmp" "$tmp/$1.html" >/dev/null
		cp "$tmp/$1.pdf" "$out/$1.pdf"
		;;
	*) contained 180 512 python3 -I ./browser-print.py "$tmp/$1.html" "$out/$1.pdf" "$tmp/$1-profile" ;;
	esac
}
for spec in "rtl-he-narrow-firefox he rtl 290" "rtl-he-narrow-libreoffice he rtl 270" "rtl-ar-narrow-libreoffice ar rtl 210" "rtl-ar-narrow-firefox ar rtl 210" "rtl-mixed-narrow-libreoffice en ltr 290"; do
	set -- $spec
	font=
	# As in gen-rtl.sh: in Noto Sans Arabic, whose Latin words are in
	# another font.
	[ "$1" = rtl-ar-narrow-firefox ] && font='"Noto Sans Arabic", sans-serif'
	awk 'BEGIN { RS = ""; FS = "\n" } NR == 1 { print "<h1>" $0 "</h1>"; next } { print "<p>" $0 "</p>" }' "src/${1%-narrow-*}.txt" |
		page "$1" "$2" "$3" "$4" 9pt ${font:+"$font"} >"$tmp/$1.html"
	print "$1"
done
# between <name> <width> <html of the paragraph between>
between() {
	awk -v mid="$3" 'BEGIN { RS = ""; FS = "\n" } NR == 2 || NR == 3 { print "<p>" $0 "</p>" } NR == 2 { print mid }' src/rtl-he.txt |
		page "$1" he rtl "$2" 0 >"$tmp/$1.html"
	print "$1"
}
between rtl-he-between-latin-firefox 290 '<p dir="ltr" style="text-align: right">Contact the library for details.</p>'
between rtl-he-between-heading-libreoffice 170 '<p><span style="font-family: serif">PDF Tolstoy Austen Library Books</span> הספרייה</p>'
between rtl-he-between-bold-firefox 290 '<p><b>הספרייה העירונית Tolstoy</b> הספרייה</p>'

(cd "$out" && sha256sum -- *.pdf >MANIFEST.sha256)
ls -l "$out"/rtl-*-narrow-* "$out"/rtl-he-between-*
