#!/usr/bin/env bash
# Regenerates testdata/fixtures from the texts in testdata/src.
#
# The output is committed, so tests do not need these tools. Run this only
# to add a fixture or after changing a source text, then review the diff
# and the scores in testdata/baseline.json.
#
# Needs: groff (with gropdf), qpdf, libreoffice, ghostscript, firefox,
# python3.
#
# Every program is run through "contained" (contain.sh): a time limit, a
# limit on the size of any file it writes, and no process left behind.
# LibreOffice in particular works through a child process that a plain
# timeout would not stop.
set -euo pipefail
cd "$(dirname "$0")"
. ./contain.sh

src=src
out=fixtures
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$out"

for tool in groff qpdf soffice gs firefox python3 sha256sum; do
	command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 1; }
done

# Fixed timestamps and IDs, so that regenerating changes as little as possible.
export SOURCE_DATE_EPOCH=1763424000

# txt2ms turns paragraphs separated by blank lines into groff ms input:
# no hyphenation, no page-number header, ragged right, so that the words
# on the page are exactly the words of the source.
txt2ms() {
	printf '.nr PS %s\n.nr VS %s\n.nr HY 0\n.nh\n.ds CH\n.na\n' "$2" "$3"
	printf '.pdfinfo /Title Sample document\n.pdfinfo /Author go-fast-pdf tests\n'
	awk 'BEGIN { RS = ""; FS = "\n" } { print ".LP"; for (i = 1; i <= NF; i++) print $i }' "$1"
}

# 1. English text, Core-14 fonts without embedded widths (groff).
txt2ms "$src/english.txt" 15 22 >"$tmp/english.ms"
contained 120 64 groff -ms -Tpdf "$tmp/english.ms" >"$out/english.pdf"

# 2. Western European text through the same route: simple fonts whose
#    encodings use /Differences.
txt2ms "$src/latin1.txt" 12 16 >"$tmp/latin1.ms"
contained 120 64 groff -k -ms -Tpdf "$tmp/latin1.ms" >"$out/latin1.pdf"

# 3. Five languages through LibreOffice: embedded TrueType subsets,
#    composite fonts, ToUnicode CMaps.
cp "$src/multilingual.txt" "$tmp/multilingual.txt"
contained 300 256 soffice --headless --norestore -env:UserInstallation="file://$tmp/lo-profile" \
	--infilter="Text (encoded):UTF8,LF,Liberation Serif,en-US" \
	--convert-to pdf --outdir "$tmp" "$tmp/multilingual.txt" >/dev/null
cp "$tmp/multilingual.pdf" "$out/multilingual.pdf"

