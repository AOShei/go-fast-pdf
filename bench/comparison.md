# Comparison with other PDF text extractors

Written by `go run ./bench/compare`. Do not edit: run it again.

| | Pages per second | Time against go-fast-pdf | Most memory | Words as pdftotext's (F1) |
|---|---:|---:|---:|---:|
| go-fast-pdf | 9,959 | 1 | 33 MB | 0.984 |
| go-fast-pdf, all processors | 18,792 | 0.53 times | 62 MB | 0.984 |
| gofastpdf (the command) | 7,986 | 1.25 times | 23 MB | 0.984 |
| pdftotext (poppler) | 1,983 | 5.02 times | 20 MB | the reference |
| PyMuPDF | 2,213 | 4.50 times | 62 MB | 0.986 |
| pypdfium2 (PDFium) | 2,209 | 4.51 times | 30 MB | 0.974 |
| pypdf | 345 | 28.9 times | 72 MB | 0.970 |
| pdfplumber | 54 | 183 times | 53 MB | 0.964 |
| pdf_oxide (Rust) | 313 | 31.9 times | 31 MB | 0.974 |
| pdf-extract (Rust) | 2,019 | 4.93 times | 24 MB | 0.970 |
| lopdf (Rust) | 6,565 | 1.52 times | 25 MB | 0.900 |

8 documents, 530 pages, 9.7 MB: the long documents of the public corpus's tuning set and one small one.
Each program reads each document 5 times and its best time counts; the table is the sum over the documents, as a batch of them would take, and on one document the order can differ. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-22.
The last column is how far each program's words agree with pdftotext's, not how correct they are: 1 is the same words.
The method, the versions and the time for each document are in [bench/comparison.md](bench/comparison.md).

## What is measured

Each program reads a file from the disk and gives the text of all its pages.

| Program | Version | What is timed |
|---|---|---|
| go-fast-pdf | commit 67a3fc4 | library, one worker |
| go-fast-pdf, all processors | commit 67a3fc4 | library, 24 workers |
| gofastpdf (the command) | commit 67a3fc4 | command, text output, one worker; the time of the process |
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
- Words as pdftotext's (F1) compares the words of each program's text with those of pdftotext's text of
  the same document, as the tests of the corpus do (internal/pdftest): the share of its words that
  pdftotext's text has and the share of pdftotext's words that it has, each word counted as often as it
  occurs, are combined, and the table gives the mean over the documents. It is agreement with poppler,
  not correctness: pdftotext makes mistakes of its own, and the order of the words does not count.
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
| english.pdf (in the repository) | 3 | 0.089 | 0.119 | 1.74 | 4.86 | 1.03 | 1.24 | 3.97 | 35.6 | 0.947 | 0.463 | 0.238 |
| govdocs1-000/000762.pdf | 112 | 12.6 | 3.25 | 14.8 | 54.8 | 52.6 | 55.7 | 418 | 2,127 | 124 | 51.1 | 20.2 |
| govdocs1-000/000153.pdf | 108 | 9.13 | 6.34 | 11.4 | 40.7 | 35.8 | 41.2 | 273 | 1,381 | 246 | 39.6 | 9.81 |
| govdocs1-000/000359.pdf | 77 | 14.2 | 4.96 | 16.2 | 45.3 | 37.0 | 36.8 | 350 | 1,737 | 1,069 | 67.2 | 20.9 |
| govdocs1-000/000578.pdf | 75 | 3.11 | 3.21 | 4.25 | 32.4 | 33.4 | 27.5 | 113 | 1,282 | 68.5 | 17.8 | 6.41 |
| govdocs1-000/000807.pdf | 58 | 5.53 | 2.97 | 6.78 | 35.4 | 29.6 | 24.3 | 163 | 803 | 66.7 | 47.7 | 7.68 |
| govdocs1-000/000358.pdf | 52 | 3.11 | 3.13 | 4.29 | 19.6 | 19.7 | 22.7 | 82.3 | 1,005 | 39.7 | 12.6 | 3.44 |
| govdocs1-000/000325.pdf | 45 | 5.45 | 4.22 | 6.81 | 34.3 | 30.4 | 30.5 | 132 | 1,366 | 80.3 | 26.0 | 12.0 |

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

Words as pdftotext's (F1):

| Document | go-fast-pdf | go-fast-pdf, all processors | gofastpdf (the command) | PyMuPDF | pypdfium2 (PDFium) | pypdf | pdfplumber | pdf_oxide (Rust) | pdf-extract (Rust) | lopdf (Rust) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| english.pdf (in the repository) | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 |
| govdocs1-000/000762.pdf | 0.983 | 0.983 | 0.983 | 0.985 | 0.985 | 0.966 | 0.975 | 0.989 | 0.980 | 0.924 |
| govdocs1-000/000153.pdf | 0.988 | 0.988 | 0.988 | 0.997 | 0.999 | 0.999 | 0.998 | 1.000 | 0.985 | 0.964 |
| govdocs1-000/000359.pdf | 0.979 | 0.979 | 0.979 | 0.979 | 0.984 | 0.971 | 0.969 | 0.970 | 0.983 | 0.953 |
| govdocs1-000/000578.pdf | 0.980 | 0.980 | 0.980 | 0.978 | 0.979 | 0.977 | 0.979 | 0.990 | 0.937 | 0.855 |
| govdocs1-000/000807.pdf | 0.957 | 0.957 | 0.957 | 0.956 | 0.856 | 0.863 | 0.823 | 0.863 | 0.889 | 0.702 |
| govdocs1-000/000358.pdf | 0.993 | 0.993 | 0.993 | 0.994 | 0.994 | 0.991 | 0.989 | 0.992 | 0.991 | 0.865 |
| govdocs1-000/000325.pdf | 0.990 | 0.990 | 0.990 | 0.997 | 0.997 | 0.996 | 0.981 | 0.992 | 0.997 | 0.938 |
