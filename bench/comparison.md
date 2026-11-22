# Comparison with other PDF text extractors

Written by `go run ./bench/compare`. Do not edit: run it again.

| | Pages per second | Time against go-fast-pdf | Most memory |
|---|---:|---:|---:|
| go-fast-pdf | 10,186 | 1 | 32 MB |
| go-fast-pdf, all processors | 18,991 | 0.54 times | 49 MB |
| gofastpdf (the command) | 8,144 | 1.25 times | 25 MB |
| pdftotext (poppler) | 1,999 | 5.10 times | 20 MB |
| PyMuPDF | 2,231 | 4.56 times | 61 MB |
| pypdfium2 (PDFium) | 2,219 | 4.59 times | 30 MB |
| pypdf | 348 | 29.3 times | 72 MB |
| pdfplumber | 56 | 181 times | 52 MB |
| pdf_oxide (Rust) | 318 | 32.0 times | 30 MB |
| pdf-extract (Rust) | 2,033 | 5.01 times | 23 MB |
| lopdf (Rust) | 6,433 | 1.58 times | 23 MB |

8 documents, 530 pages, 9.7 MB: the long documents of the public corpus's tuning set and one small one.
Each program reads each document 5 times and its best time counts; the table is the sum over the documents, as a batch of them would take, and on one document the order can differ. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-22.
The method, the versions and the time for each document are in [bench/comparison.md](bench/comparison.md).

## What is measured

Each program reads a file from the disk and gives the text of all its pages.

| Program | Version | What is timed |
|---|---|---|
| go-fast-pdf | commit aaa3bc5 | library, one worker |
| go-fast-pdf, all processors | commit aaa3bc5 | library, 24 workers |
| gofastpdf (the command) | commit aaa3bc5 | command, text output, one worker; the time of the process |
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
  one worker. The Rust libraries are held to one thread (RAYON_NUM_THREADS=1); lopdf would otherwise
  read large object streams on several. With one worker, Go's garbage collector can still work on other
  processors beside it; held to one processor (GOMAXPROCS=1), go-fast-pdf is not slower.
- The first table is the sum over the documents: what a batch of them takes. On one document the order
  can differ, as the table of each document shows.
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
| english.pdf (in the repository) | 3 | 0.091 | 0.137 | 1.30 | 5.34 | 1.00 | 1.22 | 3.94 | 35.0 | 0.897 | 0.471 | 0.188 |
| govdocs1-000/000762.pdf | 112 | 13.0 | 3.92 | 15.1 | 54.9 | 52.7 | 55.9 | 415 | 2,100 | 122 | 51.0 | 18.3 |
| govdocs1-000/000153.pdf | 108 | 8.98 | 6.98 | 11.2 | 39.7 | 34.7 | 39.5 | 274 | 1,284 | 238 | 38.0 | 11.8 |
| govdocs1-000/000359.pdf | 77 | 14.3 | 5.07 | 16.2 | 44.2 | 36.9 | 36.7 | 344 | 1,598 | 1,051 | 66.4 | 24.2 |
| govdocs1-000/000578.pdf | 75 | 2.71 | 2.43 | 4.08 | 31.2 | 33.8 | 27.8 | 113 | 1,237 | 67.6 | 17.7 | 6.65 |
| govdocs1-000/000807.pdf | 58 | 4.91 | 3.25 | 6.91 | 35.7 | 29.1 | 24.5 | 162 | 799 | 66.4 | 48.1 | 6.85 |
| govdocs1-000/000358.pdf | 52 | 3.08 | 2.23 | 4.25 | 19.9 | 19.5 | 23.0 | 81.2 | 998 | 39.7 | 12.7 | 3.47 |
| govdocs1-000/000325.pdf | 45 | 4.95 | 3.89 | 5.99 | 34.3 | 29.8 | 30.2 | 131 | 1,351 | 79.0 | 26.3 | 10.9 |

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
