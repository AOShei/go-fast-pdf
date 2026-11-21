# Comparison with other PDF text extractors

Written by `go run ./bench/compare`. Do not edit: run it again.

| | Pages per second | Time against go-fast-pdf | Most memory |
|---|---:|---:|---:|
| go-fast-pdf | 9,177 | 1 | 33 MB |
| go-fast-pdf, all processors | 18,035 | 0.51 times | 66 MB |
| gofastpdf (the command) | 7,404 | 1.24 times | 24 MB |
| pdftotext (poppler) | 1,992 | 4.61 times | 20 MB |
| PyMuPDF | 2,226 | 4.12 times | 61 MB |
| pypdfium2 (PDFium) | 2,211 | 4.15 times | 30 MB |
| pypdf | 349 | 26.3 times | 71 MB |
| pdfplumber | 56 | 162 times | 53 MB |

8 documents, 530 pages, 9.7 MB: the long documents of the public corpus's tuning set and one small one.
Each program reads each document 5 times and its best time counts. Measured on AMD Ryzen 9 9900X 12-Core Processor (24 threads, linux/amd64) with go1.27.3-X:nodwarf5, 2026-11-21.
The method, the versions and the time for each document are in [bench/comparison.md](bench/comparison.md).

## What is measured

Each program reads a file from the disk and gives the text of all its pages.

| Program | Version | What is timed |
|---|---|---|
| go-fast-pdf | commit afec275 | library, one worker |
| go-fast-pdf, all processors | commit afec275 | library, 24 workers |
| gofastpdf (the command) | commit afec275 | command, text output, one worker; the time of the process |
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
| english.pdf (in the repository) | 3 | 0.102 | 0.155 | 1.69 | 4.98 | 1.01 | 1.24 | 3.96 | 35.0 |
| govdocs1-000/000762.pdf | 112 | 13.7 | 4.11 | 15.8 | 55.3 | 52.7 | 56.1 | 412 | 2,097 |
| govdocs1-000/000153.pdf | 108 | 10.5 | 5.94 | 12.4 | 39.6 | 35.0 | 39.7 | 271 | 1,294 |
| govdocs1-000/000359.pdf | 77 | 15.4 | 5.67 | 18.2 | 44.3 | 37.2 | 37.1 | 348 | 1,583 |
| govdocs1-000/000578.pdf | 75 | 3.05 | 2.87 | 4.54 | 31.9 | 33.3 | 28.0 | 111 | 1,225 |
| govdocs1-000/000807.pdf | 58 | 5.76 | 3.38 | 7.39 | 36.1 | 29.1 | 24.3 | 161 | 798 |
| govdocs1-000/000358.pdf | 52 | 3.37 | 2.49 | 4.78 | 19.8 | 20.0 | 23.1 | 81.3 | 993 |
| govdocs1-000/000325.pdf | 45 | 5.88 | 4.76 | 6.84 | 34.1 | 29.8 | 30.3 | 131 | 1,356 |

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
