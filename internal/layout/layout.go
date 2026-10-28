// Package layout finds the byte layout of an MP4 box: the position, size, value and name of each field that the
// mp4ff decoder of the box reads.
//
// The box is decoded by mp4ff through a bits.SliceReader that records each read. The name of a read comes from
// the source of the decoder, as the target of the assignment that the read is in, such as TrackID for
// "t.TrackID = sr.ReadUint32()". The names are in names_gen.go, made by go generate from the source of the mp4ff
// version in go.mod. A read whose source line or method does not match the table has no name.
package layout

//go:generate go run ./gen

import (
	"encoding/binary"
	"fmt"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Eyevinn/mp4ff/bits"
	"github.com/Eyevinn/mp4ff/mp4"
)

// readName is the name of a read at a source line in the mp4 package.
type readName struct {
	method    string // the bits.SliceReader method
	name      string // "TrackID", "Matrix[]", or "" if the source does not tell
	quickTime bool   // the read sets a field of the QuickTime File Format
}

// Field is a field of a box, as read by the mp4ff decoder of the box.
type Field struct {
	// Pos is the position of the field in the file, and Size its number of bytes.
	Pos  uint64 `json:"pos"`
	Size int    `json:"size"`
	// Kind is the kind of read: u8, u16, i16, u24, u32, i32, u64, i64, string, bytes or skip.
	Kind string `json:"kind"`
	// Name is the name of the field, such as "TrackID", "Matrix[2]" or "dur #3" for the third read of a
	// repeated field. It is empty if the source of the decoder does not name the field.
	Name string `json:"name,omitempty"`
	// Value is the value read, as text.
	Value string `json:"value"`
	// QuickTime tells what a field of the QuickTime File Format is, and what ISOBMFF has in its place. It is
	// empty for ISOBMFF fields.
	QuickTime string `json:"quickTime,omitempty"`
	// Spec is the name of the field in the specification of the box, such as entry_count or sample_size[3], or
	// empty if the syntax tables do not have the field.
	Spec string `json:"spec,omitempty"`
	// About says what the field is.
	About string `json:"about,omitempty"`
}

// Result is the layout of a box.
type Result struct {
	Fields []Field
	// Capped is true if the box has more fields than were recorded.
	Capped bool
	// Err is the error of the decoder, if any. The fields that it read before the error are in Fields.
	Err error
	// Spec is the specification and clause of the box, or empty if the syntax tables do not have the box type.
	Spec string
}

// Range is a range of positions in the file, from Start up to End.
type Range struct {
	Start, End uint64
}

// Box returns the layout of the box in data, which starts at position pos in the file. The fields in skip, such
// as those of the child boxes, are left out. At most maxFields fields are recorded.
func Box(data []byte, pos uint64, skip []Range, maxFields int) *Result {
	r := &recorder{FixedSliceReader: bits.NewFixedSliceReader(data), base: pos, skip: skip, max: maxFields}
	box, err := mp4.DecodeBoxSR(pos, r)
	res := &Result{Fields: r.fields, Capped: r.capped, Err: err}
	nameRepeats(res.Fields, r.keys)
	if box != nil {
		var capped bool
		res.Fields, capped = expandTable(box.Type(), res.Fields, data, pos, maxFields)
		res.Capped = res.Capped || capped
		switch b := box.(type) {
		case *mp4.AudioSampleEntryBox:
			res.Fields = audioEntry(b, res.Fields, data, pos)
		case *mp4.UserDataTextBox:
			res.Fields = userDataTexts(b, res.Fields, data, pos)
		case *mp4.FtypBox, *mp4.StypBox:
			res.Fields = brands(res.Fields, data, pos)
		case *mp4.UnknownBox:
			for i := range res.Fields {
				if res.Fields[i].Name == "" && res.Fields[i].Kind == "bytes" {
					res.Fields[i].Name = "payload (not decoded)"
				}
			}
		}
	}
	addQuickTimeNotes(box, res.Fields)
	switch b := box.(type) {
	case *mp4.MvhdBox:
		AddDuration(res.Fields, "Duration", uint64(b.Timescale))
	case *mp4.MdhdBox:
		AddDuration(res.Fields, "Duration", uint64(b.Timescale))
	}
	if len(data) >= 8 {
		key := string(data[4:8])
		switch b := box.(type) {
		case *mp4.VisualSampleEntryBox:
			key = "visual"
		case *mp4.AudioSampleEntryBox:
			key = "audio"
		case *mp4.TextSampleEntryBox:
			key = "tx3g"
		case *mp4.QuickTimeTextSampleEntryBox:
			key = "qttext"
		case *mp4.UserDataTextBox:
			key = "userDataText"
		case *mp4.NameBox:
			if b.FullBox {
				key = "itemName"
			}
		}
		res.Fields, res.Spec = addSyntax(key, pos, res.Fields)
	}
	return res
}

