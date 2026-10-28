package summary

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Eyevinn/mp4lens/internal/boxview"
)

// summaryOf returns the summary of a file in testdata.
func summaryOf(t *testing.T, name string) *Summary {
	t.Helper()
	data, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	r := bytes.NewReader(data)
	return Of(boxview.Decode(r, int64(len(data)), ""), r)
}

// value returns the value of the row called name in the first section whose title starts with title.
func value(t *testing.T, s *Summary, title, name string) string {
	t.Helper()
	for _, sec := range s.Sections {
		if !strings.HasPrefix(sec.Title, title) {
			continue
		}
		for _, r := range sec.Rows {
			if r.Name == name {
				return r.Value
			}
		}
		t.Errorf("section %s has no row %s: %+v", sec.Title, name, sec.Rows)
		return ""
	}
	t.Errorf("no section %s in\n%s", title, s.Text())
	return ""
}

func TestSummary(t *testing.T) {
	testCases := []struct {
		file, section, row, want string
	}{
		{"chapters_subs.m4a", "File", "structure", "moov after mdat"},
		{"chapters_subs.m4a", "Movie", "duration", "1s (1000 at time scale 1000)"},
		{"chapters_subs.m4a", "Metadata", "Title", "Chapters"},
		{"chapters_subs.m4a", "Metadata", "Track Number", "track 5 of 13"},
		{"chapters_subs.m4a", "Chapters (chpl)", "0:00:00.500", "Second chapter"},
		{"chapters_subs.m4a", "Chapters (track 4)", "0:00:00.500", "Second chapter"},
		{"chapters_subs.m4a", "Track 1: audio", "codec", "mp4a.40.2"},
		{"chapters_subs.m4a", "Track 1: audio", "audio", "AAC LC, 48000 Hz, 1 channel"},
		{"chapters_subs.m4a", "Track 1: audio", "samples", "48, 46.9974 per second, all sync"},
		{"chapters_subs.m4a", "Track 1: audio", "edit list", "from media time 1024 (21.333ms) for 1s"},
		{"chapters_subs.m4a", "Track 2: subtitles", "font", "Arial"},
		{"chapters_subs.m4a", "Track 4: text", "chapters", "chapter titles of tracks 1, 2"},
		{"chapters_subs.m4a", "Metadata", "Cover Art", "PNG image, 531 bytes"},
		{"init_cenc.cmfv", "File", "structure", "init segment of a fragmented file, with no fragments"},
		{"init_cenc.cmfv", "Track 1: video", "codec", "avc3.64001E"},
		{"init_cenc.cmfv", "Track 1: video", "encryption",
			"scheme cenc, original format avc3, default KID f057639d-9287-3315-8bf5-50999c4945f7, IV of 8 bytes per sample"},
		{"hvc1_init.mp4", "Track 1: video", "codec", "hvc1.1.6.L63.90"},
		{"av1_init.mp4", "Track 1: video", "codec", "av01.0.00M.08"},
		{"bbb5s_aac_sidx.mp4", "Track 3: audio", "samples",
			"235, 46.875 per second, each 1024 ticks, all sync, in 3 track fragments"},
		{"1.m4s", "File", "structure", "media segment: 1 fragment without moov"},
		{"1.m4s", "Track 2 (fragments only)", "samples", "60, each 3000 ticks, 2 sync, in 1 track fragment"},
		{"qt_h264_lpcm.mov", "Track 2: audio", "audio", "48000 Hz, 6 channels, 16 bits"},
	}
	cache := map[string]*Summary{}
	for _, tc := range testCases {
		s := cache[tc.file]
		if s == nil {
			s = summaryOf(t, tc.file)
			cache[tc.file] = s
		}
		if got := value(t, s, tc.section, tc.row); got != tc.want {
			t.Errorf("%s %s %s: got %q, want %q", tc.file, tc.section, tc.row, got, tc.want)
		}
	}
}

func TestSummaryKeys(t *testing.T) {
	data, err := os.ReadFile("../../testdata/chapters_subs.m4a")
	if err != nil {
		t.Fatal(err)
	}
	r := bytes.NewReader(data)
	tr := boxview.Decode(r, int64(len(data)), "")
	s := Of(tr, r)
	for _, sec := range s.Sections {
		for _, row := range sec.Rows {
			if row.Key != "" && tr.ByKey(row.Key) == nil {
				t.Errorf("%s %s: no box with key %s", sec.Title, row.Name, row.Key)
			}
		}
	}
	if n := tr.ByKey(s.Sections[len(s.Sections)-1].Key); n == nil || n.Type != "trak" {
		t.Errorf("the last section is not about a trak: %+v", n)
	}
}

func TestClockAndSampleText(t *testing.T) {
	if got := clock(3723456789000); got != "1:02:03.457" {
		t.Errorf("clock: %s", got)
	}
	if got := sampleText([]byte{0, 6, 'a', '\n', '\t', 'b', 'x', 'y', 'z'}); got != "a bxy" {
		t.Errorf("text: %q", got)
	}
	if got := sampleText([]byte{0, 6, 0xfe, 0xff, 0, 'h', 0, 'i'}); got != "hi" {
		t.Errorf("UTF-16 text: %q", got)
	}
}

func TestImages(t *testing.T) {
	data, err := os.ReadFile("../../testdata/chapters_subs.m4a")
	if err != nil {
		t.Fatal(err)
	}
	s := summaryOf(t, "chapters_subs.m4a")
	for _, sec := range s.Sections {
		for _, r := range sec.Rows {
			if r.Name != "Cover Art" {
				continue
			}
			img := r.Image
			if img == nil || img.Type != "image/png" || img.Size != 531 ||
				!bytes.HasPrefix(data[img.Pos:img.Pos+img.Size], []byte("\x89PNG")) {
				t.Errorf("cover art image: %+v", img)
			}
			return
		}
	}
	t.Error("no cover art")
}

func TestSignatureImages(t *testing.T) {
	// A file with a camera-like thumbnail box in udta, with a PNG after 4 bytes, as after a version and flags.
	cover := summaryOf(t, "chapters_subs.m4a")
	for _, sec := range cover.Sections {
		if sec.Title == "Images" {
			t.Errorf("chapters_subs.m4a has an Images section: %+v", sec.Rows)
		}
	}
	src, err := os.ReadFile("../../testdata/chapters_subs.m4a")
	if err != nil {
		t.Fatal(err)
	}
	png := src[bytes.Index(src, []byte("\x89PNG")):]
	png = png[:bytes.Index(png, []byte("IEND"))+8]
	box := func(typ string, payload ...[]byte) []byte {
		b := binary.BigEndian.AppendUint32(nil, uint32(8+len(bytes.Join(payload, nil))))
		return append(append(b, typ...), bytes.Join(payload, nil)...)
	}
	data := append(box("ftyp", []byte("isom\x00\x00\x00\x00isom")),
		box("moov", box("udta", box("thmb", []byte{0, 0, 0, 0}, png)))...)
	r := bytes.NewReader(data)
	s := Of(boxview.Decode(r, int64(len(data)), ""), r)
	want := fmt.Sprintf("PNG image, %d bytes", len(png))
	if got := value(t, s, "Images", "moov/udta/thmb"); got != want {
		t.Fatalf("thmb: %q, want %q", got, want)
	}
	img := s.Sections[len(s.Sections)-1].Rows[0].Image
	if img == nil || img.Type != "image/png" || !bytes.Equal(data[img.Pos:img.Pos+img.Size], png) {
		t.Errorf("thmb image: %+v", img)
	}
}
