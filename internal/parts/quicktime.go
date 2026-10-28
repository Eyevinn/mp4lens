package parts

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

// quickTimeTextNote is the note of the text items of QuickTime user data atoms.
const quickTimeTextNote = "QuickTime user data text item; ISOBMFF and iTunes metadata keep such text in data boxes"

// quickTimeText returns the text items of a QuickTime user data atom, such as ©nam, in data, which starts at
// position pos in the file. Each item has a 16-bit text size, a 16-bit language code and the text. It returns nil
// if the payload is not such a list.
func quickTimeText(data []byte, pos uint64) []*Part {
	if len(data) < 8 {
		return nil
	}
	var out []*Part
	for off := 8; off < len(data); {
		if off+4 > len(data) {
			return nil
		}
		size := int(binary.BigEndian.Uint16(data[off:]))
		lang := binary.BigEndian.Uint16(data[off+2:])
		end := off + 4 + size
		if end > len(data) {
			return nil
		}
		text := data[off+4 : end]
		p := &Part{Name: "text", LongName: "QuickTime text item", Pos: pos + uint64(off), Size: uint64(4 + size),
			QuickTime: quickTimeTextNote}
		p.Info = []string{fmt.Sprintf("[text] size=%d", 4+size), field(0, "size", size),
			field(0, "language", languageText(lang)), field(0, "text", textValue(text))}
		out = append(out, p)
		off = end
	}
	return out
}

// languageText writes a QuickTime language code: a Macintosh language code below 0x400, and otherwise a packed
// ISO 639-2/T code.
func languageText(code uint16) string {
	if code < 0x400 {
		return fmt.Sprintf("%d (Macintosh language code)", code)
	}
	b := []byte{byte(code>>10&0x1f) + 0x60, byte(code>>5&0x1f) + 0x60, byte(code&0x1f) + 0x60}
	return fmt.Sprintf("%s (ISO 639-2/T)", b)
}

func textValue(b []byte) string {
	if utf8.Valid(b) {
		return fmt.Sprintf("%q", b)
	}
	return fmt.Sprintf("%x", b)
}
