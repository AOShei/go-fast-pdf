package pdftest

// KnownFailing lists the defect cases that are expected to fail, with the
// phase of IMPLEMENTATION_PLAN.md that closes each. See known.go.
//
// Rules: an entry is removed in the change that fixes the defect. Entries
// are never added for new failures; a regression is a failing test.
var KnownFailing = map[string]string{
	"B2/ctm-scale": "phase 4",
	"B2/tm-scale":  "phase 4",
	"B3":           "phase 4",
	"B5":           "phase 4",
	"B8":           "phase 5",
	"B9":           "phase 4",
	"B10/pdfdoc":   "phase 4",
	"B10/utf16":    "phase 4",
	"B11":          "phase 5",

	"E1/aes-128-userpw": "phase 3",
	"E1/rc4-128-userpw": "phase 3",
	"E3/aes-256":        "phase 3",
	"E3/aes-256-r5":     "phase 3",
	"E4/aes-128-userpw": "phase 5",
	"E4/aes-256-userpw": "phase 5",
	"E4/rc4-128-userpw": "phase 5",

	"F1/dw":       "phase 4",
	"F1/w":        "phase 4",
	"F2":          "phase 4",
	"F3/narrow":   "phase 4",
	"F3/wide":     "phase 4",
	"F4/afii":     "phase 4",
	"F4/euro":     "phase 4",
	"F4/ligature": "phase 4",
	"F4/suffix":   "phase 4",
	"F4/u":        "phase 4",
	"F4/uni":      "phase 4",

	"X1/bad-startxref": "phase 2",
	"X1/no-xref":       "phase 2",
	"X1/wrong-offsets": "phase 2",
	"X2":               "phase 2",
	"X5/short":         "phase 2",
	"X5/zero":          "phase 2",
	"X6":               "phase 2",
	"X7/abbreviated":   "phase 2",
	"X7/ascii85":       "phase 2",
	"X7/asciihex":      "phase 2",
	"X7/chain":         "phase 2",
	"X7/lzw":           "phase 2",
	"X7/runlength":     "phase 2",
}
