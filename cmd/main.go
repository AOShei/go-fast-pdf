package main

import (
	"encoding/json"
	"fmt"
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

	// Output as JSON
	jsonData, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal JSON: %v", err)
	}

	fmt.Println(string(jsonData))
}
