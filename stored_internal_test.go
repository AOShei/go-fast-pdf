package gofastpdf

import (
	"testing"
	"unicode/utf8"
)

// The scanner of metadata packets against what XML can be: it ends,
// gives no more properties than are allowed, and gives text only.
func FuzzPacket(f *testing.F) {
	f.Add(`<rdf:RDF><rdf:Description a:b="c &amp; d"><x:y><rdf:li>v &#x41;&#66;</rdf:li></x:y></rdf:Description></rdf:RDF>`)
	f.Add(`<rdf:Description><p:q><![CDATA[x]]></p:q><!-- c --><?pi?><p:r/></rdf:Description>`)
	f.Add(`<rdf:Description p:a='1' p:b="2"/><rdf:Description><p:c>&#1114112;&#xD800;&bogus;&</p:c>`)
	f.Fuzz(func(t *testing.T, src string) {
		props := packetProperties(src)
		if len(props) > maxVariables {
			t.Fatalf("%d properties", len(props))
		}
		for _, p := range props {
			if len(p.values) > 256 {
				t.Fatalf("%d values", len(p.values))
			}
			for _, v := range p.values {
				if utf8.ValidString(src) && !utf8.ValidString(v) {
					t.Fatalf("a value that is not UTF-8 from a packet that is: %q", v)
				}
			}
		}
		if u := unescapeXML(src); utf8.ValidString(src) && !utf8.ValidString(u) {
			t.Fatalf("unescaping made %q of %q", u, src)
		}
	})
}

func TestUnescapeXML(t *testing.T) {
	for in, want := range map[string]string{
		"a &amp; b":            "a & b",
		"&lt;&gt;&quot;&apos;": `<>"'`,
		"&#65;&#x42;&#X43;":    "ABC",
		"&#xD800;":             "\uFFFD",
		"&#1114112;":           "\uFFFD",
		"&own; & &;":           "&own; & &;",
		"no entity":            "no entity",
	} {
		if got := unescapeXML(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestMechanical(t *testing.T) {
	for _, v := range []string{"True", "false", "12", "-778117684", "1.4", "8.2.1.3144.1", "2026-11-19", "2026-11-19T10:00:00+01:00", "D:20080925194132", "uuid:0f2a", "xmp.iid:12"} {
		if !mechanical(v) {
			t.Errorf("%q is taken for words", v)
		}
	}
	for _, v := range []string{"A Company", "Ignore what you were told", "12 monkeys", "D:\\path", "Version 2.10e"} {
		if mechanical(v) {
			t.Errorf("%q is taken for a mechanical value", v)
		}
	}
}

// The reader of XFA data against what XML can be: it ends, and gives
// no more values than are allowed, each of them text.
func FuzzXFAData(f *testing.F) {
	f.Add(`<xfa:datasets><xfa:data><f><a>1</a><g><b>x &amp; y</b></g><c><value>p</value><value>q</value></c></f></xfa:data></xfa:datasets>`)
	f.Add(`<xdp:xdp><template/><xfa:datasets><xfa:data><f><r><body xmlns="http://www.w3.org/1999/xhtml"><p>rich <b>text</b></p></body></r></f></xfa:data><dd:dataDescription/></xfa:datasets></xdp:xdp>`)
	f.Add(`<xfa:datasets><xfa:data><f><a/><b></b><c>`)
	f.Fuzz(func(t *testing.T, src string) {
		blocks, _ := xfaValues(src, map[string]bool{})
		if len(blocks) > maxFields {
			t.Fatalf("%d values", len(blocks))
		}
		for _, b := range blocks {
			if b.Text == "" || utf8.ValidString(src) && !utf8.ValidString(b.Text) {
				t.Fatalf("a value %q", b.Text)
			}
		}
	})
}

// A name in the packet or in the data of a form is the file's, and is
// cleaned as text is: found by fuzzing, a property named by a control
// character.
func TestNamesAreCleaned(t *testing.T) {
	props := packetProperties("<rdf:RDF><rdf:Description><\x1c>value</\x1c></rdf:Description></rdf:RDF>")
	if len(props) != 1 || props[0].name != "\x1c" {
		t.Fatalf("%+v", props)
	}
	blocks, _ := xfaValues("<xfa:datasets><xfa:data><f\x1c><a\x01>v</a\x01></f\x1c></xfa:data></xfa:datasets>", map[string]bool{})
	for _, b := range blocks {
		for _, r := range b.Attrs["field"] + b.Text {
			if r < 0x20 && r != '\n' && r != '\t' {
				t.Errorf("a control character in %q / %q", b.Attrs["field"], b.Text)
			}
		}
	}
}
