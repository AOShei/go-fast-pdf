package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"github.com/AOShei/pdf-loader/pkg/loader"
)

func main() {
	concurrent := flag.Bool("concurrent", false, "Enable concurrent page processing")
	workers := flag.Int("workers", 0, "Number of worker threads (0 = auto-detect, default: NumCPU)")
	flag.Parse()

	if flag.NArg() < 1 {
		log.Fatal("Usage: pdf-loader [--concurrent] [--workers N] <path_to_pdf>")
	}

	path := flag.Arg(0)

	var err error
	var doc interface{}

	if *concurrent {
		doc, err = loader.LoadPDFConcurrent(path, *workers)
	} else {
		doc, err = loader.LoadPDF(path)
	}

	if err != nil {
		log.Fatalf("Failed to load PDF: %v", err)
	}

	// Output as JSON with HTML escaping disabled for better readability
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(doc); err != nil {
		log.Fatalf("Failed to encode JSON: %v", err)
	}
}
