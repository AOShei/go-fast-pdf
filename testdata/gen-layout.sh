#!/usr/bin/env bash
# Generates the fixtures of the layout tests, testdata/fixtures/layout-*.pdf
# and attached.pdf, from testdata/src/layout.txt: one text with known
# paragraphs, set in several styles and in columns by three programs
# that make PDF in different ways. layout_fixtures_test.go compares what
# the library makes of each with the source.
#
# The output is committed. Run this only to add a fixture or after
# changing the source text; gen.sh makes the other fixtures.
#
# Needs: groff (with gropdf), libreoffice, firefox, qpdf, python3.
set -euo pipefail
cd "$(dirname "$0")"
. ./contain.sh

text=src/layout.txt
out=fixtures
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

for tool in groff soffice firefox qpdf python3 sha256sum; do
	command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 1; }
done
export SOURCE_DATE_EPOCH=1763424000

# --- as a web page -----------------------------------------------------
# page <name> <css> [wrapper class] [html before the paragraphs]
page() {
	{
		printf '<!doctype html>\n<html lang="en"><head><meta charset="utf-8"><title>How a catalogue is kept</title>\n<style>\n'
		printf 'body { font-family: serif; font-size: 11pt; line-height: 1.3; margin: 0 }\n'
		printf 'h1 { font-size: 16pt; font-weight: bold; margin: 0 0 12pt 0 }\np { margin: 0 }\n%s\n</style></head><body>\n' "$2"
		awk -v wrap="${3:-}" -v before="${4:-}" 'BEGIN { RS = ""; FS = "\n" }
			NR == 1 { print "<h1>" $0 "</h1>"; if (wrap != "") print "<div class=\"" wrap "\">"; if (before != "") print before; next }
			{ print "<p>" $0 "</p>" }
			END { if (wrap != "") print "</div>" }' "$text"
		printf '</body></html>\n'
	} >"$tmp/$1.html"
}
firefox_pdf() { # firefox_pdf <name>
	contained 180 512 python3 -I ./browser-print.py "$tmp/$1.html" "$out/layout-$1-firefox.pdf" "$tmp/firefox-$1"
}
office_pdf() { # office_pdf <name>
	contained 300 256 soffice --headless --norestore -env:UserInstallation="file://$tmp/lo-profile" \
		--convert-to pdf --outdir "$tmp" "$tmp/$1.html" >/dev/null
	cp "$tmp/$1.pdf" "$out/layout-$1-libreoffice.pdf"
}

# Three ways of setting paragraphs.
page spaced 'p { margin-bottom: 9pt; text-align: left }'
page indented 'p { text-indent: 18pt; text-align: left }'
page justified 'p { text-indent: 18pt; text-align: justify; hyphens: none }'
for style in spaced indented justified; do
	firefox_pdf "$style"
	office_pdf "$style"
done

# Columns, a title over columns, and a box beside the text.
page cols2 'p { margin-bottom: 9pt } h1 { display: none } .cols { column-count: 2; column-gap: 18pt }' cols
page cols3 'body { font-size: 9pt } p { margin-bottom: 7pt } h1 { display: none } .cols { column-count: 3; column-gap: 14pt }' cols
page title2 'p { margin-bottom: 9pt } .cols { column-count: 2; column-gap: 18pt }' cols
page sidebox 'p { margin-bottom: 9pt } .box { float: right; width: 120pt; margin: 0 0 9pt 14pt; padding: 6pt; border: 1pt solid black; font-size: 9pt }' \
	"" '<div class="box">Opening hours change during the yearly count.</div>'
for layout in cols2 cols3 title2 sidebox; do
	firefox_pdf "$layout"
done

# Headings set with their letters apart, over an ordinary paragraph.
{
	printf '<!doctype html>\n<html lang="en"><head><meta charset="utf-8"><title>Spaced headings</title>\n<style>\n'
	printf 'body { font-family: serif; font-size: 11pt; line-height: 1.3; margin: 0 }\np { margin: 0 0 9pt 0 }\n'
	printf 'h1 { font-size: 16pt; letter-spacing: 0.4em; margin: 0 0 12pt 0 }\nh2 { font-size: 12pt; letter-spacing: 0.25em; margin: 0 0 9pt 0 }\n'
	printf '</style></head><body>\n<h1>CONTENTS</h1>\n<h2>ANNUAL REPORT</h2>\n'
	awk 'BEGIN { RS = ""; FS = "\n" } NR == 2 { print "<p>" $0 "</p>" }' "$text"
	printf '<h2>Summary of the year</h2>\n'
	awk 'BEGIN { RS = ""; FS = "\n" } NR == 3 { print "<p>" $0 "</p>" }' "$text"
	printf '</body></html>\n'
} >"$tmp/tracking.html"
firefox_pdf tracking
office_pdf tracking

# --- through groff -----------------------------------------------------
# ms <name> <requests before the text> <paragraph macro> [request after the title]
ms() {
	{
		printf '.nr PS 11\n.nr VS 14\n.nr HY 0\n.nh\n.ds CH\n%s\n' "$2"
		awk -v para="$3" -v after="${4:-}" 'BEGIN { RS = ""; FS = "\n" }
			NR == 1 { if (after != "skip") { print ".LP"; print "\\s[16]\\fB" $0 "\\fP\\s0" } if (after != "" && after != "skip") print after; next }
			{ print para; for (i = 1; i <= NF; i++) print $i }' "$text"
	} >"$tmp/$1.ms"
	contained 120 64 groff -k -ms -Tpdf "$tmp/$1.ms" >"$out/layout-$1-groff.pdf"
}
ms spaced '.na
.nr PD 0.6v' .LP
ms indented '.na
.nr PD 0
.nr PI 18p' .PP
ms justified '.ad b
.nr PD 0
.nr PI 18p' .PP
ms cols2 '.na
.nr PD 0.6v
.2C' .LP skip
ms cols3 '.na
.nr PS 9
.nr VS 11
.nr PD 0.6v
.MC 1.9i' .LP skip
ms title2 '.na
.nr PD 0.6v' .LP .2C

# --- files attached to a document ---------------------------------------
contained 60 64 qpdf --deterministic-id "$out/english.pdf" "$out/attached.pdf" \
	--add-attachment "$text" --mimetype=text/plain -- \
	--add-attachment "$out/latin1.pdf" --mimetype=application/pdf --description='A second document' --

(cd "$out" && sha256sum -- *.pdf >MANIFEST.sha256)
ls -l "$out"/layout-* "$out/attached.pdf"
