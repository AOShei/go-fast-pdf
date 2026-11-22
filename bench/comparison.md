# Comparison with other PDF text extractors

Written by `go run ./bench/compare`. Do not edit: run it again.

| | Pages per second | Time against go-fast-pdf | Most memory | Words as pdftotext's (F1) |
|---|---:|---:|---:|---:|
| go-fast-pdf | 10,184 | 1 | 32 MB | 0.984 |
| go-fast-pdf, all processors | 19,200 | 0.53 times | 69 MB | 0.984 |
| gofastpdf (the command) | 8,236 | 1.24 times | 24 MB | 0.984 |
| pdftotext (poppler) | 2,009 | 5.07 times | 20 MB | the reference |
| PyMuPDF | 2,238 | 4.55 times | 62 MB | 0.986 |
| pypdfium2 (PDFium) | 2,230 | 4.57 times | 30 MB | 0.974 |
| pypdf | 350 | 29.1 times | 72 MB | 0.970 |
| pdfplumber | 57 | 179 times | 53 MB | 0.964 |
| pdf_oxide (Rust) | 309 | 33.0 times | 30 MB | 0.974 |
| pdf-extract (Rust) | 2,053 | 4.96 times | 24 MB | 0.970 |
| lopdf (Rust) | 6,637 | 1.53 times | 25 MB | 0.900 |

8 documents, 530 pages, 9.7 MB: the long documents of the public corpus's tuning set and one small one.
Each program reads each document 5 times and its best time counts; the table is the sum over the documents, as a batch of them would take, and on one document the order can differ. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-22.
The last column is how far each program's words agree with pdftotext's, not how correct they are: 1 is the same words.
The method, the versions, the time for each document, and how far each pair of programs agree, each as the reference in turn, are in [bench/comparison.md](bench/comparison.md).

## What is measured

Each program reads a file from the disk and gives the text of all its pages.

| Program | Version | What is timed |
|---|---|---|
| go-fast-pdf | commit ef142e7 | library, one worker |
| go-fast-pdf, all processors | commit ef142e7 | library, 24 workers |
| gofastpdf (the command) | commit ef142e7 | command, text output, one worker; the time of the process |
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
| english.pdf (in the repository) | 3 | 0.104 | 0.119 | 1.55 | 6.04 | 1.01 | 1.20 | 3.93 | 34.7 | 0.894 | 0.447 | 0.259 |
| govdocs1-000/000762.pdf | 112 | 12.4 | 3.38 | 14.5 | 55.1 | 52.5 | 55.3 | 409 | 2,100 | 120 | 51.1 | 19.5 |
| govdocs1-000/000153.pdf | 108 | 8.97 | 6.37 | 10.9 | 40.1 | 34.7 | 39.8 | 271 | 1,271 | 254 | 37.5 | 9.99 |
| govdocs1-000/000359.pdf | 77 | 13.8 | 4.89 | 16.1 | 43.9 | 37.0 | 36.9 | 345 | 1,589 | 1,089 | 65.8 | 20.9 |
| govdocs1-000/000578.pdf | 75 | 3.03 | 3.11 | 4.05 | 31.2 | 33.3 | 27.4 | 111 | 1,225 | 68.4 | 17.3 | 6.35 |
| govdocs1-000/000807.pdf | 58 | 5.41 | 3.24 | 6.61 | 35.3 | 29.1 | 24.3 | 162 | 787 | 66.6 | 47.6 | 7.51 |
| govdocs1-000/000358.pdf | 52 | 3.18 | 2.13 | 4.07 | 19.4 | 19.5 | 22.8 | 80.6 | 989 | 38.7 | 12.7 | 3.59 |
| govdocs1-000/000325.pdf | 45 | 5.19 | 4.36 | 6.51 | 32.8 | 29.8 | 29.9 | 129 | 1,338 | 79.0 | 25.8 | 11.8 |

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

## How the programs agree with each other

The words of each program's text against those of each other program's, as in the last column of the
first table but with every program in turn as the reference: the mean F1 over the documents that every
program read. The table is symmetric, as F1 is the same whichever of two programs is the reference.
go-fast-pdf's three rows give the same text, and are one row here. Programs that agree make the same
choices, which are not always right: what each one does with a running header, a hyphen at the end of
a line, or a superscript is a choice as much as a mistake.

| | go-fast-pdf | pdftotext (poppler) | PyMuPDF | pypdfium2 (PDFium) | pypdf | pdfplumber | pdf_oxide (Rust) | pdf-extract (Rust) | lopdf (Rust) | Mean against the others |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| **go-fast-pdf** | - | 0.984 | 0.994 | 0.977 | 0.977 | 0.970 | 0.973 | 0.966 | 0.905 | 0.968 |
| **pdftotext (poppler)** | 0.984 | - | 0.986 | 0.974 | 0.970 | 0.964 | 0.974 | 0.970 | 0.900 | 0.965 |
| **PyMuPDF** | 0.994 | 0.986 | - | 0.981 | 0.980 | 0.973 | 0.977 | 0.968 | 0.908 | 0.971 |
| **pypdfium2 (PDFium)** | 0.977 | 0.974 | 0.981 | - | 0.987 | 0.980 | 0.986 | 0.977 | 0.914 | 0.972 |
| **pypdf** | 0.977 | 0.970 | 0.980 | 0.987 | - | 0.979 | 0.983 | 0.978 | 0.917 | 0.971 |
| **pdfplumber** | 0.970 | 0.964 | 0.973 | 0.980 | 0.979 | - | 0.978 | 0.971 | 0.915 | 0.966 |
| **pdf_oxide (Rust)** | 0.973 | 0.974 | 0.977 | 0.986 | 0.983 | 0.978 | - | 0.972 | 0.914 | 0.970 |
| **pdf-extract (Rust)** | 0.966 | 0.970 | 0.968 | 0.977 | 0.978 | 0.971 | 0.972 | - | 0.912 | 0.964 |
| **lopdf (Rust)** | 0.905 | 0.900 | 0.908 | 0.914 | 0.917 | 0.915 | 0.914 | 0.912 | - | 0.911 |