// AddDuration adds the time of the field called name in fields, a duration in units of timescale per second, as
// in "2m25.75s". A duration with all bits set, which means an unknown duration, gets no time.
func AddDuration(fields []Field, name string, timescale uint64) {
	if timescale == 0 {
		return
	}
	for i := range fields {
		f := &fields[i]
		if f.Name != name {
			continue
		}
		v, err := strconv.ParseUint(f.Value, 10, 64)
		if err != nil || v == 0xffffffff && f.Size == 4 || v == 0xffffffffffffffff {
			continue
		}
		d := time.Duration(float64(v) / float64(timescale) * float64(time.Second))
		f.Value += " (" + d.Round(time.Millisecond).String() + ")"
	}
}

// recorder is a bits.SliceReader that records the reads.
type recorder struct {
	*bits.FixedSliceReader
	base   uint64
	skip   []Range
	max    int
	capped bool
	fields []Field
	keys   []string // the source line of each field
}

// add records a read of kind from start up to the current position.
func (r *recorder) add(method, kind string, start int, value func() string) {
	pos := r.base + uint64(start)
	for _, s := range r.skip {
		if pos >= s.Start && pos < s.End {
			return
		}
	}
	if len(r.fields) >= r.max {
		r.capped = true
		return
	}
	f := Field{Pos: pos, Size: r.GetPos() - start, Kind: kind, Value: value()}
	key := ""
	if _, file, line, ok := runtime.Caller(2); ok {
		key = path.Base(path.Dir(file)) + "/" + path.Base(file) + ":" + strconv.Itoa(line)
		if n, ok := readNames[key]; ok && n.method == method {
			f.Name = n.name
			if n.quickTime {
				f.QuickTime = quickTimeField
			}
		}
	}
	r.fields = append(r.fields, f)
	r.keys = append(r.keys, key)
}

func (r *recorder) ReadUint8() byte {
	p := r.GetPos()
	v := r.FixedSliceReader.ReadUint8()
	r.add("ReadUint8", "u8", p, func() string { return strconv.FormatUint(uint64(v), 10) })
	return v
}

func (r *recorder) ReadUint16() uint16 {
	p := r.GetPos()
	v := r.FixedSliceReader.ReadUint16()
	r.add("ReadUint16", "u16", p, func() string { return strconv.FormatUint(uint64(v), 10) })
	return v
}

func (r *recorder) ReadInt16() int16 {
	p := r.GetPos()
	v := r.FixedSliceReader.ReadInt16()
	r.add("ReadInt16", "i16", p, func() string { return strconv.FormatInt(int64(v), 10) })
	return v
}

func (r *recorder) ReadUint24() uint32 {
	p := r.GetPos()
	v := r.FixedSliceReader.ReadUint24()
	r.add("ReadUint24", "u24", p, func() string { return strconv.FormatUint(uint64(v), 10) })
	return v
}

func (r *recorder) ReadUint32() uint32 {
	p := r.GetPos()
	v := r.FixedSliceReader.ReadUint32()
	r.add("ReadUint32", "u32", p, func() string { return strconv.FormatUint(uint64(v), 10) })
	return v
}

func (r *recorder) ReadInt32() int32 {
	p := r.GetPos()
	v := r.FixedSliceReader.ReadInt32()
	r.add("ReadInt32", "i32", p, func() string { return strconv.FormatInt(int64(v), 10) })
	return v
}

func (r *recorder) ReadUint64() uint64 {
	p := r.GetPos()
	v := r.FixedSliceReader.ReadUint64()
	r.add("ReadUint64", "u64", p, func() string { return strconv.FormatUint(v, 10) })
	return v
}

func (r *recorder) ReadInt64() int64 {
	p := r.GetPos()
	v := r.FixedSliceReader.ReadInt64()
	r.add("ReadInt64", "i64", p, func() string { return strconv.FormatInt(v, 10) })
	return v
}

func (r *recorder) ReadFixedLengthString(n int) string {
	p := r.GetPos()
	v := r.FixedSliceReader.ReadFixedLengthString(n)
	r.add("ReadFixedLengthString", "string", p, func() string { return strconv.Quote(v) })
	return v
}

