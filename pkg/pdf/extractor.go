package pdf

import (
	"io"
	"math"
	"strings"

	"github.com/AOShei/pdf-loader/pkg/model"
)

// Matrix is a 3x3 transform matrix (last row implicitly 0,0,1).
type Matrix [6]float64

// glyphToUnicode maps PostScript glyph names to Unicode characters
var glyphToUnicode = map[string]string{
	"/space":        " ",
	"/exclam":       "!",
	"/quotedbl":     "\"",
	"/numbersign":   "#",
	"/dollar":       "$",
	"/percent":      "%",
	"/ampersand":    "&",
	"/quoteright":   "'",
	"/quotesingle":  "'",
	"/parenleft":    "(",
	"/parenright":   ")",
	"/asterisk":     "*",
	"/plus":         "+",
	"/comma":        ",",
	"/hyphen":       "-",
	"/period":       ".",
	"/slash":        "/",
	"/zero":         "0",
	"/one":          "1",
	"/two":          "2",
	"/three":        "3",
	"/four":         "4",
	"/five":         "5",
	"/six":          "6",
	"/seven":        "7",
	"/eight":        "8",
	"/nine":         "9",
	"/colon":        ":",
	"/semicolon":    ";",
	"/less":         "<",
	"/equal":        "=",
	"/greater":      ">",
	"/question":     "?",
	"/at":           "@",
	"/A":            "A",
	"/B":            "B",
	"/C":            "C",
	"/D":            "D",
	"/E":            "E",
	"/F":            "F",
	"/G":            "G",
	"/H":            "H",
	"/I":            "I",
	"/J":            "J",
	"/K":            "K",
	"/L":            "L",
	"/M":            "M",
	"/N":            "N",
	"/O":            "O",
	"/P":            "P",
	"/Q":            "Q",
	"/R":            "R",
	"/S":            "S",
	"/T":            "T",
	"/U":            "U",
	"/V":            "V",
	"/W":            "W",
	"/X":            "X",
	"/Y":            "Y",
	"/Z":            "Z",
	"/bracketleft":  "[",
	"/backslash":    "\\",
	"/bracketright": "]",
	"/asciicircum":  "^",
	"/underscore":   "_",
	"/grave":        "`",
	"/quoteleft":    "`",
	"/a":            "a",
	"/b":            "b",
	"/c":            "c",
	"/d":            "d",
	"/e":            "e",
	"/f":            "f",
	"/g":            "g",
	"/h":            "h",
	"/i":            "i",
	"/j":            "j",
	"/k":            "k",
	"/l":            "l",
	"/m":            "m",
	"/n":            "n",
	"/o":            "o",
	"/p":            "p",
	"/q":            "q",
	"/r":            "r",
	"/s":            "s",
	"/t":            "t",
	"/u":            "u",
	"/v":            "v",
	"/w":            "w",
	"/x":            "x",
	"/y":            "y",
	"/z":            "z",
	"/braceleft":    "{",
	"/bar":          "|",
	"/braceright":   "}",
	"/asciitilde":   "~",

	// Ligatures
	"/fi":  "fi",
	"/fl":  "fl",
	"/ff":  "ff",
	"/ffi": "ffi",
	"/ffl": "ffl",
	"/st":  "st",
	"/ct":  "ct",
	"/IJ":  "IJ",
	"/ij":  "ij",

	// Extended Latin characters
	"/AE":      "Æ",
	"/ae":      "æ",
	"/OE":      "Œ",
	"/oe":      "œ",
	"/oslash":  "ø",
	"/Oslash":  "Ø",
	"/lslash":  "ł",
	"/Lslash":  "Ł",
	"/Eth":     "Ð",
	"/eth":     "ð",
	"/Thorn":   "Þ",
	"/thorn":   "þ",
	"/ssharp":  "ß",
	"/Scaron":  "Š",
	"/scaron":  "š",
	"/Zcaron":  "Ž",
	"/zcaron":  "ž",
	"/Ccedilla": "Ç",
	"/ccedilla": "ç",

	// Mathematical operators
	"/minus":        "−", // U+2212 math minus (not hyphen)
	"/multiply":     "×",
	"/divide":       "÷",
	"/notequal":     "≠",
	"/lessequal":    "≤",
	"/greaterequal": "≥",
	"/approxequal":  "≈",
	"/infinity":     "∞",
	"/integral":     "∫",
	"/product":      "∏",
	"/summation":    "∑",
	"/radical":      "√",
	"/partialdiff":  "∂",
	"/plusminus":    "±",
	"/therefore":    "∴",
	"/proportional": "∝",
	"/angle":        "∠",
	"/logicaland":   "∧",
	"/logicalor":    "∨",
	"/intersection": "∩",
	"/union":        "∪",

	// Greek letters (common in math/science)
	"/Alpha":   "Α",
	"/Beta":    "Β",
	"/Gamma":   "Γ",
	"/Delta":   "Δ",
	"/Epsilon": "Ε",
	"/Zeta":    "Ζ",
	"/Eta":     "Η",
	"/Theta":   "Θ",
	"/Iota":    "Ι",
	"/Kappa":   "Κ",
	"/Lambda":  "Λ",
	"/Mu":      "Μ",
	"/Nu":      "Ν",
	"/Xi":      "Ξ",
	"/Omicron": "Ο",
	"/Pi":      "Π",
	"/Rho":     "Ρ",
	"/Sigma":   "Σ",
	"/Tau":     "Τ",
	"/Upsilon": "Υ",
	"/Phi":     "Φ",
	"/Chi":     "Χ",
	"/Psi":     "Ψ",
	"/Omega":   "Ω",
	"/alpha":   "α",
	"/beta":    "β",
	"/gamma":   "γ",
	"/delta":   "δ",
	"/epsilon": "ε",
	"/zeta":    "ζ",
	"/eta":     "η",
	"/theta":   "θ",
	"/iota":    "ι",
	"/kappa":   "κ",
	"/lambda":  "λ",
	"/mu":      "μ",
	"/nu":      "ν",
	"/xi":      "ξ",
	"/omicron": "ο",
	"/pi":      "π",
	"/rho":     "ρ",
	"/sigma":   "σ",
	"/tau":     "τ",
	"/upsilon": "υ",
	"/phi":     "φ",
	"/chi":     "χ",
	"/psi":     "ψ",
	"/omega":   "ω",

	// Astronomy/Physics symbols
	"/circledot": "⊙", // Solar mass symbol
	"/sun":       "☉",
	"/venus":     "♀",
	"/earth":     "♁",
	"/mars":      "♂",
	"/jupiter":   "♃",
	"/saturn":    "♄",
	"/uranus":    "♅",
	"/neptune":   "♆",
	"/pluto":     "♇",

	// Superscripts
	"/zero.superior":  "⁰",
	"/one.superior":   "¹",
	"/two.superior":   "²",
	"/three.superior": "³",
	"/four.superior":  "⁴",
	"/five.superior":  "⁵",
	"/six.superior":   "⁶",
	"/seven.superior": "⁷",
	"/eight.superior": "⁸",
	"/nine.superior":  "⁹",
	"/plus.superior":  "⁺",
	"/minus.superior": "⁻",

	// Subscripts
	"/zero.inferior":  "₀",
	"/one.inferior":   "₁",
	"/two.inferior":   "₂",
	"/three.inferior": "₃",
	"/four.inferior":  "₄",
	"/five.inferior":  "₅",
	"/six.inferior":   "₆",
	"/seven.inferior": "₇",
	"/eight.inferior": "₈",
	"/nine.inferior":  "₉",
	"/plus.inferior":  "₊",
	"/minus.inferior": "₋",

	// Zero-width characters
	"/zerowidthspace":     "\u200B",
	"/zerowidthnonjoiner": "\u200C",
	"/zerowidthjoiner":    "\u200D",
}

