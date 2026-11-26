# Comparison with other PDF text extractors

Written by `go run ./bench/compare`. Do not edit: run it again.

| | Pages per second | Time against go-fast-pdf | Most memory | Words as pdftotext's (F1) |
|---|---:|---:|---:|---:|
| go-fast-pdf | 9,187 | 1 | 33 MB | 0.986 |
| go-fast-pdf, all processors | 19,225 | 0.48 times | 67 MB | 0.986 |
| gofastpdf (the command) | 7,495 | 1.23 times | 22 MB | 0.986 |
| pdftotext (poppler) | 1,988 | 4.62 times | 21 MB | the reference |
| PyMuPDF | 2,239 | 4.10 times | 63 MB | 0.986 |
| pypdfium2 (PDFium) | 2,199 | 4.18 times | 32 MB | 0.974 |
| pypdf | 349 | 26.4 times | 74 MB | 0.970 |
| pdfplumber | 56 | 163 times | 55 MB | 0.964 |
| pdf_oxide (Rust) | 320 | 28.7 times | 31 MB | 0.974 |
| pdf-extract (Rust) | 2,030 | 4.53 times | 25 MB | 0.970 |
| lopdf (Rust) | 6,358 | 1.44 times | 27 MB | 0.900 |

8 documents, 530 pages, 9.7 MB: the long documents of the public corpus's tuning set and one small one.
Each program reads each document 5 times and its best time counts; the table is the sum over the documents, as a batch of them would take, and on one document the order can differ. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-26.
The last column is how far each program's words agree with pdftotext's, not how correct they are: 1 is the same words.
The method, the versions, the time for each document, and how far each pair of programs agree, each as the reference in turn, are in [bench/comparison.md](bench/comparison.md).

## What is measured

Each program reads a file from the disk and gives the text of all its pages.

| Program | Version | What is timed |
|---|---|---|
| go-fast-pdf | commit 57a89ca, modified | library, one worker |
| go-fast-pdf, all processors | commit 57a89ca, modified | library, 24 workers |
| gofastpdf (the command) | commit 57a89ca, modified | command, text output, one worker; the time of the process |
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
| english.pdf (in the repository) | 3 | 0.088 | 0.110 | 1.45 | 5.45 | 1.01 | 1.24 | 3.96 | 35.0 | 0.888 | 0.444 | 0.228 |
| govdocs1-000/000762.pdf | 112 | 14.3 | 3.81 | 16.3 | 55.0 | 52.4 | 56.0 | 408 | 2,095 | 121 | 50.4 | 19.1 |
| govdocs1-000/000153.pdf | 108 | 10.3 | 7.34 | 12.0 | 40.0 | 34.9 | 40.4 | 274 | 1,287 | 235 | 37.6 | 11.9 |
| govdocs1-000/000359.pdf | 77 | 15.0 | 4.86 | 16.9 | 44.7 | 36.6 | 38.1 | 347 | 1,599 | 1,050 | 69.1 | 23.2 |
| govdocs1-000/000578.pdf | 75 | 3.34 | 2.84 | 4.73 | 32.2 | 33.4 | 27.7 | 112 | 1,229 | 67.2 | 17.7 | 7.27 |
| govdocs1-000/000807.pdf | 58 | 6.14 | 2.82 | 7.78 | 36.1 | 29.1 | 24.5 | 161 | 794 | 65.7 | 47.5 | 6.53 |
| govdocs1-000/000358.pdf | 52 | 2.99 | 2.10 | 4.24 | 19.3 | 19.5 | 22.9 | 82.1 | 1,002 | 38.8 | 12.5 | 3.60 |
| govdocs1-000/000325.pdf | 45 | 5.50 | 3.69 | 7.32 | 33.8 | 29.7 | 30.3 | 131 | 1,349 | 78.7 | 25.9 | 11.5 |

Characters of text, as each program counts its own output:

| Document | go-fast-pdf | go-fast-pdf, all processors | gofastpdf (the command) | pdftotext (poppler) | PyMuPDF | pypdfium2 (PDFium) | pypdf | pdfplumber | pdf_oxide (Rust) | pdf-extract (Rust) | lopdf (Rust) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| english.pdf (in the repository) | 4313 | 4313 | 4313 | 4307 | 4276 | 4357 | 4279 | 4298 | 4309 | 4290 | 4280 |
| govdocs1-000/000762.pdf | 204460 | 204460 | 204460 | 205317 | 212874 | 211667 | 212564 | 205574 | 210601 | 213787 | 213302 |
| govdocs1-000/000153.pdf | 153737 | 153737 | 153737 | 151235 | 153131 | 154615 | 152068 | 149507 | 152703 | 155942 | 150577 |
| govdocs1-000/000359.pdf | 142716 | 142716 | 142716 | 142026 | 148396 | 145411 | 147575 | 141030 | 142629 | 149000 | 146732 |
| govdocs1-000/000578.pdf | 151055 | 151055 | 151055 | 150859 | 151621 | 153152 | 151461 | 151271 | 151427 | 152328 | 148996 |
| govdocs1-000/000807.pdf | 89800 | 89800 | 89800 | 89609 | 88720 | 87205 | 86917 | 86204 | 87083 | 87615 | 84876 |
| govdocs1-000/000358.pdf | 119739 | 119739 | 119739 | 119773 | 120573 | 121132 | 120566 | 119363 | 121182 | 121007 | 118918 |
| govdocs1-000/000325.pdf | 161826 | 161826 | 161826 | 164562 | 164995 | 166970 | 164939 | 164186 | 164697 | 165306 | 164149 |

