package layout

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

// boxAt returns the bytes and position of the first box of type typ in a file in testdata, after skipping
// the first skip boxes of that type.
func boxAt(t *testing.T, file, typ string, skip int) ([]byte, uint64) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	from := 0
	for {
		i := bytes.Index(data[from:], []byte(typ))
		if i < 4 {
			t.Fatalf("no %s box in %s", typ, file)
		}
		pos := from + i - 4
		if skip == 0 {
			size := int(binary.BigEndian.Uint32(data[pos:]))
			return data[pos : pos+size], uint64(pos)
		}
		skip--
		from = pos + 8
	}
}

func names(fields []Field) string {
	var parts []string
	for _, f := range fields {
		parts = append(parts, f.Name)
	}
	return strings.Join(parts, ",")
}

func TestTkhd(t *testing.T) {
	data, pos := boxAt(t, "init.mp4", "tkhd", 0)
	res := Box(data, pos, nil, 100)
	if res.Err != nil || res.Capped {
		t.Fatalf("err=%v capped=%t", res.Err, res.Capped)
	}
	want := "size,boxType,versionAndFlags,CreationTime,ModificationTime,TrackID,ReservedAfterTrackID,Duration," +
		"Reserved 8 x 0,Layer,AlternateGroup,Volume,,Matrix[0],Matrix[1],Matrix[2],Matrix[3],Matrix[4],Matrix[5]," +
		"Matrix[6],Matrix[7],Matrix[8],Width,Height"
	if got := names(res.Fields); got != want {
		t.Errorf("names:\n got %s\nwant %s", got, want)
	}
	id := res.Fields[5]
	if id.Pos != pos+20 || id.Size != 4 || id.Kind != "u32" || id.Value != "2" || id.QuickTime != "" {
		t.Errorf("TrackID: %+v", id)
	}
	if typ := res.Fields[1]; typ.Value != `"tkhd" (746b6864)` {
		t.Errorf("boxType value %s", typ.Value)
	}
	end := pos
	for _, f := range res.Fields {
		if f.Pos != end {
			t.Fatalf("field %s at %d, want %d", f.Name, f.Pos, end)
		}
		end += uint64(f.Size)
	}
	if end != pos+uint64(len(data)) {
		t.Errorf("fields end at %d, want %d", end, pos+uint64(len(data)))
	}
}

func TestRepeats(t *testing.T) {
	data, pos := boxAt(t, "1.m4s", "trun", 0)
	res := Box(data, pos, nil, 1000)
	want := "size,boxType,versionAndFlags,sampleCount,DataOffset,dur #1,size #1,flags #1,cto #1,dur #2"
	if got := names(res.Fields[:10]); got != want {
		t.Errorf("names:\n got %s\nwant %s", got, want)
	}
	if n := len(res.Fields); n != 5+4*60 {
		t.Errorf("%d fields", n)
	}
	res = Box(data, pos, nil, 7)
	if !res.Capped || len(res.Fields) != 7 || res.Err != nil {
		t.Errorf("capped=%t fields=%d err=%v", res.Capped, len(res.Fields), res.Err)
	}
}

func TestSkip(t *testing.T) {
	data, pos := boxAt(t, "init.mp4", "moov", 0)
	res := Box(data, pos, []Range{{pos + 8, pos + uint64(len(data))}}, 100)
	if got := names(res.Fields); got != "size,boxType" {
		t.Errorf("moov fields %s", got)
	}
}

func TestError(t *testing.T) {
	data, pos := boxAt(t, "init.mp4", "stsz", 0)
	data = bytes.Clone(data)
	binary.BigEndian.PutUint32(data[16:], 1) // one sample, without a sample size
	res := Box(data, pos, nil, 100)
	if res.Err == nil {
		t.Fatal("no error")
	}
	if got := names(res.Fields); !strings.HasPrefix(got, "size,boxType,versionAndFlags,") {
		t.Errorf("fields before the error: %s", got)
	}
}

