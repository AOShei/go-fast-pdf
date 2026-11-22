# bench

- `run.sh` runs the Go benchmarks and records them with the machine and toolchain; `baseline.txt` holds
  the numbers of v0.1.1.
- `compare.sh` compares the speed of the working tree with a base commit (`BASE`), and fails if a
  benchmark got more than 10 percent slower.
- `compare/` measures go-fast-pdf against other PDF libraries and writes `comparison.md`.
- `tools/extract.py` and `tools/rust/` time the Python and the Rust libraries for `compare/`. The Rust
  program is built outside the repository:
  `CARGO_TARGET_DIR=../../../../bench-env/rust-target cargo build --release` in `tools/rust`.
- `v0.2.0.txt` holds the numbers of v0.2.0, and what the features added after Phase 8 cost.
- `v0.2.1.txt` holds the numbers of v0.2.1.
- `phase*.txt` and `phase*-evidence.txt` are dated records of measurements made while the library was
  built. They are history: the options, reasons and warnings they name are those of their date, and
  some (`DeepHidden`, the reasons `tiny` and `low_contrast`, ...) were removed before v0.2.0.