// isPrintableASCII returns true if byte is printable ASCII
func isPrintableASCII(b byte) bool {
	return b >= 0x20 && b <= 0x7E
}

// isWhitespaceChar returns true for intentional whitespace
func isWhitespaceChar(b byte) bool {
	return b == 0x09 || // tab
		b == 0x0A || // line feed
		b == 0x0D // carriage return
}

// filterControlChars removes non-printable control characters
// but preserves intentional whitespace
func filterControlChars(rawBytes []byte) string {
	var result strings.Builder
	for _, b := range rawBytes {
		if isPrintableASCII(b) || isWhitespaceChar(b) {
			result.WriteByte(b)
		}
		// Drop other control characters (0x00-0x1F except tab/LF/CR)
	}
	return result.String()
}

func IdentityMatrix() Matrix {
	return Matrix{1, 0, 0, 1, 0, 0}
}

// Mult multiplies matrix a by matrix b.
func (a Matrix) Mult(b Matrix) Matrix {
	return Matrix{
		a[0]*b[0] + a[1]*b[2],
		a[0]*b[1] + a[1]*b[3],
		a[2]*b[0] + a[3]*b[2],
		a[2]*b[1] + a[3]*b[3],
		a[4]*b[0] + a[5]*b[2] + b[4],
		a[4]*b[1] + a[5]*b[3] + b[5],
	}
}