Words as pdftotext's (F1):

| Document | go-fast-pdf | go-fast-pdf, all processors | gofastpdf (the command) | PyMuPDF | pypdfium2 (PDFium) | pypdf | pdfplumber | pdf_oxide (Rust) | pdf-extract (Rust) | lopdf (Rust) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| english.pdf (in the repository) | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 |
| govdocs1-000/000762.pdf | 0.988 | 0.988 | 0.988 | 0.985 | 0.985 | 0.966 | 0.975 | 0.989 | 0.980 | 0.924 |
| govdocs1-000/000153.pdf | 0.988 | 0.988 | 0.988 | 0.997 | 0.999 | 0.999 | 0.998 | 1.000 | 0.985 | 0.964 |
| govdocs1-000/000359.pdf | 0.980 | 0.980 | 0.980 | 0.979 | 0.984 | 0.971 | 0.969 | 0.970 | 0.983 | 0.953 |
| govdocs1-000/000578.pdf | 0.995 | 0.995 | 0.995 | 0.978 | 0.979 | 0.977 | 0.979 | 0.990 | 0.937 | 0.855 |
| govdocs1-000/000807.pdf | 0.957 | 0.957 | 0.957 | 0.956 | 0.856 | 0.863 | 0.823 | 0.863 | 0.889 | 0.702 |
| govdocs1-000/000358.pdf | 0.993 | 0.993 | 0.993 | 0.994 | 0.994 | 0.991 | 0.989 | 0.992 | 0.991 | 0.865 |
| govdocs1-000/000325.pdf | 0.990 | 0.990 | 0.990 | 0.997 | 0.997 | 0.996 | 0.981 | 0.992 | 0.997 | 0.938 |

## How the programs agree with each other

The words of each program's text against those of each other program's, as in the last column of the
first table but with every program in turn as the reference: the mean F1 over the documents that every
program read. The table is symmetric, as F1 is the same whichever of two programs is the reference.
go-fast-pdf's three rows give the same text, and are one row here. Programs that agree make the same
choices, which are not always right: what each one does with a running header, a hyphen at the end of
a line, or a superscript is a choice as much as a mistake.

| | go-fast-pdf | pdftotext (poppler) | PyMuPDF | pypdfium2 (PDFium) | pypdf | pdfplumber | pdf_oxide (Rust) | pdf-extract (Rust) | lopdf (Rust) | Mean against the others |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| **go-fast-pdf** | - | 0.986 | 0.992 | 0.977 | 0.975 | 0.968 | 0.975 | 0.964 | 0.904 | 0.968 |
| **pdftotext (poppler)** | 0.986 | - | 0.986 | 0.974 | 0.970 | 0.964 | 0.974 | 0.970 | 0.900 | 0.966 |
| **PyMuPDF** | 0.992 | 0.986 | - | 0.981 | 0.980 | 0.973 | 0.977 | 0.968 | 0.908 | 0.970 |
| **pypdfium2 (PDFium)** | 0.977 | 0.974 | 0.981 | - | 0.987 | 0.980 | 0.986 | 0.977 | 0.914 | 0.972 |
| **pypdf** | 0.975 | 0.970 | 0.980 | 0.987 | - | 0.979 | 0.983 | 0.978 | 0.917 | 0.971 |
| **pdfplumber** | 0.968 | 0.964 | 0.973 | 0.980 | 0.979 | - | 0.978 | 0.971 | 0.915 | 0.966 |
| **pdf_oxide (Rust)** | 0.975 | 0.974 | 0.977 | 0.986 | 0.983 | 0.978 | - | 0.972 | 0.914 | 0.970 |
| **pdf-extract (Rust)** | 0.964 | 0.970 | 0.968 | 0.977 | 0.978 | 0.971 | 0.972 | - | 0.912 | 0.964 |
| **lopdf (Rust)** | 0.904 | 0.900 | 0.908 | 0.914 | 0.917 | 0.915 | 0.914 | 0.912 | - | 0.910 |