func (r *recorder) ReadZeroTerminatedString(maxLen int) string {
	p := r.GetPos()
	v := r.FixedSliceReader.ReadZeroTerminatedString(maxLen)
	r.add("ReadZeroTerminatedString", "string", p, func() string { return strconv.Quote(v) })
	return v
}

func (r *recorder) ReadPossiblyZeroTerminatedString(maxLen int) (string, bool) {
	p := r.GetPos()
	v, ok := r.FixedSliceReader.ReadPossiblyZeroTerminatedString(maxLen)
	r.add("ReadPossiblyZeroTerminatedString", "string", p, func() string { return strconv.Quote(v) })
	return v, ok
}

func (r *recorder) ReadBytes(n int) []byte {
	p := r.GetPos()
	v := r.FixedSliceReader.ReadBytes(n)
	r.add("ReadBytes", "bytes", p, func() string { return bytesText(v) })
	return v
}

func (r *recorder) RemainingBytes() []byte {
	p := r.GetPos()
	v := r.FixedSliceReader.RemainingBytes()
	r.add("RemainingBytes", "bytes", p, func() string { return bytesText(v) })
	return v
}

func (r *recorder) SkipBytes(n int) {
	p := r.GetPos()
	r.FixedSliceReader.SkipBytes(n)
	r.add("SkipBytes", "skip", p, func() string {
		end := min(r.GetPos(), r.Length())
		if p >= end {
			return ""
		}
		return bytesText(r.slice(p, end))
	})
}

// SetPos moves the read position. Moving back forgets the fields read from the new position on, since the
// decoder only looked at them, and reads them again.
func (r *recorder) SetPos(pos int) {
	r.FixedSliceReader.SetPos(pos)
	abs := r.base + uint64(pos)
	i := len(r.fields)
	for i > 0 && r.fields[i-1].Pos >= abs {
		i--
	}
	r.fields, r.keys = r.fields[:i], r.keys[:i]
}

// slice returns the bytes from start to end without moving the read position.
func (r *recorder) slice(start, end int) []byte {
	b := make([]byte, end-start)
	pos := r.GetPos()
	r.FixedSliceReader.SetPos(start)
	_ = r.LookAhead(0, b)
	r.FixedSliceReader.SetPos(pos)
	return b
}

// maxValueBytes is the number of bytes that bytesText writes in hex.
const maxValueBytes = 32

// bytesText writes b in hex, as text too if it is printable ASCII, such as the four bytes of a box type.
func bytesText(b []byte) string {
	if len(b) > 0 && len(b) <= 8 && utf8.Valid(b) && printable(b) {
		return fmt.Sprintf("%q (%x)", b, b)
	}
	if len(b) > maxValueBytes {
		return fmt.Sprintf("%x… (%d bytes)", b[:maxValueBytes], len(b))
	}
	return fmt.Sprintf("%x", b)
}

func printable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// nameRepeats numbers the fields that are read more than once from the same source line: an index from 0 for
// a field with an index in the source, such as Matrix[2], and the count from 1 for other fields, such as dur #3.
func nameRepeats(fields []Field, keys []string) {
	total := map[string]int{}
	for i, k := range keys {
		if fields[i].Name != "" {
			total[k]++
		}
	}
	seen := map[string]int{}
	for i, k := range keys {
		f := &fields[i]
		if f.Name == "" {
			continue
		}
		n := seen[k]
		seen[k]++
		if j := strings.LastIndex(f.Name, "[]"); j >= 0 {
			f.Name = f.Name[:j] + "[" + strconv.Itoa(n) + "]" + f.Name[j+2:]
		} else if total[k] > 1 {
			f.Name += " #" + strconv.Itoa(n+1)
		}
	}
}

// Header returns the layout of the box header in data, at position pos in the file, for boxes that are not
// decoded, such as mdat.
func Header(data []byte, pos uint64) *Result {
	res := &Result{}
	if len(data) < 8 {
		return res
	}
	res.Fields = append(res.Fields,
		Field{Pos: pos, Size: 4, Kind: "u32", Name: "size", Value: strconv.FormatUint(uint64(binary.BigEndian.Uint32(data)), 10)},
		Field{Pos: pos + 4, Size: 4, Kind: "bytes", Name: "boxType", Value: bytesText(data[4:8])})
	if binary.BigEndian.Uint32(data) == 1 && len(data) >= 16 {
		res.Fields = append(res.Fields, Field{Pos: pos + 8, Size: 8, Kind: "u64", Name: "largeSize",
			Value: strconv.FormatUint(binary.BigEndian.Uint64(data[8:]), 10)})
	}
	res.Fields, res.Spec = addSyntax(string(data[4:8]), pos, res.Fields)
	return res
}