// GraphicsState tracks global graphics parameters (CTM).
type GraphicsState struct {
	CTM Matrix // Current Transformation Matrix
}

// Font represents a PDF font with metrics and mapping.
type Font struct {
	BaseFont   string
	CMap       *CMap
	Encoding   map[int]string  // Map char code -> glyph name (from /Encoding/Differences)
	Widths     map[int]float64 // Map char code -> width (1/1000 units)
	MissingW   float64         // Default width
	SpaceWidth float64         // Width of a space character
	IsCID      bool
}

// TextState tracks text-specific parameters.
type TextState struct {
	Font        *Font
	FontSize    float64
	CharSpacing float64
	WordSpacing float64
	Scale       float64
	Leading     float64
	Rise        float64

	TM  Matrix // Text Matrix
	TLM Matrix // Text Line Matrix
}

func NewTextState() TextState {
	return TextState{
		TM:    IdentityMatrix(),
		TLM:   IdentityMatrix(),
		Scale: 100.0,
	}
}

// Extractor handles the logic of pulling text from a page.
type Extractor struct {
	reader *Reader
	page   DictionaryObject

	// State
	gState    GraphicsState
	gStack    []GraphicsState
	textState TextState

	// Resources
	fonts map[string]*Font

	// Output
	lastX, lastY float64
	buffer       strings.Builder

	// Image tracking
	images   []model.Image
	xobjects DictionaryObject
}

func NewExtractor(r *Reader, page DictionaryObject) (*Extractor, error) {
	e := &Extractor{
		reader:    r,
		page:      page,
		gState:    GraphicsState{CTM: IdentityMatrix()},
		textState: NewTextState(),
		fonts:     make(map[string]*Font),
	}

	// Load Fonts and XObjects from Resources
	if res, ok := r.Resolve(page["/Resources"]).(DictionaryObject); ok {
		if fonts, ok := r.Resolve(res["/Font"]).(DictionaryObject); ok {
			for name, ref := range fonts {
				fontObj := r.Resolve(ref).(DictionaryObject)
				e.fonts[name] = e.loadFont(fontObj)
			}
		}

		// Load XObject resources for image handling
		if xobjects, ok := r.Resolve(res["/XObject"]).(DictionaryObject); ok {
			e.xobjects = xobjects
		}
	}

	return e, nil
}

