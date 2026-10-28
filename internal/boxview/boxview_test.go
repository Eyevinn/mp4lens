package boxview

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
)

// decodeFile decodes a file in testdata, changed by edit if it is not nil.
func decodeFile(t *testing.T, name string, edit func(data []byte)) (*Tree, *bytes.Reader) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(data)
	}
	r := bytes.NewReader(data)
	return Decode(r, int64(len(data)), ""), r
}

// corruptStsz sets the sample count of the stsz box to 1, without adding a sample size.
func corruptStsz(data []byte) {
	pos := bytes.Index(data, []byte("stsz")) - 4
	binary.BigEndian.PutUint32(data[pos+16:], 1)
}

func byPath(t *testing.T, tr *Tree, path string) *Node {
	t.Helper()
	for _, n := range tr.Nodes {
		if n.Path() == path {
			return n
		}
	}
	t.Fatalf("no box %s", path)
	return nil
}

func TestTree(t *testing.T) {
	tr, _ := decodeFile(t, "init.mp4", nil)
	if tr.ReadErr != nil {
		t.Fatal(tr.ReadErr)
	}
	if len(tr.Boxes) != 3 || len(tr.Nodes) != 26 { // 24 boxes, and the SPS and PPS of avcC
		t.Fatalf("got %d top-level boxes and %d nodes, want 3 and 26", len(tr.Boxes), len(tr.Nodes))
	}
	stsz := byPath(t, tr, "moov/trak/mdia/minf/stbl/stsz")
	if stsz.Key != "2/1/1/2/2/3" || stsz.Depth != 5 || stsz.Pos != 639 || stsz.Parent.Name() != "stbl" {
		t.Errorf("stsz: key=%s depth=%d pos=%d", stsz.Key, stsz.Depth, stsz.Pos)
	}
	if got := tr.ByKey(stsz.Key); got != stsz {
		t.Errorf("ByKey(%s) = %v", stsz.Key, got)
	}
	for _, key := range []string{"", "3", "2/9", "x", "-1"} {
		if got := tr.ByKey(key); got != nil {
			t.Errorf("ByKey(%q) = %s, want nil", key, got.Path())
		}
	}
	if got := stsz.LongName(); got != "Sample Size" {
		t.Errorf("LongName = %q", got)
	}
	if tr.Nodes[0] != tr.Boxes[0] || tr.Nodes[3] != tr.Boxes[2].Children[0] {
		t.Error("Nodes are not in file order")
	}
}

func TestAt(t *testing.T) {
	tr, _ := decodeFile(t, "init.mp4", nil)
	cases := []struct {
		pos  uint64
		want string
	}{
		{0, "ftyp"},
		{31, "ftyp"},
		{32, "skip"},
		{69, "moov"},
		{640, "moov/trak/mdia/minf/stbl/stsz"},
		{560, "moov/trak/mdia/minf/stbl/stsd/avc1/avcC"},
		{714, "moov/mvex/trex"},
		{715, ""},
	}
	for _, c := range cases {
		got := ""
		if n := tr.At(c.pos); n != nil {
			got = n.Path()
		}
		if got != c.want {
			t.Errorf("At(%d) = %q, want %q", c.pos, got, c.want)
		}
	}
}

