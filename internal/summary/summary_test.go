package summary

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"

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
		// The name of a QuickTime handler is a counted string.
		{"qt_h264_aac.mov", "Track 1: video", "handler", `vide "VideoHandler"`},
		{"init.mp4", "Track 2: video", "handler", `vide "MobiTV Video Media handler"`},
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

func TestAudioDetailsAAC(t *testing.T) {
	for _, c := range []struct {
		name   string
		config []byte
		want   string
	}{
		{"HE-AAC", []byte{0x2b, 0x11, 0x88, 0x00}, "SBR (HE-AAC), 48000 Hz (core 24000 Hz), 2 channels"},
		{"AAC LC with an SBR sync extension", []byte{0x13, 0x10, 0x56, 0xe5, 0x98},
			"SBR (HE-AAC), 48000 Hz (core 24000 Hz), 2 channels"},
		{"AAC LC with SBR and PS sync extensions", []byte{0x13, 0x10, 0x56, 0xe5, 0x9d, 0x48, 0x80},
			"PS (HE-AACv2), 48000 Hz (core 24000 Hz), 2 channels"},
		{"AAC Main", []byte{0x09, 0x90}, "AAC Main, 48000 Hz, 2 channels"},
	} {
		e := mp4.CreateAudioSampleEntryBox("mp4a", 2, 16, 48000, mp4.CreateEsdsBox(c.config))
		if got := audioDetails(e); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestImageItems(t *testing.T) {
	testCases := []struct {
		file, row, want string
	}{
		{"ex60.avif", "primary item", "item 1"},
		{"ex60.avif", "item 1", "av01.0.01M.08 600x338, 8 bits, 3 channels, primary image, 10235 bytes"},
		{"gradient_grid_alpha.heic", "item 3", "grid 1024x64, 1 row and 2 columns of tiles hvc1.3.E.L90.B0 " +
			"512x512 (items 1, 2, 1086 bytes), 8 bits, 3 channels, primaries 2, transfer 2, matrix 6 (BT.601 525), " +
			"full range, primary image"},
		{"gradient_grid_alpha.heic", "item 6", "grid 1024x64, 1 row and 2 columns of tiles hvc1.4.10.L90.BF.C8 " +
			"512x512 (items 4, 5, 348 bytes), alpha plane, 8 bits, 1 channel, hidden, auxiliary image of item 3"},
		{"gradient_grid_alpha.heic", "item 7", "Exif, hidden, metadata of item 3, 72 bytes"},
		{"gradient_grid_alpha.heic", "item 8", "mime application/rdf+xml, hidden, metadata of item 3, 572 bytes"},
	}
	for _, tc := range testCases {
		if got := value(t, summaryOf(t, tc.file), "Items", tc.row); got != tc.want {
			t.Errorf("%s %s:\n got %q\nwant %q", tc.file, tc.row, got, tc.want)
		}
	}
	heic := summaryOf(t, "gradient_grid_alpha.heic")
	if got := value(t, heic, "File", "structure"); got != "still image: 8 items in meta, no moov" {
		t.Errorf("structure: %q", got)
	}
	// A browser may show an AVIF or HEIC file as an image: the row of the primary item has the whole file.
	images := 0
	for _, sec := range heic.Sections {
		for _, r := range sec.Rows {
			if r.Name == "item 1" {
				t.Errorf("row %+v: tiles are in their grid", r)
			}
			if r.Image != nil {
				images++
				if r.Name != "item 3" || *r.Image != (Image{Pos: 0, Size: 3144, Type: "image/heic"}) {
					t.Errorf("HEIC image in row %+v: %+v", r, r.Image)
				}
			}
		}
	}
	if images != 1 {
		t.Errorf("%d HEIC images, want 1", images)
	}
	avif := summaryOf(t, "ex60.avif")
	if img := avif.Sections[1].Rows[1].Image; img == nil || *img != (Image{Pos: 0, Size: 10485, Type: "image/avif"}) {
		t.Errorf("AVIF image: %+v", img)
	}
}
