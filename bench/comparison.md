# Comparison with other PDF text extractors

Written by `go run ./bench/compare`. Do not edit: run it again.

| | Pages per second | Time against go-fast-pdf | Most memory |
|---|---:|---:|---:|
| go-fast-pdf | 11,925 | 1 | 27 MB |
| go-fast-pdf, all processors | 19,592 | 0.61 times | 35 MB |
| gofastpdf (the command) | 8,761 | 1.36 times | 16 MB |
| pdftotext (poppler) | 1,911 | 6.24 times | 18 MB |
| PyMuPDF | 2,143 | 5.56 times | 61 MB |
| pypdfium2 (PDFium) | 2,104 | 5.67 times | 30 MB |
| pypdf | 335 | 35.6 times | 71 MB |
| pdfplumber | 53 | 225 times | 52 MB |

8 documents, 530 pages, 9.7 MB: the long documents of the public corpus's tuning set and one small one.
Each program reads each document 5 times and its best time counts. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-19.
The method, the versions and the time for each document are in [bench/comparison.md](bench/comparison.md).

## What is measured

Each program reads a file from the disk and gives the text of all its pages.

| Program | Version | What is timed |
|---|---|---|
| go-fast-pdf | commit bb696d4, modified | library, one worker |
| go-fast-pdf, all processors | commit bb696d4, modified | library, 24 workers |
| gofastpdf (the command) | commit bb696d4, modified | command, text output, one worker; the time of the process |
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
| english.pdf (in the repository) | 3 | 0.100 | 0.159 | 1.45 | 6.41 | 1.04 | 1.24 | 3.97 | 36.5 |
| govdocs1-000/000762.pdf | 112 | 12.5 | 4.59 | 14.6 | 56.9 | 55.4 | 57.9 | 432 | 2,341 |
| govdocs1-000/000153.pdf | 108 | 6.03 | 4.34 | 8.54 | 41.0 | 36.8 | 44.3 | 285 | 1,369 |
| govdocs1-000/000359.pdf | 77 | 10.0 | 4.66 | 14.0 | 45.7 | 37.9 | 37.0 | 355 | 1,717 |
| govdocs1-000/000578.pdf | 75 | 3.06 | 3.48 | 5.81 | 32.8 | 34.9 | 29.5 | 117 | 1,222 |
| govdocs1-000/000807.pdf | 58 | 5.37 | 3.72 | 6.92 | 36.7 | 31.3 | 25.6 | 170 | 793 |
| govdocs1-000/000358.pdf | 52 | 2.86 | 2.66 | 3.91 | 21.7 | 19.9 | 24.8 | 85.7 | 1,024 |
| govdocs1-000/000325.pdf | 45 | 4.54 | 3.43 | 5.26 | 36.1 | 30.1 | 31.6 | 136 | 1,508 |

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