func TestFields(t *testing.T) {
	tr, _ := decodeFile(t, "init.mp4", nil)
	tkhd := byPath(t, tr, "moov/trak/tkhd")
	v, f, ok := tkhd.VersionFlags()
	if !ok || v != 0 || f != 7 {
		t.Errorf("VersionFlags = %d, %x, %t", v, f, ok)
	}
	fields := tkhd.Fields()
	if len(fields) != 5 || fields[0] != (Field{Key: "trackID", Value: "2"}) {
		t.Errorf("Fields = %+v", fields)
	}
	if _, _, ok := tr.Boxes[0].VersionFlags(); ok {
		t.Error("ftyp has version and flags")
	}
	bn := *tkhd.Node
	n := &Node{Node: &bn}
	n.Info = []string{"[test] size=8", " - a: 1", "   - b: 2", " raw line", "   - c"}
	want := []Field{{Key: "a", Value: "1"}, {Key: "b", Value: "2", Indent: 1}, {Value: "raw line"}, {Value: "c", Indent: 1}}
	got := n.Fields()
	if len(got) != len(want) {
		t.Fatalf("Fields = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestErrors(t *testing.T) {
	tr, _ := decodeFile(t, "init.mp4", corruptStsz)
	errs := tr.Errors()
	if len(errs) != 1 || errs[0].Path() != "moov/trak/mdia/minf/stbl/stsz" {
		t.Fatalf("Errors = %v", errs)
	}
	tr, _ = decodeFile(t, "init_truncated.mp4", nil)
	if n := len(tr.Errors()); n != 2 {
		t.Errorf("%d errors in truncated file, want 2", n)
	}
	if !tr.Boxes[2].Truncated(tr.Size) || tr.Boxes[1].Truncated(tr.Size) {
		t.Error("wrong Truncated")
	}
}

func TestJSON(t *testing.T) {
	tr, _ := decodeFile(t, "init.mp4", corruptStsz)
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatal(err)
	}
	type node struct {
		Type       string  `json:"type"`
		Name       string  `json:"name"`
		Pos        uint64  `json:"pos"`
		Version    *int    `json:"version"`
		Flags      string  `json:"flags"`
		Fields     []Field `json:"fields"`
		Error      string  `json:"error"`
		ChildError bool    `json:"childError"`
		Children   []node  `json:"children"`
	}
	var got struct {
		Size  int64  `json:"size"`
		Boxes []node `json:"boxes"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	moov := got.Boxes[2]
	if got.Size != 715 || moov.Type != "moov" || moov.Name != "Movie" || !moov.ChildError || moov.Version != nil {
		t.Errorf("moov: %+v", moov)
	}
	tkhd := moov.Children[1].Children[0]
	if tkhd.Version == nil || *tkhd.Version != 0 || tkhd.Flags != "000007" || tkhd.Fields[0].Key != "trackID" {
		t.Errorf("tkhd: %+v", tkhd)
	}
	stsz := moov.Children[1].Children[1].Children[2].Children[2].Children[3]
	if stsz.Type != "stsz" || stsz.Error == "" {
		t.Errorf("stsz: %+v", stsz)
	}
	empty, err := json.Marshal(&Tree{})
	if err != nil || string(empty) != `{"size":0,"boxes":[]}` {
		t.Errorf("empty tree: %s, %v", empty, err)
	}
}

func TestSearch(t *testing.T) {
	tr, r := decodeFile(t, "init.mp4", corruptStsz)
	cases := []struct {
		query string
		want  string
	}{
		{"type:stsz", "moov/trak/mdia/minf/stbl/stsz"},
		{"type:tkhd,trex", "moov/trak/tkhd moov/mvex/trex"},
		{"trackID=2", "moov/trak/tkhd moov/mvex/trex"},
		{"TRACKID=2", "moov/trak/tkhd moov/mvex/trex"},
		{"trackID=1", ""},
		{"defaultSampleFlags=00000000", "moov/mvex/trex"},
		{"mobitv", "moov/trak/mdia/hdlr"},
		{`"MobiTV Video"`, "moov/trak/mdia/hdlr"},
		{"sample size", "moov/trak/mdia/minf/stbl/stsz moov/mvex/trex"},
		{`"sample size"`, "moov/trak/mdia/minf/stbl/stsz"},
		{"path:minf/dinf", "moov/trak/mdia/minf/dinf moov/trak/mdia/minf/dinf/dref moov/trak/mdia/minf/dinf/dref/url "},
		{"path:dinf -type:dinf -type:dref", "moov/trak/mdia/minf/dinf/dref/url "},
		{"@640", "moov/trak/mdia/minf/stbl/stsz"},
		{"pos:0x280", "moov/trak/mdia/minf/stbl/stsz"},
		{"size>400", "moov moov/trak"},
		{"size=0x20 type:ftyp", "ftyp"},
		{"is:error", "moov/trak/mdia/minf/stbl/stsz"},
		{"-is:error type:stsz", ""},
		{"hex:61766331", "moov/trak/mdia/minf/stbl/stsd/avc1"},
		{"hex:'00 00 00'", ""},
		{`hex:"69 73 6f 35"`, "ftyp"},
		{"hex:0x6d7669 type:stsd", ""},
		{"", ""},
	}
	for _, c := range cases {
		q, err := ParseQuery(c.query)
		if c.query == "hex:'00 00 00'" {
			if err == nil {
				t.Errorf("%s: no error", c.query)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.query, err)
			continue
		}
		res, err := tr.Search(q, r)
		if err != nil {
			t.Errorf("%s: %v", c.query, err)
			continue
		}
		var paths []string
		for _, n := range res.Nodes {
			paths = append(paths, n.Path())
		}
		if got := strings.Join(paths, " "); got != c.want {
			t.Errorf("%s: got %q, want %q", c.query, got, c.want)
		}
	}
}

func TestSearchHits(t *testing.T) {
	tr, r := decodeFile(t, "init.mp4", nil)
	q, err := ParseQuery("hex:69736f")
	if err != nil {
		t.Fatal(err)
	}
	res, err := tr.Search(q, r)
	if err != nil {
		t.Fatal(err)
	}
	ftyp := tr.Boxes[0]
	if len(res.Nodes) != 1 || res.Nodes[0] != ftyp || res.Capped {
		t.Fatalf("Nodes = %v", res.Nodes)
	}
	want := []Span{{8, 3}, {16, 3}, {20, 3}}
	if got := res.Hits[ftyp]; len(got) != len(want) || got[0] != want[0] || got[2] != want[2] {
		t.Errorf("Hits = %v, want %v", got, want)
	}
}

func TestSearchManyChunks(t *testing.T) {
	// A file with a free box of more than two chunks, with the pattern across the chunk boundaries.
	size := 2*chunkSize + 100
	data := make([]byte, size)
	binary.BigEndian.PutUint32(data, uint32(size))
	copy(data[4:], "free")
	for _, pos := range []int{chunkSize - 2, 2*chunkSize - 1, size - 4} {
		copy(data[pos:], "\xca\xfe\xba\xbe")
	}
	r := bytes.NewReader(data)
	tr := Decode(r, int64(size), "")
	q, _ := ParseQuery("hex:cafebabe")
	res, err := tr.Search(q, r)
	if err != nil {
		t.Fatal(err)
	}
	hits := res.Hits[tr.Boxes[0]]
	if len(hits) != 3 || hits[0].Pos != chunkSize-2 || hits[1].Pos != 2*chunkSize-1 || hits[2].Pos != uint64(size-4) {
		t.Errorf("Hits = %v", hits)
	}
}

func TestUnknownAndTruncated(t *testing.T) {
	data := []byte("\x00\x00\x00\x0cabcd1234\x00\x00\x00\x10free")
	tr := Decode(bytes.NewReader(data), int64(len(data)), "")
	for query, want := range map[string]string{"is:unknown": "abcd", "is:truncated": "free"} {
		q, err := ParseQuery(query)
		if err != nil {
			t.Fatal(err)
		}
		res, err := tr.Search(q, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Nodes) != 1 || res.Nodes[0].Name() != want {
			t.Errorf("%s: %v", query, res.Nodes)
		}
	}
}

func TestParseQueryErrors(t *testing.T) {
	for _, query := range []string{"@x", "pos:", "size>x", "hex:zz", "hex:", "is:big", "-hex:00"} {
		if _, err := ParseQuery(query); err == nil {
			t.Errorf("%s: no error", query)
		}
	}
}

func TestTokenize(t *testing.T) {
	got := tokenize(` a  "b c" hex:"00 01"d "" e`)
	want := []string{"a", "b c", "hex:00 01d", "", "e"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("tokenize = %q, want %q", got, want)
	}
}

func TestParts(t *testing.T) {
	tr, r := decodeFile(t, "init.mp4", nil)
	sps := byPath(t, tr, "moov/trak/mdia/minf/stbl/stsd/avc1/avcC/SPS")
	if sps.IsBox() || sps.LongName() != "Sequence Parameter Set" || sps.Key != "2/1/1/2/2/0/0/0/0" {
		t.Errorf("SPS: box=%t name=%q key=%s", sps.IsBox(), sps.LongName(), sps.Key)
	}
	if got := tr.At(sps.Pos + 3); got != sps {
		t.Errorf("At in SPS = %v", got.Path())
	}
	if tr.Layout(sps, r) != nil {
		t.Error("a part has a layout")
	}
	q, _ := ParseQuery("is:part Width=640")
	res, _ := tr.Search(q, r)
	if len(res.Nodes) != 1 || res.Nodes[0] != sps {
		t.Errorf("search: %v", res.Nodes)
	}
}

func TestQuickTime(t *testing.T) {
	tr, r := decodeFile(t, "qt_h264_aac.mov", nil)
	if tr.QuickTime != "the major brand is qt" {
		t.Errorf("file: %q", tr.QuickTime)
	}
	q, _ := ParseQuery("is:quicktime")
	res, _ := tr.Search(q, r)
	var got []string
	for _, n := range res.Nodes {
		got = append(got, n.Name())
	}
	want := `ftyp wide hdlr hdlr avc1 hdlr hdlr mp4a wave \x00\x00\x00\x00 chan ©nam ©swr`
	if strings.Join(got, " ") != want {
		t.Errorf("QuickTime boxes:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	if len(tr.Errors()) != 0 {
		t.Errorf("errors: %v", tr.Errors())
	}
	nam := byPath(t, tr, "moov/udta/©nam")
	if _, ok := nam.Box.(*mp4.UserDataTextBox); !ok {
		t.Errorf("©nam decoded as %T", nam.Box)
	}
	lay := tr.Layout(nam, r)
	var names []string
	for _, f := range lay.Fields {
		names = append(names, f.Name+"="+f.Value)
	}
	wantLayout := `size=24 boxType=a96e616d size #1=12 language #1=21956 (und) text #1="mp4lens test"`
	if got := strings.Join(names, " "); got != wantLayout {
		t.Errorf("©nam layout:\n got %s\nwant %s", got, wantLayout)
	}

	iso, r2 := decodeFile(t, "prog_8s.mp4", nil)
	res, _ = iso.Search(q, r2)
	if iso.QuickTime != "" || len(res.Nodes) != 0 {
		t.Errorf("ISOBMFF file: %q %v", iso.QuickTime, res.Nodes)
	}
}

func TestLayout(t *testing.T) {
	tr, r := decodeFile(t, "init.mp4", nil)
	tkhd := byPath(t, tr, "moov/trak/tkhd")
	res := tr.Layout(tkhd, r)
	if res == nil || res.Err != nil || len(res.Fields) != 25 || res.Fields[6].Name != "TrackID" {
		t.Fatalf("tkhd layout: %+v", res)
	}
	moov := byPath(t, tr, "moov")
	if res := tr.Layout(moov, r); len(res.Fields) != 2 {
		t.Errorf("moov layout has %d fields", len(res.Fields))
	}
	seg, r2 := decodeFile(t, "1.m4s", nil)
	mdat := byPath(t, seg, "mdat")
	if res := seg.Layout(mdat, r2); len(res.Fields) != 2 || res.Fields[0].Value != strconv.FormatUint(mdat.Size, 10) {
		t.Errorf("mdat layout: %+v", res)
	}
}

func TestPlainContainer(t *testing.T) {
	tr, _ := decodeFile(t, "qt_h264_aac.mov", nil)
	want := map[string]bool{
		"moov": true, "moov/trak": true, "moov/trak/mdia": true, "moov/trak/mdia/minf": true,
		"moov/trak/mdia/minf/stbl": true, "moov/trak/mdia/minf/dinf": true, "moov/trak/edts": true,
		"moov/trak/mdia/minf/dinf/dref":               false, // version, flags and entry count first
		"moov/trak/mdia/minf/stbl/stsd":               false,
		"moov/trak/mdia/minf/stbl/stsd/avc1":          false, // sample entry fields first
		"moov/trak/mdia/minf/stbl/stsd/mp4a/wave":     true,
		"moov/trak/mdia/minf/stbl/stsd/avc1/avcC":     false, // not a container
		"moov/trak/mdia/minf/stbl/stsd/avc1/avcC/SPS": false, // a part
		"moov/udta":           true,
		"moov/trak/mdia/mdhd": false,
		"moov/trak/mdia/minf/stbl/stsd/mp4a/wave/esds":               false,
		"moov/trak/mdia/minf/stbl/stsd/mp4a/wave/esds/ES_Descriptor": false,
	}
	for path, w := range want {
		var found *Node
		for _, n := range tr.Nodes {
			if n.Path() == path {
				found = n
				break
			}
		}
		if found == nil {
			t.Errorf("no %s", path)
			continue
		}
		if got := found.PlainContainer(); got != w {
			t.Errorf("%s: PlainContainer = %t", path, got)
		}
	}
	// A udta that ends with the QuickTime terminator holds more than its child boxes.
	data := []byte("\x00\x00\x00\x18udta\x00\x00\x00\x0cWLOC\x00\x01\x00\x02\x00\x00\x00\x00")
	tr = Decode(bytes.NewReader(data), int64(len(data)), "")
	if tr.Boxes[0].PlainContainer() {
		t.Error("udta with a terminator is a plain container")
	}
}

func TestQuickTimeHandlers(t *testing.T) {
	tr, _ := decodeFile(t, "qt_h264_aac.mov", nil)
	media := byPath(t, tr, "moov/trak/mdia/hdlr")
	data := byPath(t, tr, "moov/trak/mdia/minf/hdlr")
	if !strings.HasPrefix(media.QuickTime, "QuickTime media handler") {
		t.Errorf("mdia/hdlr: %q", media.QuickTime)
	}
	if !strings.HasPrefix(data.QuickTime, "QuickTime data handler in minf") {
		t.Errorf("minf/hdlr: %q", data.QuickTime)
	}
}

func TestTextTracksAndItems(t *testing.T) {
	tr, r := decodeFile(t, "chapters_subs.m4a", nil)
	tx3g := byPath(t, tr, "moov/trak/mdia/minf/stbl/stsd/tx3g")
	res := tr.Layout(tx3g, r)
	if res.Spec != "3GPP TS 26.245 5.16" || tx3g.QuickTime != "" {
		t.Errorf("tx3g: spec %q, QuickTime %q", res.Spec, tx3g.QuickTime)
	}
	for _, f := range res.Fields {
		if f.Name == "" || f.Spec == "" {
			t.Errorf("tx3g field without name or spec: %+v", f)
		}
	}
	text := byPath(t, tr, "moov/trak/mdia/minf/stbl/stsd/text")
	if _, ok := text.Box.(*mp4.TextSampleEntryBox); !ok || !strings.Contains(text.QuickTime, "3GPP timed text") {
		t.Errorf("chapter sample entry: %T, QuickTime %q", text.Box, text.QuickTime)
	}
	if gmhdText := byPath(t, tr, "moov/trak/mdia/minf/gmhd/text"); !strings.HasPrefix(gmhdText.QuickTime, "Undocumented") {
		t.Errorf("gmhd text: QuickTime %q", gmhdText.QuickTime)
	}
	chpl := tr.Layout(byPath(t, tr, "moov/udta/chpl"), r)
	if f := chpl.Fields[len(chpl.Fields)-3]; f.Spec != "start time[2]" || f.Value != "5000000 (500ms)" {
		t.Errorf("chpl start of chapter 2: %+v", f)
	}
	for path, want := range map[string]string{
		"moov/udta/meta/ilst/trkn/data": "(track 5 of 13)",
		"moov/udta/meta/ilst/©nam/data": `"Chapters"`,
	} {
		res := tr.Layout(byPath(t, tr, path), r)
		if v := res.Fields[len(res.Fields)-1].Value; !strings.Contains(v, want) {
			t.Errorf("%s: value %q", path, v)
		}
	}
}

func TestDurations(t *testing.T) {
	tr, r := decodeFile(t, "chapters_subs.m4a", nil)
	for _, path := range []string{"moov/mvhd", "moov/trak/tkhd", "moov/trak/mdia/mdhd"} {
		res := tr.Layout(byPath(t, tr, path), r)
		found := false
		for _, f := range res.Fields {
			if f.Name == "Duration" {
				found = true
				if !strings.HasSuffix(f.Value, "s)") {
					t.Errorf("%s: duration %q has no time", path, f.Value)
				}
			}
		}
		if !found {
			t.Errorf("%s: no duration", path)
		}
	}
}
