// Package gofastpdf extracts text and structure from PDF files using the
// standard library and the go-fast modules only.
//
// The library is being rebuilt towards v0.2.0; IMPLEMENTATION_PLAN.md has
// the phases. Until Phase 5 lands the public API here, the entry points
// are in pkg/loader, and this package holds only the conformance tests
// that every phase is measured against.
package gofastpdf
