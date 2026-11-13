package parts

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/Eyevinn/mp4ff/mp4"
)

// psshParts returns the data of a Widevine or PlayReady pssh box, decoded. data holds the bytes of the box, which
// starts at position pos in the file. Other protection systems give no parts.
func psshParts(b *mp4.PsshBox, data []byte, pos uint64) []*Part {
	if len(b.Data) == 0 || len(data) < 8 {
		return nil
	}
	start := 8 // box header
	if binary.BigEndian.Uint32(data) == 1 {
		start = 16 // with a 64-bit size
	}
	start += 4 + 16 // version and flags, system ID
	if b.Version > 0 {
		start += 4 + 16*len(b.KIDs)
	}
	start += 4 // data size
	if start+len(b.Data) > len(data) || !bytes.Equal(data[start:start+len(b.Data)], b.Data) {
		return nil
	}
	dataPos := pos + uint64(start)
	switch b.SystemID.String() {
	case mp4.UUIDWidevine:
		return []*Part{widevineData(b.Data, dataPos)}
	case mp4.UUIDPlayReady:
		return []*Part{playReadyObject(b.Data, dataPos)}
	}
	return nil
}

// widevineData decodes the WidevinePsshData protobuf message in b, at position pos in the file.
func widevineData(b []byte, pos uint64) *Part {
	p := &Part{Name: "WidevinePsshData", LongName: "Widevine PSSH Data", Pos: pos, Size: uint64(len(b))}
	p.Info = []string{fmt.Sprintf("[%s] size=%d", p.Name, len(b))}
	d, err := mp4.DecodeWidevinePsshData(b)
	if err != nil {
		p.Err = err
		return p
	}
	p.Info = append(p.Info, infoLines(d)...)
	return p
}

// playReadyObject decodes the PlayReady Object in b, at position pos in the file, with a part for each record.
func playReadyObject(b []byte, pos uint64) *Part {
	p := &Part{Name: "PlayReadyObject", LongName: "PlayReady Object", Pos: pos, Size: uint64(len(b))}
	p.Info = []string{fmt.Sprintf("[%s] size=%d", p.Name, len(b))}
	if len(b) >= 6 {
		p.Info = append(p.Info, field(0, "length", binary.LittleEndian.Uint32(b)),
			field(0, "recordCount", binary.LittleEndian.Uint16(b[4:])))
	}
	obj, err := mp4.DecodePlayReadyObject(b)
	if err != nil {
		p.Err = err
		return p
	}
	for i := range obj.Records {
		p.Children = append(p.Children, playReadyRecord(&obj.Records[i], pos))
	}
	return p
}

// playReadyRecord makes a part of record r of a PlayReady Object at position pos in the file: the XML and key
// IDs of a PlayReady Header, and the data of other records.
func playReadyRecord(r *mp4.PlayReadyRecord, pos uint64) *Part {
	name, longName := strings.ReplaceAll(r.TypeName(), " ", ""), r.TypeName()+" record"
	if r.TypeName() == "Unknown" {
		name, longName = fmt.Sprintf("Record %d", r.Type), fmt.Sprintf("PlayReady record of type %d", r.Type)
	}
	p := &Part{Name: name, LongName: longName, Pos: pos + uint64(r.Offset), Size: uint64(4 + len(r.Data))}
	p.Info = []string{fmt.Sprintf("[%s] size=%d", name, p.Size),
		field(0, "type", fmt.Sprintf("%d (%s)", r.Type, r.TypeName())), field(0, "length", len(r.Data))}
	if r.Type == mp4.PlayReadyRecordHeader {
		p.Info = append(p.Info, infoLines(r)...)
	} else {
		p.Info = append(p.Info, field(0, "data", hexText(r.Data, maxHexBytes)))
	}
	return p
}

// infoLines returns the lines that the Info method of v writes, at nesting level 0.
func infoLines(v interface{ Info(io.Writer, string) error }) []string {
	var sb strings.Builder
	if err := v.Info(&sb, ""); err != nil {
		return []string{field(0, "error", err)}
	}
	return strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
}
