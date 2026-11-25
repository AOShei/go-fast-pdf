#!/usr/bin/env bash
# Generates the fixtures of the tests of tables of contents: a heading,
# entries with dot leaders, one of them of two lines, and a paragraph, from
# testdata/src/contents-en.txt, contents-fr.txt and contents-ar.txt.
#
# contents_fixtures_test.go reads them.
#
# The output is committed. Run this only to add a fixture or after
# changing a source text.
#
# Needs: libreoffice, python3, and the fonts Noto Sans and Noto Sans Arabic.
set -euo pipefail
cd "$(dirname "$0")"
. ./contain.sh

out=fixtures
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
for tool in soffice python3 sha256sum; do
	command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 1; }
done
export SOURCE_DATE_EPOCH=1763424000
for lang in en fr ar; do
	name=contents-$lang-libreoffice
	python3 -I - "src/contents-$lang.txt" "$lang" >"$tmp/$name.fodt" <<'PY'
import sys
from xml.sax.saxutils import escape
heading, entries, text = open(sys.argv[1], encoding="utf-8").read().strip().split("\n\n")
rtl = sys.argv[2] == "ar"
mode, align = ("rl-tb", "end") if rtl else ("lr-tb", "start")
print(f'''<?xml version="1.0" encoding="UTF-8"?>
<office:document xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0" xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0" office:version="1.3" office:mimetype="application/vnd.oasis.opendocument.text">
<office:font-face-decls><style:font-face style:name="L" svg:font-family="'Noto Sans'"/><style:font-face style:name="C" svg:font-family="'Noto Sans Arabic'"/></office:font-face-decls>
<office:styles><style:default-style style:family="paragraph"><style:paragraph-properties style:writing-mode="{mode}"/><style:text-properties style:font-name="L" style:font-name-complex="C" fo:font-size="11pt" style:font-size-complex="11pt"/></style:default-style></office:styles>
<office:automatic-styles>
<style:page-layout style:name="pl"><style:page-layout-properties fo:page-width="420pt" fo:page-height="595pt" fo:margin="50pt" style:writing-mode="{mode}"/></style:page-layout>
<style:style style:name="H" style:family="paragraph"><style:paragraph-properties fo:margin-bottom="12pt" fo:text-align="{align}" style:writing-mode="{mode}"/><style:text-properties fo:font-size="18pt" style:font-size-complex="18pt"/></style:style>
<style:style style:name="E" style:family="paragraph"><style:paragraph-properties fo:text-align="{align}" style:writing-mode="{mode}" fo:margin-right="0pt"><style:tab-stops><style:tab-stop style:position="320pt" style:type="right" style:leader-style="dotted" style:leader-text="."/></style:tab-stops></style:paragraph-properties></style:style>
<style:style style:name="P" style:family="paragraph"><style:paragraph-properties fo:margin-top="14pt" fo:text-align="{align}" style:writing-mode="{mode}"/></style:style>
</office:automatic-styles>
<office:master-styles><style:master-page style:name="Standard" style:page-layout-name="pl"/></office:master-styles>
<office:body><office:text>''')
print(f'<text:p text:style-name="H">{escape(heading)}</text:p>')
for e in entries.split("\n"):
    title, label = e.split("\t")
    print(f'<text:p text:style-name="E">{escape(title)}<text:tab/>{escape(label)}</text:p>')
print(f'<text:p text:style-name="P">{escape(text)}</text:p></office:text></office:body></office:document>')
PY
	contained 300 256 soffice --headless --norestore -env:UserInstallation="file://$tmp/lo-profile" \
		--convert-to pdf --outdir "$tmp" "$tmp/$name.fodt" >/dev/null
	cp "$tmp/$name.pdf" "$out/$name.pdf"
done
(cd "$out" && sha256sum -- *.pdf >MANIFEST.sha256)