# 4. Structural variants of the English file. Each must give the same
#    text as english.pdf.
# The same three paragraphs in five languages, each from three programs
# that make PDF in different ways. The expected text of each is its
# source, src/para-<language>.txt, so what is measured is accuracy, not
# agreement with another reader.
office() { # office <text file>: LibreOffice writes <name>.pdf beside it
	contained 300 256 soffice --headless --norestore -env:UserInstallation="file://$tmp/lo-profile" \
		--infilter="Text (encoded):UTF8,LF,Liberation Serif,en-US" \
		--convert-to pdf --outdir "$tmp" "$1" >/dev/null
}
distill() { # distill <input> <output>: Ghostscript writes the PDF again
	contained 120 64 gs -q -dBATCH -dNOPAUSE -dSAFER -sDEVICE=pdfwrite -o "$2" "$1"
}
for lang in en fr de ru el; do
	cp "$src/para-$lang.txt" "$tmp/para-$lang.txt"
	office "$tmp/para-$lang.txt"
	cp "$tmp/para-$lang.pdf" "$out/para-$lang-libreoffice.pdf"

	# Firefox prints a web page of the same text.
	{
		printf '<!doctype html>\n<html lang="%s"><head><meta charset="utf-8"><title>Sample document</title>\n' "$lang"
		printf '<style>body { font-family: serif; font-size: 12pt; line-height: 1.4 }</style></head><body>\n'
		awk 'BEGIN { RS = ""; FS = "\n" } NR == 1 { print "<h1>" $0 "</h1>"; next } { print "<p>" $0 "</p>" }' "$src/para-$lang.txt"
		printf '</body></html>\n'
	} >"$tmp/para-$lang.html"
	contained 180 512 python3 -I ./browser-print.py "$tmp/para-$lang.html" "$out/para-$lang-firefox.pdf" "$tmp/firefox-$lang"

	# Ghostscript: from groff's PostScript where groff's fonts have the
	# letters, and from LibreOffice's PDF where they do not.
	case $lang in
	en | fr | de)
		{
			printf '.nr PS 12\n.nr VS 16\n.nr HY 0\n.nh\n.ds CH\n.na\n'
			awk 'BEGIN { RS = ""; FS = "\n" } { print ".LP"; for (i = 1; i <= NF; i++) print $i }' "$src/para-$lang.txt"
		} >"$tmp/para-$lang.ms"
		contained 120 64 groff -k -ms -Tps "$tmp/para-$lang.ms" >"$tmp/para-$lang.ps"
		distill "$tmp/para-$lang.ps" "$out/para-$lang-ghostscript.pdf"
		;;
	*)
		distill "$tmp/para-$lang.pdf" "$out/para-$lang-ghostscript.pdf"
		;;
	esac
done

q() { contained 60 64 qpdf --deterministic-id "$@"; }
q --object-streams=generate "$out/english.pdf" "$out/english-objstm.pdf"
q --linearize "$out/english.pdf" "$out/english-linearized.pdf"
q --stream-data=uncompress --object-streams=disable "$out/english.pdf" "$out/english-uncompressed.pdf"
q --object-streams=generate --linearize "$out/multilingual.pdf" "$out/multilingual-objstm-linearized.pdf"

# 5. Encryption variants. The user password is empty unless the name says
#    otherwise; the owner password is always "owner".
e() { # e <name> <user password> <bits> <options...>
	local name=$1 user=$2 bits=$3
	shift 3
	contained 60 64 qpdf --static-id --allow-weak-crypto "$out/english.pdf" "$out/enc-$name.pdf" --encrypt "$user" owner "$bits" "$@" --
}
e rc4-40 "" 40
e rc4-128 "" 128 --use-aes=n
e rc4-128-v4 "" 128 --use-aes=n --force-V4
e aes-128 "" 128 --use-aes=y
e aes-256 "" 256
e aes-256-r5 "" 256 --force-R5
# With a user password: the file does not open without it.
e rc4-40-userpw user 40
e rc4-128-userpw user 128 --use-aes=n
e rc4-128-v4-userpw user 128 --use-aes=n --force-V4
e aes-128-userpw user 128 --use-aes=y
e aes-256-userpw user 256
e aes-256-r5-userpw user 256 --force-R5
# Metadata left in the clear, which changes how the key is derived.
e aes-128-cleartext "" 128 --use-aes=y --cleartext-metadata
e aes-256-cleartext "" 256 --cleartext-metadata
# A password outside ASCII: Latin-1 bytes up to 128 bits, UTF-8 for 256.
e aes-128-unicodepw "pässwörd" 128 --use-aes=y
e aes-256-unicodepw "pässwörd" 256

# The file key of every encrypted fixture as qpdf derives it, for the
# key-derivation tests.
: >"$out/KEYS.txt"
for f in "$out"/enc-*.pdf; do
	name=$(basename "$f" .pdf)
	pw=""
	case $name in *-unicodepw) pw="pässwörd" ;; *-userpw) pw="user" ;; esac
	key=$(contained 60 64 qpdf --show-encryption --show-encryption-key --password="$pw" "$f" | sed -n 's/^Encryption key = //p')
	echo "$name $key" >>"$out/KEYS.txt"
done

(cd "$out" && sha256sum -- *.pdf >MANIFEST.sha256)
ls -l "$out"
