package pdf

import (
	"fmt"
	"math"
	"strings"
)

// Matrix is a 3x3 transform matrix (last row implicitly 0,0,1).
type Matrix [6]float64

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
}

func NewExtractor(r *Reader, page DictionaryObject) (*Extractor, error) {
	e := &Extractor{
		reader:    r,
		page:      page,
		gState:    GraphicsState{CTM: IdentityMatrix()},
		textState: NewTextState(),
		fonts:     make(map[string]*Font),
	}

	// Load Fonts from Resources
	if res, ok := r.Resolve(page["/Resources"]).(DictionaryObject); ok {
		if fonts, ok := r.Resolve(res["/Font"]).(DictionaryObject); ok {
			for name, ref := range fonts {
				fontObj := r.Resolve(ref).(DictionaryObject)
				e.fonts[name] = e.loadFont(fontObj)
			}
		}
	}

	return e, nil
}

// loadFont parses widths and ToUnicode maps
func (e *Extractor) loadFont(obj DictionaryObject) *Font {
	f := &Font{
		Widths:   make(map[int]float64),
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
		}
	} else {
		f.CMap = NewCMap() // Empty map, will fallback to ASCII
	}

	return f
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
		ops, err := parser.Parse()
		if err != nil {
			return "", err
		}
		for _, op := range ops {
			e.processOp(op)
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
	decoded := ""
	if e.textState.Font != nil && e.textState.Font.CMap != nil && len(e.textState.Font.CMap.Map) > 0 {
		i := 0
		for i < len(rawBytes) {
			// Try 2 bytes
			if i+1 < len(rawBytes) {
				key := fmt.Sprintf("%04X", (int(rawBytes[i])<<8)|int(rawBytes[i+1]))
				if val, ok := e.textState.Font.CMap.Map[key]; ok {
					decoded += val
					i += 2
					continue
				}
			}
			// Try 1 byte
			key := fmt.Sprintf("%04X", rawBytes[i])
			if val, ok := e.textState.Font.CMap.Map[key]; ok {
				decoded += val
				i++
				continue
			}
			// Fallback
			decoded += string(rawBytes[i])
			i++
		}
	} else {
		decoded = string(rawBytes)
	}

	e.buffer.WriteString(decoded)

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
		spaceCount := strings.Count(decoded, " ")
		totalWidth += float64(spaceCount) * e.textState.WordSpacing

		totalWidth *= (e.textState.Scale / 100.0)

	} else {
		// Fallback Heuristic (0.5 em per char)
		totalWidth = float64(len(decoded)) * e.textState.FontSize * 0.5 * (e.textState.Scale / 100.0)
	}

	e.lastX = x + totalWidth
	e.lastY = y

	// Update TM
	e.textState.TM[4] += totalWidth * e.textState.TM[0]
	e.textState.TM[5] += totalWidth * e.textState.TM[1]
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
