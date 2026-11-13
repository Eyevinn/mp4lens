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
func partsOf(t *testing.T, file, typ string) ([]*Part, uint64, []byte) {
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
	return Of(box, b, uint64(i)), uint64(i), data
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
	ps, pos, data := partsOf(t, "init.mp4", "avcC")
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
	ps, _, data := partsOf(t, "hvc1_init.mp4", "hvcC")
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
	ps, _, data := partsOf(t, "av1_init.mp4", "av1C")
	if got := summary(ps); got != "SequenceHeader" {
		t.Fatalf("parts %s", got)
	}
	if ps[0].Err != nil || data[ps[0].Pos]>>3&0xf != 1 {
		t.Errorf("sequence header at %d: err %v", ps[0].Pos, ps[0].Err)
	}
}

func TestEsds(t *testing.T) {
	ps, pos, _ := partsOf(t, "prog_8s.mp4", "esds")
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

func TestAudioSpecificConfigObjectTypes(t *testing.T) {
	const note = "- note: mp4ff decodes only these leading fields of this object type"
	// The AudioSpecificConfig of an xHE-AAC (USAC) stream, which mp4ff decodes in full.
	usac := []byte{0xf9, 0x46, 0x43, 0x22, 0x1c, 0xc0, 0x58, 0x52, 0x00, 0x20, 0x00, 0xa0, 0x40, 0x46, 0xd0, 0xb8, 0x00}
	p := audioSpecificConfig(usac, 100)
	if p.Err != nil || !hasLine(p, "- audioObjectType: 42 xHE-AAC (USAC)") ||
		!hasLine(p, "- samplingFrequency: 48000") || !hasLine(p, "- channelConfiguration: 2") || hasLine(p, note) {
		t.Errorf("USAC: err %v\n%v", p.Err, p.Info)
	}
	// So a USAC config that ends in its UsacConfig is an error, not a config with only the leading fields.
	if p := audioSpecificConfig(usac[:3], 100); p.Err == nil || hasLine(p, note) {
		t.Errorf("short USAC: err %v\n%v", p.Err, p.Info)
	}
	// AAC Main, 48 kHz, stereo, which mp4ff decodes in full.
	if p := audioSpecificConfig([]byte{0x09, 0x90}, 100); p.Err != nil ||
		!hasLine(p, "- audioObjectType: 1 AAC Main") || hasLine(p, note) {
		t.Errorf("AAC Main: err %v\n%v", p.Err, p.Info)
	}
	// AAC LTP, 48 kHz, stereo, of which mp4ff decodes the leading fields.
	if p := audioSpecificConfig([]byte{0x21, 0x90}, 100); p.Err != nil ||
		!hasLine(p, "- audioObjectType: 4 AAC LTP") || !hasLine(p, "- channelConfiguration: 2") || !hasLine(p, note) {
		t.Errorf("AAC LTP: err %v\n%v", p.Err, p.Info)
	}
	// An AAC LC config that ends before its channel configuration is still an error.
	if p := audioSpecificConfig([]byte{0x11}, 100); p.Err == nil {
		t.Errorf("short AAC LC: no error\n%v", p.Info)
	}
}

func TestPssh(t *testing.T) {
	data, err := os.ReadFile("../../testdata/init_cenc.cmfv")
	if err != nil {
		t.Fatal(err)
	}
	// The file has a Widevine and then a PlayReady pssh box.
	var ps []*Part
	for from := 0; ; {
		i := bytes.Index(data[from:], []byte("pssh"))
		if i < 0 {
			break
		}
		pos := from + i - 4
		b := data[pos : pos+int(binary.BigEndian.Uint32(data[pos:]))]
		box, err := mp4.DecodeBoxSR(uint64(pos), bits.NewFixedSliceReader(b))
		if err != nil {
			t.Fatal(err)
		}
		ps = append(ps, Of(box, b, uint64(pos))...)
		from = pos + len(b)
	}
	if got := summary(ps); got != "WidevinePsshData PlayReadyObject(PlayReadyHeader)" {
		t.Fatalf("parts %s", got)
	}
	const kid = "f057639d-9287-3315-8bf5-50999c4945f7"
	wv, pr := ps[0], ps[1]
	if wv.Err != nil || wv.Pos != 796 || wv.Size != 69 || !hasLine(wv, `- provider: "castlabs"`) ||
		!hasLine(wv, "- keyID[1]: "+kid) {
		t.Errorf("Widevine at %d size %d err %v: %v", wv.Pos, wv.Size, wv.Err, wv.Info)
	}
	hdr := pr.Children[0]
	if pr.Err != nil || !hasLine(pr, "- length: 786") || !hasLine(pr, "- recordCount: 1") ||
		hdr.Pos != pr.Pos+6 || hdr.Size != pr.Size-6 || !hasLine(hdr, "- type: 1 (PlayReady Header)") ||
		!hasLine(hdr, "- KID[1]: "+kid) {
		t.Errorf("PlayReady at %d err %v: %v\nheader at %d size %d: %v", pr.Pos, pr.Err, pr.Info, hdr.Pos, hdr.Size,
			hdr.Info)
	}
	// The PlayReady Object and its records start with little-endian lengths and types.
	checkBytes(t, ps, data, map[string][]byte{"WidevinePsshData": {0x08, 0x01}, "PlayReadyObject": {0x12, 0x03, 0, 0}})
	checkBytes(t, pr.Children, data, map[string][]byte{"PlayReadyHeader": {0x01, 0x00, 0x08, 0x03}})
}

// psshPartsOf makes a pssh box at position 100, and returns its parts.
func psshPartsOf(t *testing.T, systemID string, kids []string, data []byte) []*Part {
	t.Helper()
	b, err := mp4.NewPsshBox(systemID, kids, data)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := b.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	box, err := mp4.DecodeBoxSR(100, bits.NewFixedSliceReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	return Of(box, buf.Bytes(), 100)
}

func TestPsshVersions(t *testing.T) {
	// A Widevine message with key ID f057639d-9287-3315-8bf5-50999c4945f7 in a version 1 box with two KIDs: the
	// data follows the header, version and flags, system ID, KID count, KIDs and data size.
	msg := append([]byte{0x12, 0x10}, 0xf0, 0x57, 0x63, 0x9d, 0x92, 0x87, 0x33, 0x15, 0x8b, 0xf5, 0x50, 0x99, 0x9c, 0x49,
		0x45, 0xf7)
	kids := []string{"f057639d-9287-3315-8bf5-50999c4945f7", "00000000-0000-0000-0000-000000000001"}
	ps := psshPartsOf(t, mp4.UUIDWidevine, kids, msg)
	if len(ps) != 1 || ps[0].Err != nil || ps[0].Pos != 100+8+4+16+4+2*16+4 ||
		!hasLine(ps[0], "- keyID[1]: f057639d-9287-3315-8bf5-50999c4945f7") {
		t.Errorf("version 1: %+v", ps)
	}
	// Data that is not a Widevine message is an error of the part.
	if ps := psshPartsOf(t, mp4.UUIDWidevine, nil, []byte{0xff}); len(ps) != 1 || ps[0].Err == nil {
		t.Errorf("bad Widevine data: %+v", ps)
	}
	if ps := psshPartsOf(t, mp4.UUIDPlayReady, nil, []byte{1, 2, 3}); len(ps) != 1 || ps[0].Err == nil {
		t.Errorf("bad PlayReady data: %+v", ps)
	}
	// Other protection systems have no parts.
	if ps := psshPartsOf(t, mp4.UUIDFairPlay, nil, msg); len(ps) != 0 {
		t.Errorf("FairPlay: %+v", ps)
	}
}
