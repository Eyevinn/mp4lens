package layout

import (
	"encoding/binary"
	"math"
	"strconv"

	"github.com/Eyevinn/mp4ff/mp4"
)

// Some decoders read a part of the box in one read and decode it on their own, for speed or to try more than
// one layout. The functions here split such a read into its fields.

// column is a column of a table of entries.
type column struct {
	name string
	size int
}

// tables are the entries that the decoders of sample table boxes read as one block named "data".
var tables = map[string][]column{
	"stco": {{"ChunkOffset", 4}},
	"co64": {{"ChunkOffset", 8}},
	"stss": {{"SampleNumber", 4}},
	"stsz": {{"SampleSize", 4}},
	"stts": {{"SampleCount", 4}, {"SampleDelta", 4}},
	"ctts": {{"SampleCount", 4}, {"SampleOffset", 4}},
}

// expandTable splits the "data" field of a sample table box into its entries, and returns the new fields.
// Each entry gets the number of the entry from 1, as "SampleSize #3". Fields beyond max are left out.
func expandTable(boxType string, fields []Field, data []byte, base uint64, max int) ([]Field, bool) {
	cols := tables[boxType]
	if cols == nil {
		return fields, false
	}
	signed := false
	if boxType == "ctts" && len(fields) > 2 && fields[2].Name == "versionAndFlags" {
		vf, _ := strconv.ParseUint(fields[2].Value, 10, 32)
		signed = vf>>24 >= 1
	}
	for i, f := range fields {
		if f.Name != "data" || f.Kind != "bytes" {
			continue
		}
		out := append([]Field(nil), fields[:i]...)
		entrySize := 0
		for _, c := range cols {
			entrySize += c.size
		}
		start := int(f.Pos - base)
		capped := false
		for e := 0; (e+1)*entrySize <= f.Size; e++ {
			off := start + e*entrySize
			for _, c := range cols {
				if len(out) >= max {
					capped = true
					break
				}
				b := data[off : off+c.size]
				nf := Field{Pos: base + uint64(off), Size: c.size, Name: c.name + " #" + strconv.Itoa(e+1)}
				if c.size == 8 {
					nf.Kind, nf.Value = "u64", strconv.FormatUint(binary.BigEndian.Uint64(b), 10)
				} else if signed && c.name == "SampleOffset" {
					nf.Kind, nf.Value = "i32", strconv.FormatInt(int64(int32(binary.BigEndian.Uint32(b))), 10)
				} else {
					nf.Kind, nf.Value = "u32", strconv.FormatUint(uint64(binary.BigEndian.Uint32(b)), 10)
				}
				out = append(out, nf)
				off += c.size
			}
			if capped {
				break
			}
		}
		return append(out, fields[i+1:]...), capped
	}
	return fields, false
}

// audioEntry returns the fields of an audio sample entry, whose decoder reads the payload in one read, from
// the layout that the decoder found: ISOBMFF, or a QuickTime sound description of version 0, 1 or 2.
func audioEntry(a *mp4.AudioSampleEntryBox, fields []Field, data []byte, base uint64) []Field {
	if len(fields) != 3 || fields[2].Name != "data" {
		return fields
	}
	out := fields[:2]
	off := 8
	add := func(name, kind string, size int) {
		if off+size > len(data) {
			return
		}
		b := data[off : off+size]
		f := Field{Pos: base + uint64(off), Size: size, Kind: kind, Name: name}
		switch kind {
		case "u16":
			f.Value = strconv.FormatUint(uint64(binary.BigEndian.Uint16(b)), 10)
		case "u32":
			f.Value = strconv.FormatUint(uint64(binary.BigEndian.Uint32(b)), 10)
		case "f64":
			f.Value = strconv.FormatFloat(math.Float64frombits(binary.BigEndian.Uint64(b)), 'g', -1, 64)
		default:
			f.Value = bytesText(b)
		}
		out = append(out, f)
		off += size
	}
	add("reserved", "skip", 6)
	add("DataReferenceIndex", "u16", 2)
	add("QuickTimeVersion", "u16", 2)
	add("QuickTimeRevisionLevel", "u16", 2)
	add("QuickTimeVendor", "u32", 4)
	add("ChannelCount", "u16", 2)
	add("SampleSize", "u16", 2)
	add("CompressionID", "u16", 2)
	add("PacketSize", "u16", 2)
	add("SampleRate", "u32", 4)
	switch {
	case a.QuickTimeV1 != nil:
		for _, name := range []string{"SamplesPerPacket", "BytesPerPacket", "BytesPerFrame", "BytesPerSample"} {
			add(name, "u32", 4)
			out[len(out)-1].QuickTime = soundDescriptionNote
		}
	case a.QuickTimeV2 != nil:
		for _, c := range []column{{"SizeOfStructOnly", 4}, {"AudioSampleRate", 8}, {"NumAudioChannels", 4},
			{"Always7F000000", 4}, {"ConstBitsPerChannel", 4}, {"FormatSpecificFlags", 4},
			{"ConstBytesPerAudioPacket", 4}, {"ConstLPCMFramesPerAudioPacket", 4}} {
			kind := "u32"
			if c.size == 8 {
				kind = "f64"
			}
			add(c.name, kind, c.size)
			out[len(out)-1].QuickTime = soundDescriptionNote
		}
	}
	return out
}
