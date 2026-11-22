# Comparison with other PDF text extractors

Written by `go run ./bench/compare`. Do not edit: run it again.

| | Pages per second | Time against go-fast-pdf | Most memory |
|---|---:|---:|---:|
| go-fast-pdf | 10,074 | 1 | 30 MB |
| go-fast-pdf, all processors | 19,869 | 0.51 times | 55 MB |
| gofastpdf (the command) | 7,973 | 1.26 times | 25 MB |
| pdftotext (poppler) | 2,004 | 5.03 times | 20 MB |
| PyMuPDF | 2,226 | 4.53 times | 61 MB |
| pypdfium2 (PDFium) | 2,212 | 4.55 times | 30 MB |
| pypdf | 349 | 28.9 times | 71 MB |
| pdfplumber | 56 | 179 times | 53 MB |
| pdf_oxide (Rust) | 322 | 31.3 times | 30 MB |
| pdf-extract (Rust) | 2,046 | 4.92 times | 23 MB |
| lopdf (Rust) | 7,445 | 1.35 times | 25 MB |

8 documents, 530 pages, 9.7 MB: the long documents of the public corpus's tuning set and one small one.
Each program reads each document 5 times and its best time counts. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-22.
The method, the versions and the time for each document are in [bench/comparison.md](bench/comparison.md).

## What is measured

Each program reads a file from the disk and gives the text of all its pages.

| Program | Version | What is timed |
|---|---|---|
| go-fast-pdf | commit 76b3e10, modified | library, one worker |
| go-fast-pdf, all processors | commit 76b3e10, modified | library, 24 workers |
| gofastpdf (the command) | commit 76b3e10, modified | command, text output, one worker; the time of the process |
| pdftotext (poppler) | 26.10.0 | command; the time of the process |
| PyMuPDF | 2.1 | Python library; the time of its calls, without starting Python |
| pypdfium2 (PDFium) | 5.15.2 | Python library; the time of its calls, without starting Python |
| pypdf | 7.1.0 | Python library; the time of its calls, without starting Python |
| pdfplumber | 0.11.10 | Python library; the time of its calls, without starting Python |
| pdf_oxide (Rust) | 0.3.78 | Rust library; the time of its calls, without starting the program |
| pdf-extract (Rust) | 0.12.1 | Rust library; the time of its calls, without starting the program |
| lopdf (Rust) | 0.47.0 | Rust library; the time of its calls, without starting the program |

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

| Document | Pages | go-fast-pdf | go-fast-pdf, all processors | gofastpdf (the command) | pdftotext (poppler) | PyMuPDF | pypdfium2 (PDFium) | pypdf | pdfplumber | pdf_oxide (Rust) | pdf-extract (Rust) | lopdf (Rust) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| english.pdf (in the repository) | 3 | 0.090 | 0.113 | 1.52 | 5.28 | 1.01 | 1.23 | 3.89 | 34.7 | 0.899 | 0.436 | 0.276 |
| govdocs1-000/000762.pdf | 112 | 12.6 | 3.24 | 15.7 | 55.6 | 52.8 | 56.1 | 411 | 2,110 | 121 | 50.6 | 18.4 |
| govdocs1-000/000153.pdf | 108 | 9.16 | 6.59 | 11.4 | 40.9 | 35.1 | 39.8 | 270 | 1,280 | 236 | 38.2 | 7.44 |
| govdocs1-000/000359.pdf | 77 | 14.9 | 4.95 | 16.3 | 43.7 | 37.0 | 36.8 | 347 | 1,585 | 1,034 | 66.3 | 18.4 |
| govdocs1-000/000578.pdf | 75 | 2.89 | 2.45 | 4.14 | 31.5 | 33.4 | 27.7 | 111 | 1,235 | 68.5 | 17.2 | 6.13 |
| govdocs1-000/000807.pdf | 58 | 5.19 | 2.97 | 6.92 | 34.8 | 29.3 | 24.5 | 163 | 792 | 66.6 | 47.8 | 6.21 |
| govdocs1-000/000358.pdf | 52 | 2.79 | 2.88 | 4.23 | 20.0 | 19.6 | 23.3 | 81.5 | 1,002 | 39.6 | 12.7 | 3.47 |
| govdocs1-000/000325.pdf | 45 | 4.98 | 3.48 | 6.31 | 32.8 | 29.8 | 30.1 | 131 | 1,366 | 79.3 | 25.9 | 10.9 |

Characters of text, as each program counts its own output:

| Document | go-fast-pdf | go-fast-pdf, all processors | gofastpdf (the command) | pdftotext (poppler) | PyMuPDF | pypdfium2 (PDFium) | pypdf | pdfplumber | pdf_oxide (Rust) | pdf-extract (Rust) | lopdf (Rust) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| english.pdf (in the repository) | 4313 | 4313 | 4313 | 4307 | 4276 | 4357 | 4279 | 4298 | 4309 | 4290 | 4280 |
| govdocs1-000/000762.pdf | 204241 | 204241 | 204241 | 205317 | 212874 | 211667 | 212564 | 205574 | 210601 | 213787 | 213302 |
| govdocs1-000/000153.pdf | 153861 | 153861 | 153861 | 151235 | 153131 | 154615 | 152068 | 149507 | 152703 | 155942 | 150577 |
| govdocs1-000/000359.pdf | 142747 | 142747 | 142747 | 142026 | 148396 | 145411 | 147575 | 141030 | 142629 | 149000 | 146732 |
| govdocs1-000/000578.pdf | 151325 | 151325 | 151325 | 150859 | 151621 | 153152 | 151461 | 151271 | 151427 | 152328 | 148996 |
| govdocs1-000/000807.pdf | 89792 | 89792 | 89792 | 89609 | 88720 | 87205 | 86917 | 86204 | 87083 | 87615 | 84876 |
| govdocs1-000/000358.pdf | 119736 | 119736 | 119736 | 119773 | 120573 | 121132 | 120566 | 119363 | 121182 | 121007 | 118918 |
| govdocs1-000/000325.pdf | 161828 | 161828 | 161828 | 164562 | 164995 | 166970 | 164939 | 164186 | 164697 | 165306 | 164149 |
