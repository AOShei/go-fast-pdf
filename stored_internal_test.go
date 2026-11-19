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
