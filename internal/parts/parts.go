// Package parts finds the structures inside boxes that the boxes hold as bytes, and decodes them with mp4ff:
// the parameter sets and SEI messages in avcC, hvcC and vvcC, the OBUs in av1C, the descriptors and the
// AudioSpecificConfig in esds, and the text items of QuickTime user data atoms.
package parts

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/Eyevinn/mp4ff/mp4"
)

// Part is a structure inside a box.
type Part struct {
	// Name is a short name, such as "SPS" or "ES_Descriptor".
	Name string
	// LongName says what the part is, such as "Sequence Parameter Set".
	LongName string
	// Pos is the position of the part in the file, and Size its number of bytes.
	Pos, Size uint64
	// Info are lines in the format of the Info output of mp4ff: a first line, and then " - key: value" lines.
	Info []string
	// Err tells why the part could not be decoded.
	Err error
	// QuickTime tells what a QuickTime structure is. It is empty for other parts.
	QuickTime string
	// Children are the parts inside this part.
	Children []*Part
}

// Of returns the parts of a box of type boxType, decoded by mp4ff as box, or nil if it could not be decoded.
// data holds the bytes of the box, which starts at position pos in the file. parentType is the type of the
// parent box, or "" for a top-level box.
func Of(boxType string, box mp4.Box, data []byte, pos uint64, parentType string) []*Part {
	f := &finder{data: data, pos: pos}
	switch b := box.(type) {
	case *mp4.AvcCBox:
		return f.avcC(b)
	case *mp4.HvcCBox:
		return f.hvcC(b)
	case *mp4.VvcCBox:
		return f.vvcC(b)
	case *mp4.Av1CBox:
		return f.av1C(b)
	case *mp4.EsdsBox:
		return esdsParts(data, pos)
	}
	if parentType == "udta" && len(boxType) == 4 && boxType[0] == 0xa9 {
		return quickTimeText(data, pos)
	}
	return nil
}

// finder finds the positions of byte strings, such as NAL units, in the bytes of a box.
type finder struct {
	data []byte
	pos  uint64
	from int // where the next search starts
}

// find returns the position in the file of b, the first time it occurs in the box after the previous one, and
// whether it was found.
func (f *finder) find(b []byte) (uint64, bool) {
	if len(b) == 0 {
		return 0, false
	}
	i := bytes.Index(f.data[f.from:], b)
	if i < 0 {
		return 0, false
	}
	start := f.from + i
	f.from = start + len(b)
	return f.pos + uint64(start), true
}

// part makes a part for the bytes b, found in the box, with the fields of v from structLines.
func (f *finder) part(name, longName string, b []byte, v any, err error) *Part {
	p := &Part{Name: name, LongName: longName, Size: uint64(len(b)), Err: err}
	p.Pos, _ = f.find(b)
	p.Info = append([]string{fmt.Sprintf("[%s] size=%d", name, len(b))}, structLines(v, 0)...)
	return p
}

// hexText writes b in hex, at most max bytes.
func hexText(b []byte, max int) string {
	if len(b) > max {
		return fmt.Sprintf("%x… (%d bytes)", b[:max], len(b))
	}
	return fmt.Sprintf("%x", b)
}

// field writes an Info line for a field at nesting level indent.
func field(indent int, key string, value any) string {
	return strings.Repeat("  ", indent) + fmt.Sprintf(" - %s: %v", key, value)
}
