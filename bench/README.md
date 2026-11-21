# bench

- `run.sh` runs the Go benchmarks and records them with the machine and toolchain; `baseline.txt` holds
  the numbers of v0.1.1.
- `compare.sh` compares the speed of the working tree with a base commit (`BASE`), and fails if a
  benchmark got more than 10 percent slower.
- `compare/` measures go-fast-pdf against other PDF libraries and writes `comparison.md`.
- `v0.2.0.txt` holds the numbers of v0.2.0, and what the features added after Phase 8 cost.
- `phase*.txt` and `phase*-evidence.txt` are dated records of measurements made while the library was
  built. They are history: the options, reasons and warnings they name are those of their date, and
  some (`DeepHidden`, the reasons `tiny` and `low_contrast`, ...) were removed before v0.2.0.
