package pdftest

// KnownFailing lists the defect cases that are expected to fail, with the
// phase of IMPLEMENTATION_PLAN.md that closes each. See known.go.
//
// Rules: an entry is removed in the change that fixes the defect. Entries
// are never added for new failures; a regression is a failing test.
var KnownFailing = map[string]string{
	"B8":                   "phase 5",
	"B11":                  "phase 5",
	"E4/aes-128-unicodepw": "phase 5",
	"E4/aes-128-userpw":    "phase 5",
	"E4/aes-256-r5-userpw": "phase 5",
	"E4/aes-256-unicodepw": "phase 5",
	"E4/aes-256-userpw":    "phase 5",
	"E4/rc4-128-userpw":    "phase 5",
	"E4/rc4-128-v4-userpw": "phase 5",
	"E4/rc4-40-userpw":     "phase 5",
}
