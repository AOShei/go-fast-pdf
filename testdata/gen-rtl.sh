#!/usr/bin/env bash
# Generates the fixtures of the tests of text written from right to left,
# testdata/fixtures/rtl-*.pdf, from testdata/src/rtl-*.txt: paragraphs of
# Hebrew and of Arabic, with numbers, Latin words and brackets in them,
# and English paragraphs with Hebrew and Arabic in them, each set by
# LibreOffice, and the Hebrew and the Arabic by Firefox (the Arabic in Noto Sans
# Arabic and in Noto Naskh Arabic). rtl_fixtures_test.go compares what the
# library reads with the source, in the order it is written.
#
# The output is committed. Run this only to add a fixture or after
# changing a source text.
#
# Needs: libreoffice, firefox, python3, and fonts for Hebrew and Arabic
# (Noto Sans Hebrew, Noto Sans Arabic and Noto Naskh Arabic were used, from
# the Arch Linux package noto-fonts 1:2026.11.01-1).
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

# page <name> <lang> <dir> [<font> [<html name>]]: the first paragraph a
# heading, in bold.
page() {
	local fonts=${4:-'"Noto Sans", "Noto Sans Hebrew", "Noto Sans Arabic", sans-serif'}
	{
		printf '<!doctype html>\n<html lang="%s" dir="%s"><head><meta charset="utf-8"><title>%s</title>\n<style>\n' "$2" "$3" "$1"
		printf 'body { font-family: %s; font-size: 12pt; line-height: 1.4; margin: 0 }\n' "$fonts"
		printf 'h1 { font-size: 16pt; margin: 0 0 12pt 0 }\np { margin: 0 0 9pt 0 }\n</style></head><body>\n'
		awk 'BEGIN { RS = ""; FS = "\n" } NR == 1 { print "<h1>" $0 "</h1>"; next } { print "<p>" $0 "</p>" }' "src/$1.txt"
		printf '</body></html>\n'
	} >"$tmp/${5:-$1}.html"
}
for spec in "rtl-he he rtl" "rtl-ar ar rtl" "rtl-mixed en ltr"; do
	set -- $spec
	page "$1" "$2" "$3"
	contained 300 256 soffice --headless --norestore -env:UserInstallation="file://$tmp/lo-profile" \
		--convert-to pdf --outdir "$tmp" "$tmp/$1.html" >/dev/null
	cp "$tmp/$1.pdf" "$out/$1-libreoffice.pdf"
done
contained 180 512 python3 -I ./browser-print.py "$tmp/rtl-he.html" "$out/rtl-he-firefox.pdf" "$tmp/firefox-rtl-he"
# Firefox embeds subsets of the Noto fonts for Arabic that give the glyphs
# of a letter drawn in pieces (its body, its dots) no character: the
# library reads them by their outlines (internal/font/glyphs.go).
page rtl-ar ar rtl '"Noto Sans Arabic", sans-serif'
contained 180 512 python3 -I ./browser-print.py "$tmp/rtl-ar.html" "$out/rtl-ar-firefox.pdf" "$tmp/firefox-rtl-ar"
page rtl-ar ar rtl '"Noto Naskh Arabic", serif' rtl-ar-naskh
contained 180 512 python3 -I ./browser-print.py "$tmp/rtl-ar-naskh.html" "$out/rtl-ar-naskh-firefox.pdf" "$tmp/firefox-rtl-ar-naskh"

(cd "$out" && sha256sum -- *.pdf >MANIFEST.sha256)
ls -l "$out"/rtl-*
