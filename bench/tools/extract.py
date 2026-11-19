"""Times one PDF library on one file, for bench/compare.

    python extract.py <library> <file> <repeats> <budget seconds>

Reads the file and extracts the text of every page, as a caller of the
library would, <repeats> times or until the budget is spent, and prints
one line of JSON: the best time, the characters extracted, the pages,
and the library's version. Starting Python and importing the library are
not part of the time.

Libraries: pymupdf, pypdfium2, pypdf, pdfplumber.
"""

import json
import sys
import time


def pymupdf_run():
    import pymupdf

    def run(path):
        chars = 0
        doc = pymupdf.open(path)
        pages = doc.page_count
        for page in doc:
            chars += len(page.get_text())
        doc.close()
        return chars, pages

    return run, pymupdf.__version__


def pypdfium2_run():
    import pypdfium2

    def run(path):
        chars = 0
        pdf = pypdfium2.PdfDocument(path)
        pages = len(pdf)
        for i in range(pages):
            page = pdf[i]
            text = page.get_textpage()
            chars += len(text.get_text_bounded())
            text.close()
            page.close()
        pdf.close()
        return chars, pages

    return run, str(pypdfium2.PYPDFIUM_INFO)


def pypdf_run():
    import pypdf

    def run(path):
        chars = 0
        reader = pypdf.PdfReader(path)
        for page in reader.pages:
            chars += len(page.extract_text())
        return chars, len(reader.pages)

    return run, pypdf.__version__


def pdfplumber_run():
    import pdfplumber

    def run(path):
        chars = 0
        with pdfplumber.open(path) as pdf:
            pages = len(pdf.pages)
            for page in pdf.pages:
                chars += len(page.extract_text() or "")
                page.close()
        return chars, pages

    return run, pdfplumber.__version__


LIBRARIES = {
    "pymupdf": pymupdf_run,
    "pypdfium2": pypdfium2_run,
    "pypdf": pypdf_run,
    "pdfplumber": pdfplumber_run,
}


def main():
    library, path, repeats, budget = sys.argv[1], sys.argv[2], int(sys.argv[3]), float(sys.argv[4])
    try:
        run, version = LIBRARIES[library]()
    except ImportError as err:
        print(json.dumps({"missing": str(err)}))
        return
    best, spent, runs = None, 0.0, 0
    chars = pages = 0
    while runs < repeats and (runs == 0 or spent < budget):
        start = time.perf_counter()
        chars, pages = run(path)
        took = time.perf_counter() - start
        best = took if best is None else min(best, took)
        spent += took
        runs += 1
    print(json.dumps({"seconds": best, "chars": chars, "pages": pages, "runs": runs, "version": version}))


if __name__ == "__main__":
    main()
