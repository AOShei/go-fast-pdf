package gofastpdf_test

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	gofastpdf "github.com/AOShei/go-fast-pdf"
	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// packageFiles parses the files of the package in dir that are not
// tests, with their comments.
func packageFiles(t *testing.T, dir string) (*token.FileSet, []*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	names, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	sort.Strings(names)
	var files []*ast.File
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	return fset, files
}

// TestAPISnapshot holds the exported identifiers of the package, with
// their types, against testdata/api.txt: a change to what callers
// compile against shows up in the diff of a change and is meant.
// Regenerate with GOFASTPDF_UPDATE=1.
func TestAPISnapshot(t *testing.T) {
	fset, files := packageFiles(t, ".")
	var decls []string
	for _, f := range files {
		if !ast.FileExports(f) {
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				d.Body, d.Doc = nil, nil
			case *ast.GenDecl:
				if d.Tok == token.IMPORT {
					continue
				}
				d.Doc = nil
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						s.Doc, s.Comment = nil, nil
						if st, ok := s.Type.(*ast.StructType); ok {
							for _, field := range st.Fields.List {
								field.Doc, field.Comment = nil, nil
							}
						}
					case *ast.ValueSpec:
						s.Doc, s.Comment = nil, nil
					}
				}
			}
			var buf bytes.Buffer
			// Printed without the file's comments, so that a comment is
			// not part of the snapshot.
			if err := format.Node(&buf, fset, d); err != nil {
				t.Fatal(err)
			}
			decls = append(decls, buf.String())
		}
	}
	sort.Strings(decls)
	got := "API of github.com/AOShei/go-fast-pdf: the exported declarations, sorted.\n\n" + strings.Join(decls, "\n\n") + "\n"
	pdftest.Golden(t, "testdata/api.txt", []byte(got))
}

// TestEveryExportedIdentifierIsDocumented: functions, methods, types,
// the fields of exported structs, constants and variables each have a
// comment, their own or that of their group.
func TestEveryExportedIdentifierIsDocumented(t *testing.T) {
	for _, dir := range []string{".", "cmd/gofastpdf"} {
		fset, files := packageFiles(t, dir)
		missing := func(pos token.Pos, what string) {
			t.Errorf("%s: %s has no comment", fset.Position(pos), what)
		}
		for _, f := range files {
			for _, d := range f.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					if !d.Name.IsExported() {
						continue
					}
					if d.Recv != nil {
						// A method of an unexported type is not part of
						// the API.
						recv := d.Recv.List[0].Type
						if star, ok := recv.(*ast.StarExpr); ok {
							recv = star.X
						}
						if id, ok := recv.(*ast.Ident); ok && !id.IsExported() {
							continue
						}
					}
					if d.Doc == nil {
						missing(d.Pos(), "func "+d.Name.Name)
					}
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						switch s := spec.(type) {
						case *ast.TypeSpec:
							if !s.Name.IsExported() {
								continue
							}
							if s.Doc == nil && d.Doc == nil {
								missing(s.Pos(), "type "+s.Name.Name)
							}
							if st, ok := s.Type.(*ast.StructType); ok {
								var last *ast.Field
								for _, field := range st.Fields.List {
									exported := false
									for _, name := range field.Names {
										exported = exported || name.IsExported()
									}
									// A field may share the comment of the
									// field just before it, when the two are
									// described together.
									documented := field.Doc != nil || field.Comment != nil ||
										last != nil && fset.Position(field.Pos()).Line == fset.Position(last.End()).Line+1
									if exported && !documented {
										missing(field.Pos(), "field "+s.Name.Name+"."+field.Names[0].Name)
									}
									if exported && (field.Doc != nil || documented) {
										last = field
									} else {
										last = nil
									}
								}
							}
						case *ast.ValueSpec:
							for _, name := range s.Names {
								if name.IsExported() && s.Doc == nil && s.Comment == nil && d.Doc == nil {
									missing(name.Pos(), name.Name)
								}
							}
						}
					}
				}
			}
		}
	}
}

// TestEveryWarningCodeHasAConstant: every code of this library's own
// that the sources name is the value of an exported constant, so that
// a caller can compare with a name and not with a string.
func TestEveryWarningCodeHasAConstant(t *testing.T) {
	constants := map[string]bool{}
	for _, code := range []string{
		gofastpdf.WarnRTLVisualOrder, gofastpdf.WarnDeepHiddenIncomplete, gofastpdf.WarnGlyphTextAssumed, gofastpdf.WarnFontMissing,
		gofastpdf.WarnContentStreamDamaged, gofastpdf.WarnContentStreamUnreadable, gofastpdf.WarnContentBytesSkipped,
		gofastpdf.WarnObjectUnreadable, gofastpdf.WarnPageTreeDamaged, gofastpdf.WarnXRefSectionUnreadable,
		gofastpdf.WarnXRefEntryUnreadable, gofastpdf.WarnStringUndecryptable,
	} {
		if constants[code] || !strings.HasPrefix(code, "pdf.") {
			t.Errorf("the constant with the value %q: twice, or not a code of this library", code)
		}
		constants[code] = true
	}
	code := regexp.MustCompile(`"(pdf\.[a-z_]+)"`)
	used := map[string]bool{}
	filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, _ := os.ReadFile(path)
		for _, m := range code.FindAllStringSubmatch(string(data), -1) {
			used[m[1]] = true
			if !constants[m[1]] {
				t.Errorf("%s names the code %q, which has no constant", path, m[1])
			}
		}
		return nil
	})
	for c := range constants {
		if !used[c] {
			t.Errorf("the code %q has a constant and is used nowhere", c)
		}
	}
}

// TestTheLibraryNeverPrints: no file of the library writes to standard
// output or standard error, or uses the log package. The programs do
// (the command, and the harness under bench), and are not looked at.
func TestTheLibraryNeverPrints(t *testing.T) {
	var dirs []string
	program := func(path string) bool {
		for _, dir := range []string{"cmd", "bench"} {
			if path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && !program(path) &&
			!strings.HasPrefix(d.Name(), ".") && d.Name() != "testdata" && d.Name() != "pdftest" && d.Name() != "gen" {
			dirs = append(dirs, path)
		}
		return nil
	})
	if len(dirs) < 8 {
		t.Fatalf("directories looked at: %v", dirs)
	}
	for _, dir := range dirs {
		fset, files := packageFiles(t, dir)
		for _, f := range files {
			for _, imp := range f.Imports {
				if imp.Path.Value == `"log"` {
					t.Errorf("%s imports log", fset.Position(imp.Pos()))
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				name := pkg.Name + "." + sel.Sel.Name
				switch name {
				case "fmt.Print", "fmt.Println", "fmt.Printf", "os.Stdout", "os.Stderr":
					t.Errorf("%s uses %s", fset.Position(n.Pos()), name)
				}
				return true
			})
			_ = f
		}
	}
	// The built-in print functions write to standard error too.
	for _, dir := range dirs {
		names, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, name := range names {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			data, _ := os.ReadFile(name)
			for i, line := range strings.Split(string(data), "\n") {
				if code := strings.TrimSpace(line); strings.HasPrefix(code, "println(") || strings.HasPrefix(code, "print(") {
					t.Errorf("%s:%d uses a built-in print function", name, i+1)
				}
			}
		}
	}
}
