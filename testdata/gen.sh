#!/usr/bin/env bash
# Regenerates testdata/fixtures from the texts in testdata/src.
#
# The output is committed, so tests do not need these tools. Run this only
# to add a fixture or after changing a source text, then review the diff
# and the scores in testdata/baseline.json.
#
# Needs: groff (with gropdf), qpdf, libreoffice.
set -euo pipefail
cd "$(dirname "$0")"

src=src
out=fixtures
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$out"

for tool in groff qpdf soffice sha256sum; do
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
groff -ms -Tpdf "$tmp/english.ms" >"$out/english.pdf"

# 2. Western European text through the same route: simple fonts whose
#    encodings use /Differences.
txt2ms "$src/latin1.txt" 12 16 >"$tmp/latin1.ms"
groff -k -ms -Tpdf "$tmp/latin1.ms" >"$out/latin1.pdf"

# 3. Five languages through LibreOffice: embedded TrueType subsets,
#    composite fonts, ToUnicode CMaps.
cp "$src/multilingual.txt" "$tmp/multilingual.txt"
soffice --headless --norestore -env:UserInstallation="file://$tmp/lo-profile" \
	--infilter="Text (encoded):UTF8,LF,Liberation Serif,en-US" \
	--convert-to pdf --outdir "$tmp" "$tmp/multilingual.txt" >/dev/null
cp "$tmp/multilingual.pdf" "$out/multilingual.pdf"

# 4. Structural variants of the English file. Each must give the same
#    text as english.pdf.
q() { qpdf --deterministic-id "$@"; }
q --object-streams=generate "$out/english.pdf" "$out/english-objstm.pdf"
q --linearize "$out/english.pdf" "$out/english-linearized.pdf"
q --stream-data=uncompress --object-streams=disable "$out/english.pdf" "$out/english-uncompressed.pdf"
q --object-streams=generate --linearize "$out/multilingual.pdf" "$out/multilingual-objstm-linearized.pdf"

# 5. Encryption variants. The user password is empty unless the name says
#    otherwise; the owner password is always "owner".
e() { # e <name> <user password> <bits> <options...>
	local name=$1 user=$2 bits=$3
	shift 3
	qpdf --static-id --allow-weak-crypto "$out/english.pdf" "$out/enc-$name.pdf" --encrypt "$user" owner "$bits" "$@" --
}
e rc4-40 "" 40
e rc4-128 "" 128 --use-aes=n
e rc4-128-v4 "" 128 --use-aes=n --force-V4
e aes-128 "" 128 --use-aes=y
e aes-256 "" 256
e aes-256-r5 "" 256 --force-R5
e rc4-128-userpw user 128 --use-aes=n
e aes-128-userpw user 128 --use-aes=y
e aes-256-userpw user 256

(cd "$out" && sha256sum -- *.pdf >MANIFEST.sha256)
ls -l "$out"
