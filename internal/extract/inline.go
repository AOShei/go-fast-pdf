package extract

import (
	"github.com/AOShei/go-fast-pdf/internal/content"
	"github.com/AOShei/go-fast-pdf/internal/syntax"
)

// Inline images.
//
// An image can be written into a content stream, between BI and EI: its
// dictionary, then its data after ID. The dictionary may abbreviate its
// keys (W for Width, BPC for BitsPerComponent), the names of colour
// spaces (G for DeviceGray) and of filters (Fl for FlateDecode), and may
// name a colour space of the resources in force. Its entries are written
// out here, when asked for (Options.InlineDicts), so that the image reads
// as an image XObject does, and the colour space it names is looked up.
// The data is copied when it is asked for (Options.InlineData): the
// content stream it is in is not kept.

// inlineKeys are the abbreviated keys of an inline image's dictionary.
var inlineKeys = map[string]string{
	"BPC": "BitsPerComponent", "CS": "ColorSpace", "D": "Decode", "DP": "DecodeParms",
	"F": "Filter", "H": "Height", "IM": "ImageMask", "I": "Interpolate", "W": "Width",
	"L": "Length",
}

// inlineSpaces are the abbreviated names of colour spaces.
var inlineSpaces = map[string]string{"G": "DeviceGray", "RGB": "DeviceRGB", "CMYK": "DeviceCMYK", "I": "Indexed"}

// maxInlineDict bounds the bytes of an inline image's dictionary that
// are read.
const maxInlineDict = 64 << 10

// inlineImage records an inline image. Its size is read from the
// operands; its dictionary and data from the scanner.
func (in *interp) inlineImage(scan *content.Scanner, args []content.Operand) {
	img := Image{Inline: true}
	for i := 0; i+1 < len(args); i += 2 {
		key, val := args[i], args[i+1]
		if key.Kind != content.Name {
			continue
		}
		switch string(key.Raw) {
		case "W", "Width":
			img.Width = val.Float()
		case "H", "Height":
			img.Height = val.Float()
		}
	}
	if in.opts.InlineDicts || in.opts.InlineData {
		img.Dict = in.inlineDict(scan.InlineDict)
		if cs := img.Dict.Get("ColorSpace"); cs.Kind == syntax.Name {
			img.ColorSpace = "/" + cs.NameString()
		}
	}
	if in.opts.InlineData && scan.InlineData != nil {
		img.Data = append([]byte(nil), scan.InlineData...)
	}
	in.addImage(img)
}

// inlineDict reads the entries of an inline image's dictionary, raw, and
// writes them out.
func (in *interp) inlineDict(raw []byte) syntax.Value {
	if len(raw) > maxInlineDict {
		in.cut(boundInline, 1, 0)
		return syntax.Value{}
	}
	buf := make([]byte, 0, len(raw)+4)
	buf = append(append(append(buf, "<<"...), raw...), ">>"...)
	p := syntax.NewParser(buf, 0, 16)
	d, err := p.Object()
	if err != nil || d.Kind != syntax.Dict {
		return syntax.Value{}
	}
	for i := 0; i+1 < len(d.List); i += 2 {
		k, v := &d.List[i], &d.List[i+1]
		if k.Kind != syntax.Name {
			continue
		}
		if full, ok := inlineKeys[string(k.Data)]; ok {
			k.Data = []byte(full)
		}
		switch string(k.Data) {
		case "ColorSpace":
			*v = in.inlineSpace(*v)
		}
	}
	return d
}

// inlineSpace writes out the colour space of an inline image: an
// abbreviated name, or the first element and the base of an array
// ([/I /RGB 255 <...>]); another name is one of the resources.
func (in *interp) inlineSpace(v syntax.Value) syntax.Value {
	switch v.Kind {
	case syntax.Name:
		if full, ok := inlineSpaces[v.NameString()]; ok {
			return syntax.Value{Kind: syntax.Name, Data: []byte(full)}
		}
		switch v.NameString() {
		case "DeviceGray", "DeviceRGB", "DeviceCMYK", "Indexed", "Pattern":
			return v
		}
		if r := in.f.Resolve(in.spaceDict().Get(v.NameString())); !r.IsNull() {
			return r
		}
	case syntax.Array:
		list := append([]syntax.Value(nil), v.List...)
		for i := 0; i < len(list) && i < 2; i++ {
			if list[i].Kind == syntax.Name {
				if full, ok := inlineSpaces[list[i].NameString()]; ok {
					list[i] = syntax.Value{Kind: syntax.Name, Data: []byte(full)}
				} else if i == 1 {
					list[i] = in.inlineSpace(list[i])
				}
			}
		}
		v.List = list
	}
	return v
}
