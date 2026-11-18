// Command go-fast-pdf prints a PDF file as JSON in the shared document
// model of the go-fast libraries.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
)

func main() {
	workers := flag.Int("workers", 0, "pages extracted at a time (0: one per CPU)")
	images := flag.Bool("images", false, "list the images on each page")
	password := flag.String("password", "", "password of an encrypted file")
	flag.Parse()
	if flag.NArg() != 1 {
		log.Fatal("usage: go-fast-pdf [-workers N] [-images] [-password P] <file.pdf>")
	}
	file, err := os.Open(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		log.Fatal(err)
	}
	opts := &gofastcore.Options{Workers: *workers, Password: *password, Name: info.Name()}
	if *images {
		opts.Images = gofastcore.ImagesMeta
	}
	doc, err := gofastpdf.Extract(context.Background(), file, info.Size(), opts)
	if err != nil {
		log.Fatalf("%s: %v", flag.Arg(0), err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		log.Fatal(err)
	}
}