// loadFont parses widths and ToUnicode maps
func (e *Extractor) loadFont(obj DictionaryObject) *Font {
	f := &Font{
		Widths:   make(map[int]float64),
		Encoding: make(map[int]string),
		MissingW: 0, // Default usually 0 unless specified
	}

	// 1. Get BaseFont name (for debugging/fallback)
	if bf, ok := e.reader.Resolve(obj["/BaseFont"]).(NameObject); ok {
		f.BaseFont = string(bf)
	}

	// 2. Parse Widths (Simple Fonts)
	// PDF defines widths for range FirstChar to LastChar
	if firstObj, ok := e.reader.Resolve(obj["/FirstChar"]).(NumberObject); ok {
		first := int(firstObj)
		if widths, ok := e.reader.Resolve(obj["/Widths"]).(ArrayObject); ok {
			for i, wObj := range widths {
				if w, ok := wObj.(NumberObject); ok {
					f.Widths[first+i] = float64(w)
				}
			}
		}
	} else {
		// TODO: Handle CIDFonts (Type0) /DescendantFonts which use /W array
		// For now, we leave Widths empty, handleText will fallback to heuristic
		f.IsCID = true
	}

	// 3. Determine Space Width (Try char 32, else 250 default)
	if w, ok := f.Widths[32]; ok {
		f.SpaceWidth = w
	} else {
		f.SpaceWidth = 250.0 // Standard PDF default
	}

	// 4. Parse ToUnicode CMap
	if toUnicode, ok := e.reader.Resolve(obj["/ToUnicode"]).(StreamObject); ok {
		if cmap, err := ParseCMap(toUnicode.Data); err == nil {
			f.CMap = cmap
		} else {
			f.CMap = NewCMap()
		}
	} else {
		f.CMap = NewCMap() // Empty map, will fallback to encoding
		// Check if there's an Encoding dictionary
		if enc, ok := obj["/Encoding"]; ok {
			e.parseEncoding(f, enc)
		}
	}

	return f
}

// parseEncoding parses the /Encoding dictionary and populates the font's encoding map
func (e *Extractor) parseEncoding(f *Font, encObj Object) {
	resolved := e.reader.Resolve(encObj)

	// Handle NameObject (built-in encodings like /WinAnsiEncoding, /MacRomanEncoding)
	if _, ok := resolved.(NameObject); ok {
		// TODO: Could load standard encoding tables here
		return
	}

	// Handle DictionaryObject with /Differences array
	encDict, ok := resolved.(DictionaryObject)
	if !ok {
		return
	}

	// Parse /Differences array
	// Format: [code1 /name1 /name2 ... code2 /name3 ...]
	// Numbers set the current code, names assign to sequential codes
	if diff, ok := e.reader.Resolve(encDict["/Differences"]).(ArrayObject); ok {
		currentCode := 0
		for _, item := range diff {
			if num, ok := item.(NumberObject); ok {
				// Number sets the current code
				currentCode = int(num)
			} else if name, ok := item.(NameObject); ok {
				// Name assigns to current code, then increment
				glyphName := string(name)
				f.Encoding[currentCode] = glyphName
				currentCode++
			}
		}
	}
}

// ExtractText is the main entry point.
func (e *Extractor) ExtractText() (string, error) {
	contents := e.reader.Resolve(e.page["/Contents"])
	var streams []StreamObject

	if arr, ok := contents.(ArrayObject); ok {
		for _, ref := range arr {
			if s, ok := e.reader.Resolve(ref).(StreamObject); ok {
				streams = append(streams, s)
			}
		}
	} else if s, ok := contents.(StreamObject); ok {
		streams = append(streams, s)
	}

	for _, stream := range streams {
		parser := NewContentStreamParser(stream.Data)
		for {
			op, err := parser.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			e.processOp(*op)
		}
	}

	return e.buffer.String(), nil
}

