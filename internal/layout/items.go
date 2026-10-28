package layout

import (
	"encoding/binary"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Eyevinn/mp4ff/mp4"
)

// maxItemText is the number of bytes of a text value that ItemData shows.
const maxItemText = 200

// ItemData adds what the value of the data box of a metadata item in ilst means to the value field in fields, the
// layout of the data box. data holds the bytes of the data box, which starts at position pos in the file. item
// is the type of the item, such as trkn, and name is the name of a freeform item ----, such as iTunSMPB.
func ItemData(item, name string, fields []Field, data []byte, pos uint64) {
	dataType := -1
	for i := range fields {
		f := &fields[i]
		switch f.Name {
		case "DataType":
			n, _, _ := strings.Cut(f.Value, " ")
			if v, err := strconv.Atoi(n); err == nil {
				dataType = v
			}
		case "Data":
			start := int(f.Pos - pos)
			if start < 0 || start+f.Size > len(data) {
				return
			}
			if meaning := itemMeaning(item, name, dataType, data[start:start+f.Size]); meaning != "" {
				f.Value += " (" + meaning + ")"
			}
		}
	}
}

// ItemValue returns the value v of type dataType of a metadata item as text, or "" if it is not text and mp4ff
// has no meaning for it: the text of a UTF-8 or UTF-16 value, or the meaning that mp4.DataBox.Meaning gives, such
// as the number of an integer, the track number of trkn or the gapless playback information of iTunSMPB. item
// is the type of the item, such as trkn, and name is the name of a freeform item ----, such as iTunSMPB.
func ItemValue(item, name string, dataType int, v []byte) string {
	if meaning := dataMeaning(item, name, dataType, v); meaning != "" {
		return meaning
	}
	switch dataType {
	case 1, 4: // UTF-8
		return string(v)
	case 2, 5: // UTF-16
		return utf16Text(v)
	}
	return ""
}

// dataMeaning returns what mp4ff says that the value v of type dataType of a metadata item means, or "".
func dataMeaning(item, name string, dataType int, v []byte) string {
	return (&mp4.DataBox{DataType: uint32(dataType), Data: v}).Meaning(item, name)
}

// utf16Text returns the big-endian UTF-16 text in v, or "" if v has an odd length.
func utf16Text(v []byte) string {
	if len(v)%2 != 0 {
		return ""
	}
	u := make([]uint16, len(v)/2)
	for i := range u {
		u[i] = binary.BigEndian.Uint16(v[2*i:])
	}
	return string(utf16.Decode(u))
}

// itemMeaning returns what the value v of type dataType of a metadata item means, as the layout adds it to
// the value, or "": the meaning from mp4ff, or the text of a value that the layout shows in hex.
func itemMeaning(item, name string, dataType int, v []byte) string {
	if meaning := dataMeaning(item, name, dataType, v); meaning != "" {
		return meaning
	}
	switch dataType {
	case 1, 4: // UTF-8
		if strings.HasPrefix(bytesText(v), `"`) {
			return "" // the value shows the text already
		}
		return quoteText(string(v))
	case 2, 5: // UTF-16
		if text := utf16Text(v); text != "" {
			return quoteText(text)
		}
	}
	return ""
}

// quoteText quotes text, cut to maxItemText bytes.
func quoteText(text string) string {
	if len(text) > maxItemText {
		cut := maxItemText
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		return strconv.Quote(text[:cut]) + "…"
	}
	return strconv.Quote(text)
}
