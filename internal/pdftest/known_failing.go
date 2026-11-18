package pdftest

// KnownFailing lists the defect cases that are expected to fail, with the
// phase of IMPLEMENTATION_PLAN.md that closes each. See known.go.
//
// Rules: an entry is removed in the change that fixes the defect. Entries
// are never added for new failures; a regression is a failing test.
var KnownFailing = map[string]string{}
