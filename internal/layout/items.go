package layout

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
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

// itemMeaning returns what the value v of type dataType of a metadata item means, or "".
func itemMeaning(item, name string, dataType int, v []byte) string {
	switch dataType {
	case 1, 4: // UTF-8
		if name == "iTunSMPB" {
			if gapless := iTunSMPB(string(v)); gapless != "" {
				return gapless
			}
		}
		if strings.HasPrefix(bytesText(v), `"`) {
			return "" // the value shows the text already
		}
		return quoteText(string(v))
	case 2, 5: // UTF-16
		if len(v)%2 != 0 {
			return ""
		}
		u := make([]uint16, len(v)/2)
		for i := range u {
			u[i] = binary.BigEndian.Uint16(v[2*i:])
		}
		return quoteText(string(utf16.Decode(u)))
	case 21, 22: // big-endian integer
		if len(v) == 0 || len(v) > 8 {
			return ""
		}
		var n uint64
		for _, b := range v {
			n = n<<8 | uint64(b)
		}
		if dataType == 21 && v[0]&0x80 != 0 {
			return strconv.FormatInt(int64(n<<(64-8*len(v)))>>(64-8*len(v)), 10)
		}
		return strconv.FormatUint(n, 10)
	case 0: // implicit, given by the item type
		switch {
		case (item == "trkn" || item == "disk") && len(v) >= 6:
			what := map[string]string{"trkn": "track", "disk": "disc"}[item]
			return fmt.Sprintf("%s %d of %d", what, binary.BigEndian.Uint16(v[2:]), binary.BigEndian.Uint16(v[4:]))
		case item == "gnre" && len(v) == 2 && binary.BigEndian.Uint16(v) > 0:
			return fmt.Sprintf("ID3v1 genre %d, the value minus 1", binary.BigEndian.Uint16(v)-1)
		}
	}
	return ""
}

// iTunSMPB returns the gapless playback information in the text of an iTunSMPB item: the encoder delay, the
// padding and the number of samples, in hexadecimal after a first 0 field. It returns "" if text is not in
// that form.
func iTunSMPB(text string) string {
	words := strings.Fields(text)
	if len(words) < 4 {
		return ""
	}
	var vals [3]uint64
	for i := range vals {
		v, err := strconv.ParseUint(words[i+1], 16, 64)
		if err != nil {
			return ""
		}
		vals[i] = v
	}
	return fmt.Sprintf("encoder delay %d, padding %d, %d samples", vals[0], vals[1], vals[2])
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
