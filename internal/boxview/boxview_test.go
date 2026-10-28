package boxview

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"strings"
	"testing"
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
	if len(tr.Boxes) != 3 || len(tr.Nodes) != 24 {
		t.Fatalf("got %d top-level boxes and %d boxes, want 3 and 24", len(tr.Boxes), len(tr.Nodes))
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
