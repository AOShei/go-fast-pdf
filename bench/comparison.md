# Comparison with other PDF text extractors

Written by `go run ./bench/compare`. Do not edit: run it again.

| | Pages per second | Time against go-fast-pdf | Most memory |
|---|---:|---:|---:|
| go-fast-pdf | 10,427 | 1 | 34 MB |
| go-fast-pdf, all processors | 19,000 | 0.55 times | 77 MB |
| gofastpdf (the command) | 7,837 | 1.33 times | 25 MB |
| pdftotext (poppler) | 1,990 | 5.24 times | 20 MB |
| PyMuPDF | 2,238 | 4.66 times | 62 MB |
| pypdfium2 (PDFium) | 2,221 | 4.69 times | 30 MB |
| pypdf | 349 | 29.8 times | 72 MB |
| pdfplumber | 57 | 184 times | 52 MB |

8 documents, 530 pages, 9.7 MB: the long documents of the public corpus's tuning set and one small one.
Each program reads each document 5 times and its best time counts. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-21.
The method, the versions and the time for each document are in [bench/comparison.md](bench/comparison.md).

## What is measured

Each program reads a file from the disk and gives the text of all its pages.

| Program | Version | What is timed |
|---|---|---|
| go-fast-pdf | commit e733ea4 | library, one worker |
| go-fast-pdf, all processors | commit e733ea4 | library, 24 workers |
| gofastpdf (the command) | commit e733ea4 | command, text output, one worker; the time of the process |
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
| english.pdf (in the repository) | 3 | 0.086 | 0.138 | 1.59 | 5.01 | 1.01 | 1.23 | 3.91 | 34.6 |
| govdocs1-000/000762.pdf | 112 | 12.8 | 3.86 | 15.1 | 54.5 | 52.6 | 55.9 | 411 | 2,119 |
| govdocs1-000/000153.pdf | 108 | 8.68 | 5.84 | 11.8 | 39.5 | 34.9 | 39.9 | 270 | 1,271 |
| govdocs1-000/000359.pdf | 77 | 13.4 | 4.48 | 16.9 | 44.3 | 36.7 | 36.4 | 344 | 1,578 |
| govdocs1-000/000578.pdf | 75 | 3.01 | 2.80 | 4.34 | 32.4 | 33.3 | 27.6 | 112 | 1,221 |
| govdocs1-000/000807.pdf | 58 | 4.96 | 3.31 | 6.90 | 37.2 | 28.9 | 24.6 | 164 | 789 |
| govdocs1-000/000358.pdf | 52 | 3.01 | 3.60 | 4.35 | 20.0 | 19.4 | 23.2 | 81.4 | 987 |
| govdocs1-000/000325.pdf | 45 | 4.90 | 3.86 | 6.60 | 33.4 | 30.0 | 29.7 | 130 | 1,342 |

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