func TestQuickTime(t *testing.T) {
	notes := func(file, typ string, skip int) map[string]string {
		data, pos := boxAt(t, file, typ, skip)
		res := Box(data, pos, nil, 100)
		m := map[string]string{}
		for _, f := range res.Fields {
			if f.QuickTime != "" {
				m[f.Name] = f.QuickTime
			}
		}
		return m
	}
	// ffmpeg writes vendor FFMP and the qualities, but version and revision level 0.
	avc1 := notes("qt_h264_aac.mov", "avc1", 0)
	if len(avc1) != 3 || !strings.HasPrefix(avc1["QuickTimeVendor"], "QuickTime vendor;") {
		t.Errorf("avc1: %v", avc1)
	}
	mp4a := notes("qt_h264_aac.mov", "mp4a", 0)
	for name, prefix := range map[string]string{"QuickTimeVersion": "QuickTime sound description version",
		"CompressionID": "QuickTime compression ID", "SamplesPerPacket": "QuickTime sound description field"} {
		if !strings.HasPrefix(mp4a[name], prefix) {
			t.Errorf("mp4a %s: %q", name, mp4a[name])
		}
	}
	// The second hdlr is the data handler in minf, which QuickTime writes with component type dhlr.
	if hdlr := notes("qt_h264_aac.mov", "hdlr", 1); !strings.Contains(hdlr["PreDefined"], "component type") {
		t.Errorf("hdlr: %v", hdlr)
	}
	for _, typ := range []string{"avc1", "hdlr", "mvhd", "mdhd"} {
		if n := notes("init.mp4", typ, 0); len(n) != 0 {
			t.Errorf("ISOBMFF %s has QuickTime notes: %v", typ, n)
		}
	}
}

// TestCoverage decodes all top-level boxes but mdat in the files in testdata, and checks that the fields
// cover the boxes and that nearly all have names.
func TestCoverage(t *testing.T) {
	files, err := os.ReadDir("../../testdata")
	if err != nil {
		t.Fatal(err)
	}
	var total, named int
	for _, file := range files {
		data, err := os.ReadFile("../../testdata/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		for pos := 0; pos+8 <= len(data); {
			size := int(binary.BigEndian.Uint32(data[pos:]))
			if size < 8 || pos+size > len(data) {
				break
			}
			if string(data[pos+4:pos+8]) != "mdat" {
				res := Box(data[pos:pos+size], uint64(pos), nil, 1<<20)
				for _, f := range res.Fields {
					total += f.Size
					if f.Name != "" {
						named += f.Size
					}
				}
			}
			pos += size
		}
	}
	if total == 0 || float64(named)/float64(total) < 0.98 {
		t.Errorf("%d of %d bytes named", named, total)
	}
}

func TestTables(t *testing.T) {
	data, pos := boxAt(t, "prog_8s.mp4", "stsz", 0)
	res := Box(data, pos, nil, 100)
	want := "size,boxType,versionAndFlags,SampleUniformSize,SampleNumber,SampleSize #1,SampleSize #2"
	if got := names(res.Fields[:7]); got != want {
		t.Errorf("stsz:\n got %s\nwant %s", got, want)
	}
	if f := res.Fields[5]; f.Value != "128" || f.Pos != pos+20 || f.Size != 4 {
		t.Errorf("first sample size: %+v", f)
	}
	if !res.Capped || len(res.Fields) != 100 {
		t.Errorf("capped=%t fields=%d", res.Capped, len(res.Fields))
	}
	data, pos = boxAt(t, "prog_8s.mp4", "stts", 0)
	res = Box(data, pos, nil, 100)
	if got := names(res.Fields[4:]); got != "SampleCount #1,SampleDelta #1" {
		t.Errorf("stts: %s", got)
	}
}

func TestAudioEntry(t *testing.T) {
	data, pos := boxAt(t, "qt_h264_aac.mov", "mp4a", 0)
	res := Box(data, pos, nil, 100)
	want := "size,boxType,reserved,DataReferenceIndex,QuickTimeVersion,QuickTimeRevisionLevel,QuickTimeVendor," +
		"ChannelCount,SampleSize,CompressionID,PacketSize,SampleRate," +
		"SamplesPerPacket,BytesPerPacket,BytesPerFrame,BytesPerSample"
	if got := names(res.Fields); got != want {
		t.Errorf("mp4a:\n got %s\nwant %s", got, want)
	}
	data, pos = boxAt(t, "prog_8s.mp4", "mp4a", 0)
	res = Box(data, pos, nil, 100)
	for _, f := range res.Fields {
		if f.QuickTime != "" {
			t.Errorf("ISOBMFF mp4a field %s has note %q", f.Name, f.QuickTime)
		}
	}
	if got := len(res.Fields); got != 12 {
		t.Errorf("ISOBMFF mp4a has %d fields", got)
	}
}
