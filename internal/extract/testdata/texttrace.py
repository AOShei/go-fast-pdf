"""Prints PyMuPDF's text trace of a PDF as JSON, for TestAgainstPyMuPDF.

One list per page, of [glyphs, type, red, green, blue, opacity, size] for
every span: type is 0 for filled text, 1 for stroked, 3 for text that is
not painted; the colour is sRGB from 0 to 255.
"""
import json
import sys

import pymupdf

pages = []
for page in pymupdf.open(sys.argv[1]):
    spans = []
    for s in page.get_texttrace():
        color = s.get("color") or (0, 0, 0)
        if not isinstance(color, (tuple, list)):
            color = (color, color, color)
        if len(color) == 1:
            color = color * 3
        r, g, b = (int(round(max(0.0, min(1.0, c)) * 255)) for c in color[:3])
        spans.append([len(s["chars"]), s["type"], r, g, b, s.get("opacity", 1.0), s.get("size", 0.0)])
    pages.append(spans)
json.dump(pages, sys.stdout)
