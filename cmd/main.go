package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/AOShei/pdf-loader/pkg/loader"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run cmd/main.go <path_to_pdf>")
	}

	path := os.Args[1]
	doc, err := loader.LoadPDF(path)
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