func (e *Extractor) processOp(op Operation) {
	switch op.Operator {
	case "q":
		e.gStack = append(e.gStack, e.gState)
	case "Q":
		if len(e.gStack) > 0 {
			e.gState = e.gStack[len(e.gStack)-1]
			e.gStack = e.gStack[:len(e.gStack)-1]
		}
	case "cm":
		if len(op.Operands) == 6 {
			m := argsToMatrix(op.Operands)
			e.gState.CTM = m.Mult(e.gState.CTM)
		}
	case "BT":
		e.textState.TM = IdentityMatrix()
		e.textState.TLM = IdentityMatrix()
	case "Tc":
		e.textState.CharSpacing = number(op.Operands[0])
	case "Tw":
		e.textState.WordSpacing = number(op.Operands[0])
	case "Tz":
		e.textState.Scale = number(op.Operands[0])
	case "TL":
		e.textState.Leading = number(op.Operands[0])
	case "Tf":
		if name, ok := op.Operands[0].(NameObject); ok {
			if font, ok := e.fonts[string(name)]; ok {
				e.textState.Font = font
			}
		}
		e.textState.FontSize = number(op.Operands[1])
	case "Td":
		tx, ty := number(op.Operands[0]), number(op.Operands[1])
		m := Matrix{1, 0, 0, 1, tx, ty}
		e.textState.TLM = m.Mult(e.textState.TLM)
		e.textState.TM = e.textState.TLM
	case "TD":
		tx, ty := number(op.Operands[0]), number(op.Operands[1])
		e.textState.Leading = -ty
		m := Matrix{1, 0, 0, 1, tx, ty}
		e.textState.TLM = m.Mult(e.textState.TLM)
		e.textState.TM = e.textState.TLM
	case "Tm":
		if len(op.Operands) == 6 {
			e.textState.TM = argsToMatrix(op.Operands)
			e.textState.TLM = e.textState.TM
		}
	case "T*":
		m := Matrix{1, 0, 0, 1, 0, -e.textState.Leading}
		e.textState.TLM = m.Mult(e.textState.TLM)
		e.textState.TM = e.textState.TLM
	case "Tj":
		if len(op.Operands) > 0 {
			e.handleText(op.Operands[0])
		}
	case "TJ":
		if arr, ok := op.Operands[0].(ArrayObject); ok {
			for _, obj := range arr {
				if numObj, ok := obj.(NumberObject); ok {
					// Adjustment: -num/1000 * fontsize * scale
					shift := -float64(numObj) / 1000.0 * e.textState.FontSize * (e.textState.Scale / 100.0)
					e.textState.TM[4] += shift * e.textState.TM[0]
					e.textState.TM[5] += shift * e.textState.TM[1]
				} else {
					e.handleText(obj)
				}
			}
		}
	case "'":
		e.processOp(Operation{Operator: "T*"})
		e.processOp(Operation{Operator: "Tj", Operands: op.Operands})
	case "\"":
		e.textState.WordSpacing = number(op.Operands[0])
		e.textState.CharSpacing = number(op.Operands[1])
		e.processOp(Operation{Operator: "T*"})
		e.processOp(Operation{Operator: "Tj", Operands: op.Operands[2:]})
	case "INLINE_IMAGE":
		// Handle inline image placeholder
		if len(op.Operands) > 0 {
			if dict, ok := op.Operands[0].(DictionaryObject); ok {
				e.recordInlineImage(dict)
			}
		}
	case "Do":
		// Handle XObject (image) reference
		if len(op.Operands) > 0 {
			if name, ok := op.Operands[0].(NameObject); ok {
				e.recordImage(string(name))
			}
		}
	}
}

