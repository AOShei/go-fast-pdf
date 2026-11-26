// Command gofastpdf extracts the text and structure of PDF files.
//
//	gofastpdf [flags] <file or directory>...
//
// Each input is written to standard output, or to the file given with
// -o, in the format asked for: the shared document model of the go-fast
// libraries as JSON, or its text as Markdown or plain text. A directory
// stands for the PDF files in it and below it. What goes wrong is
// reported on standard error, and the exit status is 1 if any input
// could not be read and 2 if the command line is wrong.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"

	gofastcore "github.com/AOShei/go-fast-core"
	gofastpdf "github.com/AOShei/go-fast-pdf"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// pageRange reads the value of -pages: "3", "2-5", "4-" or "-7".
func pageRange(v string) (gofastcore.PartRange, error) {
	bad := fmt.Errorf("%q is not a range of pages; write 3, 2-5, 4- or -7", v)
	number := func(s string) (int, error) {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return 0, bad
		}
		return n, nil
	}
	first, last, dash := strings.Cut(v, "-")
	var r gofastcore.PartRange
	var err error
	switch {
	case !dash:
		if r.First, err = number(first); err != nil {
			return r, err
		}
		r.Last = r.First
	case first == "" && last == "":
		return r, bad
	default:
		if first != "" {
			if r.First, err = number(first); err != nil {
				return r, err
			}
		}
		if last != "" {
			if r.Last, err = number(last); err != nil {
				return r, err
			}
		}
		if r.Last != 0 && r.First > r.Last {
			return r, bad
		}
	}
	return r, nil
}

// version is what -version prints: the version of the module the
// command was built from.
func version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

// run is the command: its arguments, where it writes, and its exit
// status.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("gofastpdf", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "json", "what to write: json, markdown or text")
	output := flags.String("o", "", "write to this `file` and not to standard output")
	password := flags.String("password", "", "the password of an encrypted file")
	pages := flags.String("pages", "", "the pages to read: 3, 2-5, 4- or -7 (default: all)")
	images := flags.String("images", "meta", "images: meta (a block for each, with where it is and how large), data (their bytes too) or none")
	workers := flags.Int("workers", 0, "pages read at a time (default: one per processor)")
	offPage := flags.Bool("exclude-off-page", false, "leave out the text that is wholly outside the visible box of its page")
	// The flags for the renderers, which every go-fast command has.
	renderFlags := gofastcore.AddRenderFlags(flags)
	flags.Lookup("stored").Usage += "; a PDF file has macro, document_variable, custom_xml, form_field and after_end"
	quiet := flags.Bool("quiet", false, "do not report the warnings of files that were read")
	showVersion := flags.Bool("version", false, "print the version and exit")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: gofastpdf [flags] <file or directory>...")
		fmt.Fprintln(stderr, "Extracts PDF files into JSON (the go-fast document model), Markdown or text.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "gofastpdf", version())
		return 0
	}
	usage := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "gofastpdf: "+format+"\n", a...)
		return 2
	}
	if flags.NArg() == 0 {
		flags.Usage()
		return 2
	}

	opts := &gofastcore.Options{Workers: *workers, Password: *password}
	render := &gofastcore.RenderOptions{}
	renderFlags.Apply(render)
	switch *format {
	case "json", "markdown", "text":
	default:
		return usage("-format %q: write json, markdown or text", *format)
	}
	switch *images {
	case "none":
		opts.Images = gofastcore.ImagesNone
	case "meta":
		opts.Images = gofastcore.ImagesMeta
	case "data":
		opts.Images = gofastcore.ImagesData
	default:
		return usage("-images %q: write none, meta or data", *images)
	}
	if *pages != "" {
		r, err := pageRange(*pages)
		if err != nil {
			return usage("-pages: %v", err)
		}
		opts.Parts = r
	}
	if *workers < 0 {
		return usage("-workers %d: not a number of workers", *workers)
	}

	// The inputs: files as given, and the PDF files of directories.
	var inputs []string
	status := 0
	failed := func(path string, err error) {
		fmt.Fprintf(stderr, "gofastpdf: %s: %v\n", path, err)
		status = 1
	}
	for _, arg := range flags.Args() {
		info, err := os.Stat(arg)
		switch {
		case err != nil:
			failed(arg, err)
		case info.IsDir():
			var found []string
			err := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					failed(path, err)
					return nil
				}
				if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".pdf") {
					found = append(found, path)
				}
				return nil
			})
			if err != nil {
				failed(arg, err)
			}
			sort.Strings(found)
			inputs = append(inputs, found...)
		default:
			inputs = append(inputs, arg)
		}
	}

	out := stdout
	if *output != "" {
		file, err := os.Create(*output)
		if err != nil {
			fmt.Fprintf(stderr, "gofastpdf: %v\n", err)
			return 1
		}
		defer func() {
			if err := file.Close(); err != nil {
				failed(*output, err)
			}
		}()
		out = file
	}
	several := len(inputs) > 1 || len(flags.Args()) > 1
	extractor := &gofastpdf.Extractor{NoOffPageText: *offPage}

	for _, path := range inputs {
		if ctx.Err() != nil {
			failed(path, ctx.Err())
			break
		}
		doc, err := extractor.ExtractFile(ctx, path, opts)
		if err != nil {
			failed(path, err)
			continue
		}
		if !*quiet {
			report(stderr, path, doc)
		}
		switch *format {
		case "json":
			enc := json.NewEncoder(out)
			enc.SetEscapeHTML(false)
			if !several {
				enc.SetIndent("", "  ")
			}
			err = enc.Encode(doc)
		case "markdown":
			if several {
				fmt.Fprintf(out, "<!-- %s -->\n\n", path)
			}
			err = doc.WriteMarkdown(out, render)
		case "text":
			if several {
				fmt.Fprintf(out, "==> %s <==\n", path)
			}
			err = doc.WriteText(out, render)
		}
		if err != nil {
			failed(path, err)
		}
	}
	return status
}

// report says on w which warnings a document that was read has: one
// line for the file, with each code and how often it occurs.
func report(w io.Writer, path string, doc *gofastcore.Document) {
	counts := map[string]int{}
	for _, warning := range doc.AllWarnings() {
		counts[warning.Code]++
	}
	if len(counts) == 0 {
		return
	}
	codes := make([]string, 0, len(counts))
	for code := range counts {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for i, code := range codes {
		if counts[code] > 1 {
			codes[i] = fmt.Sprintf("%s (%d)", code, counts[code])
		}
	}
	fmt.Fprintf(w, "gofastpdf: %s: %s\n", path, strings.Join(codes, ", "))
}
