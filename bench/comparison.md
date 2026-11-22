# Comparison with other PDF text extractors

Written by `go run ./bench/compare`. Do not edit: run it again.

| | Pages per second | Time against go-fast-pdf | Most memory |
|---|---:|---:|---:|
| go-fast-pdf | 10,011 | 1 | 30 MB |
| go-fast-pdf, all processors | 19,773 | 0.51 times | 59 MB |
| gofastpdf (the command) | 7,907 | 1.27 times | 24 MB |
| pdftotext (poppler) | 1,989 | 5.03 times | 20 MB |
| PyMuPDF | 2,234 | 4.48 times | 62 MB |
| pypdfium2 (PDFium) | 2,202 | 4.55 times | 30 MB |
| pypdf | 345 | 29.1 times | 72 MB |
| pdfplumber | 56 | 178 times | 53 MB |

8 documents, 530 pages, 9.7 MB: the long documents of the public corpus's tuning set and one small one.
Each program reads each document 5 times and its best time counts. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-22.
The method, the versions and the time for each document are in [bench/comparison.md](bench/comparison.md).

## What is measured

Each program reads a file from the disk and gives the text of all its pages.

| Program | Version | What is timed |
|---|---|---|
| go-fast-pdf | commit 09513b2 | library, one worker |
| go-fast-pdf, all processors | commit 09513b2 | library, 24 workers |
| gofastpdf (the command) | commit 09513b2 | command, text output, one worker; the time of the process |
| pdftotext (poppler) | 26.10.0 | command; the time of the process |
| PyMuPDF | 2.1 | Python library; the time of its calls, without starting Python |
| pypdfium2 (PDFium) | 5.15.2 | Python library; the time of its calls, without starting Python |
| pypdf | 7.1.0 | Python library; the time of its calls, without starting Python |
| pdfplumber | 0.11.10 | Python library; the time of its calls, without starting Python |

- The libraries are timed inside a process that has already started: the time is that of opening the file
  and extracting every page. The two commands are timed as processes, start to end, which adds about a
  millisecond to each of their runs.
- go-fast-pdf does more than give text: it builds the document of the shared model, with blocks in reading
  order, their boxes, headings, lists and tables. The others are asked for plain text.
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
| english.pdf (in the repository) | 3 | 0.109 | 0.135 | 1.69 | 5.28 | 1.03 | 1.25 | 3.93 | 34.9 |
| govdocs1-000/000762.pdf | 112 | 12.9 | 4.07 | 15.6 | 55.6 | 52.5 | 56.0 | 415 | 2,141 |
| govdocs1-000/000153.pdf | 108 | 9.35 | 6.64 | 11.3 | 40.5 | 35.1 | 40.5 | 278 | 1,283 |
| govdocs1-000/000359.pdf | 77 | 14.2 | 4.83 | 17.1 | 44.6 | 37.0 | 37.5 | 353 | 1,604 |
| govdocs1-000/000578.pdf | 75 | 2.73 | 2.25 | 4.24 | 32.1 | 33.4 | 27.7 | 113 | 1,232 |
| govdocs1-000/000807.pdf | 58 | 5.56 | 2.66 | 6.78 | 35.9 | 29.0 | 24.8 | 163 | 800 |
| govdocs1-000/000358.pdf | 52 | 3.06 | 2.17 | 4.28 | 19.0 | 19.4 | 23.1 | 82.1 | 996 |
| govdocs1-000/000325.pdf | 45 | 5.00 | 4.06 | 6.01 | 33.5 | 29.7 | 29.9 | 131 | 1,355 |

Characters of text, as each program counts its own output:

| Document | go-fast-pdf | go-fast-pdf, all processors | gofastpdf (the command) | pdftotext (poppler) | PyMuPDF | pypdfium2 (PDFium) | pypdf | pdfplumber |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| english.pdf (in the repository) | 4313 | 4313 | 4313 | 4307 | 4276 | 4357 | 4279 | 4298 |
| govdocs1-000/000762.pdf | 204241 | 204241 | 204241 | 205317 | 212874 | 211667 | 212564 | 205574 |
| govdocs1-000/000153.pdf | 153861 | 153861 | 153861 | 151235 | 153131 | 154615 | 152068 | 149507 |
| govdocs1-000/000359.pdf | 142747 | 142747 | 142747 | 142026 | 148396 | 145411 | 147575 | 141030 |
| govdocs1-000/000578.pdf | 151325 | 151325 | 151325 | 150859 | 151621 | 153152 | 151461 | 151271 |
| govdocs1-000/000807.pdf | 89792 | 89792 | 89792 | 89609 | 88720 | 87205 | 86917 | 86204 |
| govdocs1-000/000358.pdf | 119736 | 119736 | 119736 | 119773 | 120573 | 121132 | 120566 | 119363 |
| govdocs1-000/000325.pdf | 161828 | 161828 | 161828 | 164562 | 164995 | 166970 | 164939 | 164186 |