// handleText calculates position using REAL font metrics if possible
func (e *Extractor) handleText(obj Object) {
	var rawBytes []byte
	switch o := obj.(type) {
	case StringObject:
		rawBytes = []byte(o)
	case HexStringObject:
		rawBytes = []byte(o)
	default:
		return
	}

	// 1. Calculate precise text width (in unscaled text space units)
	// We need this BEFORE layout check to know where the string *should* start relative to lastX.
	// Actually, lastX is where the PREVIOUS string ended.
	// e.textState.TM contains the start position of THIS string.
	// So we can check the gap immediately.

	fm := e.textState.TM.Mult(e.gState.CTM)
	x, y := fm[4], fm[5]

	// 2. Detect Spacing
	// Calculate dynamic threshold based on space width
	spaceWidth := 0.0
	if e.textState.Font != nil {
		// Convert font units (1/1000) to user space
		spaceWidth = (e.textState.Font.SpaceWidth / 1000.0) * e.textState.FontSize * (e.textState.Scale / 100.0)
	}

	// If we don't have metrics, assume 0.2em threshold (small safe gap)
	threshold := e.textState.FontSize * 0.2
	if spaceWidth > 0 {
		threshold = spaceWidth * 0.5 // Trigger if gap is > 50% of a space
	}

	if math.Abs(y-e.lastY) > (e.textState.FontSize * 0.5) {
		if e.buffer.Len() > 0 {
			e.buffer.WriteString("\n")
		}
	} else {
		gap := x - e.lastX
		// Use threshold check
		if gap > threshold {
			if e.buffer.Len() > 0 && !strings.HasSuffix(e.buffer.String(), "\n") && !strings.HasSuffix(e.buffer.String(), " ") {
				e.buffer.WriteString(" ")
			}
		}
	}

	// 3. Decode Text
	var decoded strings.Builder
	if e.textState.Font != nil && e.textState.Font.CMap != nil && len(e.textState.Font.CMap.Map) > 0 {
		i := 0
		for i < len(rawBytes) {
			// Try 2 bytes
			if i+1 < len(rawBytes) {
				key := string(rawBytes[i : i+2])
				if val, ok := e.textState.Font.CMap.Map[key]; ok {
					decoded.WriteString(val)
					i += 2
					continue
				}
			}
			// Try 1 byte
			key := string(rawBytes[i : i+1])
			if val, ok := e.textState.Font.CMap.Map[key]; ok {
				decoded.WriteString(val)
				i++
				continue
			}
			// Fallback
			decoded.WriteByte(rawBytes[i])
			i++
		}
	} else if e.textState.Font != nil && len(e.textState.Font.Encoding) > 0 {
		// Use /Encoding dictionary to map character codes to glyphs
		for _, b := range rawBytes {
			code := int(b)
			if glyphName, ok := e.textState.Font.Encoding[code]; ok {
				// Map glyph name to Unicode
				if unicode, ok := glyphToUnicode[glyphName]; ok {
					decoded.WriteString(unicode)
				} else {
					// Unknown glyph, try to extract character from name
					// e.g., "/a" -> 'a'
					if len(glyphName) == 2 && glyphName[0] == '/' {
						decoded.WriteByte(glyphName[1])
					} else {
						// Fallback: use the byte value as-is
						decoded.WriteByte(b)
					}
				}
			} else {
				// No encoding entry, use byte value as-is (standard ASCII)
				decoded.WriteByte(b)
			}
		}
	} else {
		// No CMap and no Encoding - fallback to direct byte conversion
		// Filter out non-printable control characters
		decoded.WriteString(filterControlChars(rawBytes))
	}

	e.buffer.WriteString(decoded.String())

	// 4. Calculate total width of this string to update lastX
	totalWidth := 0.0

	if e.textState.Font != nil && len(e.textState.Font.Widths) > 0 {
		// Use Widths Map
		for _, b := range rawBytes {
			code := int(b)
			w := e.textState.Font.MissingW
			if val, ok := e.textState.Font.Widths[code]; ok {
				w = val
			}
			// Add width + char spacing + word spacing (if space)
			totalWidth += w

			// Note: This simple loop assumes 1-byte char codes for widths.
			// Complex CID fonts are harder, but this covers standard pdfTeX.
		}
		// Convert to user space
		// width = (sum(w)/1000 * fs + charSpacing + wordSpacing) * scale
		// Simplified: we sum the raw widths first.
		totalWidth = (totalWidth / 1000.0) * e.textState.FontSize

		// Add CharSpacing * count
		totalWidth += float64(len(rawBytes)) * e.textState.CharSpacing

		// Add WordSpacing (approximation: count spaces in decoded)
		// Better: check raw code 32, but decoded is safer for generic check
		decodedStr := decoded.String()
		spaceCount := strings.Count(decodedStr, " ")
		totalWidth += float64(spaceCount) * e.textState.WordSpacing

		totalWidth *= (e.textState.Scale / 100.0)

	} else {
		// Fallback Heuristic (0.5 em per char)
		totalWidth = float64(decoded.Len()) * e.textState.FontSize * 0.5 * (e.textState.Scale / 100.0)
	}

	e.lastX = x + totalWidth
	e.lastY = y

	// Update TM
	e.textState.TM[4] += totalWidth * e.textState.TM[0]
	e.textState.TM[5] += totalWidth * e.textState.TM[1]
}

