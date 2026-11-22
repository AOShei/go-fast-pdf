"""Times one PDF library on one file, for bench/compare.

    python extract.py <library> <file> <repeats> <budget seconds> [<text file>]

Reads the file and extracts the text of every page, as a caller of the
library would, <repeats> times or until the budget is spent, and prints
one line of JSON: the best time, the characters extracted, the pages,
and the library's version. Starting Python and importing the library are
not part of the time. With a text file, the text of the last run is
written to it, the pages separated by a line feed, for bench/compare to
score.

Libraries: pymupdf, pypdfium2, pypdf, pdfplumber.
"""

import json
import sys
import time


def pymupdf_run():
    import pymupdf

    def run(path):
        doc = pymupdf.open(path)
        texts = [page.get_text() for page in doc]
        doc.close()
        return texts

    return run, pymupdf.__version__


def pypdfium2_run():
    import pypdfium2

    def run(path):
        texts = []
        pdf = pypdfium2.PdfDocument(path)
        for i in range(len(pdf)):
            page = pdf[i]
            text = page.get_textpage()
            texts.append(text.get_text_bounded())
            text.close()
            page.close()
        pdf.close()
        return texts

    return run, str(pypdfium2.PYPDFIUM_INFO)


def pypdf_run():
    import pypdf

    def run(path):
        reader = pypdf.PdfReader(path)
        return [page.extract_text() for page in reader.pages]

    return run, pypdf.__version__


def pdfplumber_run():
    import pdfplumber

    def run(path):
        texts = []
        with pdfplumber.open(path) as pdf:
            for page in pdf.pages:
                texts.append(page.extract_text() or "")
                page.close()
        return texts

    return run, pdfplumber.__version__


LIBRARIES = {
    "pymupdf": pymupdf_run,
    "pypdfium2": pypdfium2_run,
    "pypdf": pypdf_run,
    "pdfplumber": pdfplumber_run,
}


def main():
    library, path, repeats, budget = sys.argv[1], sys.argv[2], int(sys.argv[3]), float(sys.argv[4])
    out = sys.argv[5] if len(sys.argv) > 5 else ""
    try:
        run, version = LIBRARIES[library]()
    except ImportError as err:
        print(json.dumps({"missing": str(err)}))
        return
    best, spent, runs = None, 0.0, 0
    texts = []
    while runs < repeats and (runs == 0 or spent < budget):
        start = time.perf_counter()
        got = run(path)
        took = time.perf_counter() - start
        texts = got  # the last run's text is let go after its time is taken
        best = took if best is None else min(best, took)
        spent += took
        runs += 1
    if out:
        with open(out, "w", encoding="utf-8", errors="replace") as f:
            f.write("\n".join(texts))
    chars = sum(len(t) for t in texts)
    print(json.dumps({"seconds": best, "chars": chars, "pages": len(texts), "runs": runs, "version": version}))


if __name__ == "__main__":
    main()
