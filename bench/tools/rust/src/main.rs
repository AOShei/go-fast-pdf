//! Times one Rust PDF library on one file, for bench/compare.
//!
//!     pdfbench-rust <library> <file> <repeats> <budget seconds> [<text file>]
//!
//! Reads the file and extracts the text of every page, as a caller of the
//! library would, <repeats> times or until the budget is spent, and prints
//! one line of JSON: the best time, the characters extracted, the pages,
//! and the library's version, as bench/tools/extract.py does for the
//! Python libraries. Starting the program is not part of the time. With a
//! text file, the text of the last run is written to it, the pages
//! separated by a line feed, for bench/compare to score.
//!
//! Libraries: pdf_oxide, pdf-extract, lopdf.

use std::panic;
use std::process::exit;
use std::time::Instant;

/// The text of every page of the file.
type Run = fn(&str) -> Result<Vec<String>, String>;

fn pdf_oxide(path: &str) -> Result<Vec<String>, String> {
    let doc = pdf_oxide::PdfDocument::open(path).map_err(|e| e.to_string())?;
    let pages = doc.page_count().map_err(|e| e.to_string())?;
    (0..pages).map(|i| doc.extract_text(i).map_err(|e| e.to_string())).collect()
}

fn pdf_extract(path: &str) -> Result<Vec<String>, String> {
    let data = std::fs::read(path).map_err(|e| e.to_string())?;
    pdf_extract::extract_text_from_mem_by_pages(&data).map_err(|e| e.to_string())
}

fn lopdf(path: &str) -> Result<Vec<String>, String> {
    let data = std::fs::read(path).map_err(|e| e.to_string())?;
    let doc = lopdf::Document::load_mem(&data).map_err(|e| e.to_string())?;
    let numbers: Vec<u32> = doc.get_pages().keys().copied().collect();
    numbers.iter().map(|n| doc.extract_text(&[*n]).map_err(|e| e.to_string())).collect()
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 5 && args.len() != 6 {
        eprintln!("usage: pdfbench-rust <library> <file> <repeats> <budget seconds> [<text file>]");
        exit(2);
    }
    let (run, version): (Run, &str) = match args[1].as_str() {
        "pdf_oxide" => (pdf_oxide, "0.3.78"),
        "pdf-extract" => (pdf_extract, "0.12.1"),
        "lopdf" => (lopdf, "0.47.0"),
        other => {
            println!("{{\"missing\": \"no library {other}\"}}");
            return;
        }
    };
    let path = args[2].clone();
    let repeats: u32 = args[3].parse().unwrap_or(1);
    let budget: f64 = args[4].parse().unwrap_or(20.0);
    // A library that panics on a file fails on it; the comparison goes on.
    panic::set_hook(Box::new(|info| eprintln!("panic: {info}")));
    let (mut best, mut spent, mut runs) = (f64::MAX, 0.0, 0u32);
    let mut texts = Vec::new();
    while runs < repeats && (runs == 0 || spent < budget) {
        let start = Instant::now();
        let got = panic::catch_unwind(|| run(&path));
        let took = start.elapsed().as_secs_f64();
        // The last run's text is let go after its time is taken.
        match got {
            Ok(Ok(t)) => texts = t,
            Ok(Err(e)) => {
                eprintln!("error: {e}");
                exit(1);
            }
            Err(_) => exit(1),
        }
        best = best.min(took);
        spent += took;
        runs += 1;
    }
    if let Some(out) = args.get(5) {
        if let Err(e) = std::fs::write(out, texts.join("\n")) {
            eprintln!("error: {out}: {e}");
            exit(1);
        }
    }
    let chars: usize = texts.iter().map(|t| t.chars().count()).sum();
    let pages = texts.len();
    println!("{{\"seconds\": {best}, \"chars\": {chars}, \"pages\": {pages}, \"runs\": {runs}, \"version\": \"{version}\"}}");
}