// recordInlineImage records an inline image placeholder
func (e *Extractor) recordInlineImage(dict DictionaryObject) {
	img := model.Image{
		Type: "inline_image",
		Rect: e.calculateImageRect(),
	}

	// Extract metadata from inline image dictionary
	if w, ok := dict["/W"].(NumberObject); ok {
		img.Width = float64(w)
	} else if w, ok := dict["/Width"].(NumberObject); ok {
		img.Width = float64(w)
	}

	if h, ok := dict["/H"].(NumberObject); ok {
		img.Height = float64(h)
	} else if h, ok := dict["/Height"].(NumberObject); ok {
		img.Height = float64(h)
	}

	if cs, ok := dict["/CS"].(NameObject); ok {
		img.ColorSpace = string(cs)
	} else if cs, ok := dict["/ColorSpace"].(NameObject); ok {
		img.ColorSpace = string(cs)
	}

	e.images = append(e.images, img)
}

// recordImage records an XObject image reference
func (e *Extractor) recordImage(name string) {
	if e.xobjects == nil {
		return
	}

	// Resolve XObject to get metadata
	xobj := e.reader.Resolve(e.xobjects[name])
	xobjDict, ok := xobj.(DictionaryObject)
	if !ok {
		return
	}

	// Check if it's actually an image (not a form XObject)
	if subtype, ok := e.reader.Resolve(xobjDict["/Subtype"]).(NameObject); ok {
		if string(subtype) != "/Image" {
			return // Not an image, skip
		}
	}

	img := model.Image{
		Type: "image",
		ID:   name,
		Rect: e.calculateImageRect(),
	}

	// Extract image metadata
	if w, ok := e.reader.Resolve(xobjDict["/Width"]).(NumberObject); ok {
		img.Width = float64(w)
	}
	if h, ok := e.reader.Resolve(xobjDict["/Height"]).(NumberObject); ok {
		img.Height = float64(h)
	}
	if cs, ok := e.reader.Resolve(xobjDict["/ColorSpace"]).(NameObject); ok {
		img.ColorSpace = string(cs)
	}

	e.images = append(e.images, img)
}

// calculateImageRect calculates the bounding box of an image using current CTM
func (e *Extractor) calculateImageRect() []float64 {
	// In PDF, images are drawn in a unit square (0,0) to (1,1)
	// The CTM transforms this to the actual position/size on the page
	ctm := e.gState.CTM

	// Transform corners of unit square
	x := ctm[4]
	y := ctm[5]
	width := math.Sqrt(ctm[0]*ctm[0] + ctm[1]*ctm[1])
	height := math.Sqrt(ctm[2]*ctm[2] + ctm[3]*ctm[3])

	return []float64{x, y, width, height}
}

// GetImages returns the images found on this page
func (e *Extractor) GetImages() []model.Image {
	return e.images
}

// Helpers

func number(o Object) float64 {
	if n, ok := o.(NumberObject); ok {
		return float64(n)
	}
	return 0
}

func argsToMatrix(args []Object) Matrix {
	return Matrix{
		number(args[0]), number(args[1]),
		number(args[2]), number(args[3]),
		number(args[4]), number(args[5]),
	}
}
