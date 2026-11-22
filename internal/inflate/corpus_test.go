package inflate

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/AOShei/go-fast-pdf/internal/pdftest"
)

// corpusStreams are the zlib streams of the files of the tuning set of
// the public corpus, as the cache has them, with the size of each one's
// output: the streams of real PDF files, mostly page content. A stream
// is found by the keyword that starts it and a valid zlib header after
// it, and runs to the next endstream; only those that decompress whole
// are kept. Images are left out, as reading a file's text does not
// decompress them. Nil when the corpus has not been fetched.
var corpusStreams = sync.OnceValues(func() (streams [][]byte, sizes []int) {
	cache := os.Getenv("GOFASTPDF_CACHE")
	if cache == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, nil
		}
		cache = filepath.Join(base, "gofastpdf-corpus")
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "corpus-public.json"))
	if err != nil {
		return nil, nil
	}
	var manifest []struct {
		Name, Set string
	}
	if json.Unmarshal(raw, &manifest) != nil {
		return nil, nil
	}
	for _, f := range manifest {
		if f.Set != "tune" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cache, filepath.FromSlash(f.Name)))
		if err != nil {
			continue
		}
		for at := 0; ; {
			i := bytes.Index(data[at:], []byte("stream"))
			if i < 0 {
				break
			}
			at += i + len("stream")
			if at < 9 || string(data[at-9:at]) == "endstream" {
				continue
			}
			// The dictionary of the stream: from its object's start.
			dict := data[max(0, at-2048):at]
			if o := bytes.LastIndex(dict, []byte(" obj")); o >= 0 {
				dict = dict[o:]
			}
			if bytes.Contains(dict, []byte("/Image")) {
				continue
			}
			start := at
			if start < len(data) && data[start] == '\r' {
				start++
			}
			if start < len(data) && data[start] == '\n' {
				start++
			}
			end := bytes.Index(data[start:], []byte("endstream"))
			if end < 0 {
				break
			}
			src := data[start : start+end]
			out, err := Zlib(nil, src, 1<<28)
			if err != nil {
				continue
			}
			streams = append(streams, src)
			sizes = append(sizes, len(out))
		}
	}
	return streams, sizes
})

// BenchmarkZlibCorpus decompresses every zlib stream of the tuning set
// once per run. Its MB/s are of output.
func BenchmarkZlibCorpus(b *testing.B) {
	streams, sizes := corpusStreams()
	if len(streams) == 0 {
		b.Skip("no public corpus in the cache; run testdata/fetch.sh")
	}
	total, most := 0, 0
	for _, n := range sizes {
		total += n
		most = max(most, n)
	}
	b.SetBytes(int64(total))
	dst := make([]byte, 0, most+fastRoom)
	defer pdftest.BenchInstructions(b)()
	for b.Loop() {
		for _, src := range streams {
			if _, err := Zlib(dst, src, 1<<28); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportMetric(float64(len(streams)), "streams")
}

// The streams of real files give what compress/zlib gives.
func TestCorpusLikeCompressZlib(t *testing.T) {
	streams, _ := corpusStreams()
	if len(streams) == 0 {
		t.Skip("no public corpus in the cache; run testdata/fetch.sh")
	}
	if testing.Short() {
		t.Skip("the corpus is skipped with -short")
	}
	for i, src := range streams {
		got, err := Zlib(nil, src, 1<<28)
		zr, zerr := zlib.NewReader(bytes.NewReader(src))
		if zerr != nil {
			t.Fatalf("stream %d: compress/zlib: %v", i, zerr)
		}
		want, zerr := io.ReadAll(zr)
		if err != nil || zerr != nil || !bytes.Equal(got, want) {
			t.Fatalf("stream %d: %d bytes, %v; compress/zlib %d bytes, %v", i, len(got), err, len(want), zerr)
		}
	}
	t.Logf("%d streams", len(streams))
}
