package parts

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"github.com/Eyevinn/mp4ff/bits"
	"github.com/Eyevinn/mp4ff/mp4"
)

// partsOf decodes the first box of type typ in a file in testdata, and returns its parts and its position.
func partsOf(t *testing.T, file, typ, parent string) ([]*Part, uint64, []byte) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(data, []byte(typ)) - 4
	if i < 0 {
		t.Fatalf("no %s in %s", typ, file)
	}
	b := data[i : i+int(binary.BigEndian.Uint32(data[i:]))]
	box, _ := mp4.DecodeBoxSR(uint64(i), bits.NewFixedSliceReader(b))
	return Of(typ, box, b, uint64(i), parent), uint64(i), data
}

func summary(ps []*Part) string {
	var s []string
	for _, p := range ps {
		x := p.Name
		if len(p.Children) > 0 {
			x += "(" + summary(p.Children) + ")"
		}
		s = append(s, x)
	}
	return strings.Join(s, " ")
}

func hasLine(p *Part, line string) bool {
	for _, l := range p.Info {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

// checkBytes checks that each part starts with the bytes that it says it holds.
func checkBytes(t *testing.T, ps []*Part, data []byte, want map[string][]byte) {
	t.Helper()
	for _, p := range ps {
		if w, ok := want[p.Name]; ok && !bytes.HasPrefix(data[p.Pos:], w) {
			t.Errorf("%s at %d: % x", p.Name, p.Pos, data[p.Pos:p.Pos+4])
		}
	}
}

func TestAvcC(t *testing.T) {
	ps, pos, data := partsOf(t, "init.mp4", "avcC", "avc1")
	if got := summary(ps); got != "SPS PPS" {
		t.Fatalf("parts %s", got)
	}
	sps := ps[0]
	if sps.Err != nil || sps.Pos != pos+16 || sps.Size != 25 || !hasLine(sps, "- Width: 640") {
		t.Errorf("SPS at %d size %d err %v: %v", sps.Pos, sps.Size, sps.Err, sps.Info)
	}
	checkBytes(t, ps, data, map[string][]byte{"SPS": {0x67}, "PPS": {0x68}})
}

func TestHvcC(t *testing.T) {
	ps, _, data := partsOf(t, "hvc1_init.mp4", "hvcC", "hvc1")
	if got := summary(ps); got != "VPS SPS PPS SEI(SEI 5)" {
		t.Fatalf("parts %s", got)
	}
	for _, p := range ps {
		if p.Err != nil {
			t.Errorf("%s: %v", p.Name, p.Err)
		}
	}
	checkBytes(t, ps, data, map[string][]byte{"VPS": {0x40}, "SPS": {0x42}, "PPS": {0x44}, "SEI": {0x4e}})
	if msg := ps[3].Children[0]; !hasLine(msg, "- UUID: 2ca2de09b51747dbbb55a4fe7fc2fc4e") {
		t.Errorf("SEI message: %v", msg.Info)
	}
}

func TestAv1C(t *testing.T) {
	ps, _, data := partsOf(t, "av1_init.mp4", "av1C", "av01")
	if got := summary(ps); got != "SequenceHeader" {
		t.Fatalf("parts %s", got)
	}
	if ps[0].Err != nil || data[ps[0].Pos]>>3&0xf != 1 {
		t.Errorf("sequence header at %d: err %v", ps[0].Pos, ps[0].Err)
	}
}

func TestEsds(t *testing.T) {
	ps, pos, _ := partsOf(t, "prog_8s.mp4", "esds", "mp4a")
	want := "ES_Descriptor(DecoderConfigDescriptor(DecoderSpecificInfo(AudioSpecificConfig)) SLConfigDescriptor)"
	if got := summary(ps); got != want {
		t.Fatalf("parts %s", got)
	}
	es := ps[0]
	dc := es.Children[0]
	asc := dc.Children[0].Children[0]
	if es.Pos != pos+12 || !hasLine(dc, "- objectTypeIndication: 0x40 Audio ISO/IEC 14496-3 (AAC)") ||
		!hasLine(asc, "- audioObjectType: 2 AAC LC") || !hasLine(asc, "- samplingFrequency: 48000") {
		t.Errorf("ES at %d\n%v\n%v", es.Pos, dc.Info, asc.Info)
	}
}

func TestQuickTimeText(t *testing.T) {
	ps, _, _ := partsOf(t, "qt_h264_aac.mov", "\xa9nam", "udta")
	if len(ps) != 1 || !hasLine(ps[0], `- text: "mp4lens test"`) || ps[0].QuickTime == "" {
		t.Fatalf("parts %v", ps)
	}
	if !hasLine(ps[0], "- language: und (ISO 639-2/T)") && !strings.Contains(strings.Join(ps[0].Info, "\n"), "Macintosh") {
		t.Errorf("language: %v", ps[0].Info)
	}
}
