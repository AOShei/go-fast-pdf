# Comparison with other PDF text extractors

Written by `go run ./bench/compare`. Do not edit: run it again.

| | Pages per second | Time against go-fast-pdf | Most memory |
|---|---:|---:|---:|
| go-fast-pdf | 12,342 | 1 | 28 MB |
| go-fast-pdf, all processors | 20,903 | 0.59 times | 36 MB |
| gofastpdf (the command) | 9,383 | 1.32 times | 17 MB |
| pdftotext (poppler) | 1,969 | 6.27 times | 18 MB |
| PyMuPDF | 2,230 | 5.53 times | 61 MB |
| pypdfium2 (PDFium) | 2,212 | 5.58 times | 30 MB |
| pypdf | 347 | 35.6 times | 71 MB |
| pdfplumber | 56 | 218 times | 52 MB |

8 documents, 530 pages, 9.7 MB: the long documents of the public corpus's tuning set and one small one.
Each program reads each document 5 times and its best time counts. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-19.
The method, the versions and the time for each document are in [bench/comparison.md](bench/comparison.md).

## What is measured

Each program reads a file from the disk and gives the text of all its pages.

| Program | Version | What is timed |
|---|---|---|
| go-fast-pdf | commit b506f9e | library, one worker |
| go-fast-pdf, all processors | commit b506f9e | library, 24 workers |
| gofastpdf (the command) | commit b506f9e | command, text output, one worker; the time of the process |
| pdftotext (poppler) | 26.10.0 | command; the time of the process |
| PyMuPDF | 2.1 | Python library; the time of its calls, without starting Python |
| pypdfium2 (PDFium) | 5.15.2 | Python library; the time of its calls, without starting Python |
| pypdf | 7.1.0 | Python library; the time of its calls, without starting Python |
| pdfplumber | 0.11.10 | Python library; the time of its calls, without starting Python |

- The libraries are timed inside a process that has already started: the time is that of opening the file
  and extracting every page. The two commands are timed as processes, start to end, which adds about a
  millisecond to each of their runs.
- go-fast-pdf does more than give text: it builds the document of the shared model, with blocks in reading
  order, their boxes, and the checks for hidden text. The others are asked for plain text.
- The other programs read a page at a time on one thread, so the row to compare them with is the one with
  one worker.
- Memory is the most that the process had resident, the interpreter included for the Python libraries.
- The documents are chosen by a rule, from what the manifest of the corpus says of them and nothing else:
  the tuning set, 30 pages or more, not encrypted, not damaged, at most two from one family of producers,
  the longest first. No document is chosen for how a program does on it.
- A time that another job on the machine disturbed is too long, never too short; the best of the runs is
  the one disturbed least.

## Each document

Milliseconds, best run.

| Document | Pages | go-fast-pdf | go-fast-pdf, all processors | gofastpdf (the command) | pdftotext (poppler) | PyMuPDF | pypdfium2 (PDFium) | pypdf | pdfplumber |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| english.pdf (in the repository) | 3 | 0.099 | 0.129 | 1.48 | 5.25 | 1.01 | 1.24 | 3.92 | 34.5 |
| govdocs1-000/000762.pdf | 112 | 12.1 | 5.31 | 14.4 | 55.3 | 52.5 | 56.5 | 418 | 2,107 |
| govdocs1-000/000153.pdf | 108 | 6.08 | 4.13 | 8.14 | 41.7 | 35.0 | 39.6 | 271 | 1,279 |
| govdocs1-000/000359.pdf | 77 | 9.74 | 3.64 | 11.9 | 44.4 | 37.1 | 36.7 | 349 | 1,582 |
| govdocs1-000/000578.pdf | 75 | 2.88 | 3.67 | 4.35 | 31.8 | 33.3 | 27.8 | 113 | 1,227 |
| govdocs1-000/000807.pdf | 58 | 5.23 | 2.71 | 6.78 | 35.9 | 29.2 | 24.6 | 162 | 793 |
| govdocs1-000/000358.pdf | 52 | 2.69 | 2.71 | 4.01 | 19.7 | 19.9 | 23.1 | 80.9 | 997 |
| govdocs1-000/000325.pdf | 45 | 4.13 | 3.04 | 5.37 | 35.0 | 29.7 | 30.1 | 130 | 1,362 |

Characters of text, as each program counts its own output:

| Document | go-fast-pdf | go-fast-pdf, all processors | gofastpdf (the command) | pdftotext (poppler) | PyMuPDF | pypdfium2 (PDFium) | pypdf | pdfplumber |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| english.pdf (in the repository) | 4314 | 4314 | 4314 | 4307 | 4276 | 4357 | 4279 | 4298 |
| govdocs1-000/000762.pdf | 206810 | 206810 | 206810 | 205317 | 212874 | 211667 | 212564 | 205574 |
| govdocs1-000/000153.pdf | 151242 | 151242 | 151242 | 151235 | 153131 | 154615 | 152068 | 149507 |
| govdocs1-000/000359.pdf | 143224 | 143224 | 143224 | 142026 | 148396 | 145411 | 147575 | 141030 |
| govdocs1-000/000578.pdf | 151320 | 151320 | 151320 | 150859 | 151621 | 153152 | 151461 | 151271 |
| govdocs1-000/000807.pdf | 89828 | 89828 | 89828 | 89609 | 88720 | 87205 | 86917 | 86204 |
| govdocs1-000/000358.pdf | 119937 | 119937 | 119937 | 119773 | 120573 | 121132 | 120566 | 119363 |
| govdocs1-000/000325.pdf | 165465 | 165465 | 165465 | 164562 | 164995 | 166970 | 164939 | 164186 |
