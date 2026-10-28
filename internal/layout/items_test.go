package layout

import (
	"encoding/binary"
	"testing"
)

// dataBox returns the bytes of a data box with dataType and value at position pos, and its type and value
// fields as the layout names them, with an empty value field.
func dataBox(dataType uint32, value []byte, pos uint64) ([]byte, []Field) {
	data := make([]byte, 16, 16+len(value))
	binary.BigEndian.PutUint32(data, uint32(16+len(value)))
	copy(data[4:], "data")
	binary.BigEndian.PutUint32(data[8:], dataType)
	data = append(data, value...)
	fields := []Field{
		{Pos: pos + 8, Size: 4, Kind: "u32", Name: "DataType", Value: "0"},
		{Pos: pos + 16, Size: len(value), Kind: "bytes", Name: "Data"},
	}
	fields[0].Value = map[uint32]string{0: "0 (implicit)", 1: "1 (UTF-8)", 2: "2 (UTF-16)", 21: "21 (signed integer)",
		22: "22 (unsigned integer)"}[dataType]
	return data, fields
}

func TestItemData(t *testing.T) {
	smpb := " 00000000 00000840 000001FD 000000000013B5C3 00000000 00000000"
	testCases := []struct {
		item, name string
		dataType   uint32
		value      []byte
		want       string
	}{
		{"trkn", "", 0, []byte{0, 0, 0, 5, 0, 13, 0, 0}, "track 5 of 13"},
		{"disk", "", 0, []byte{0, 0, 0, 1, 0, 2}, "disc 1 of 2"},
		{"gnre", "", 0, []byte{0, 0x12}, "ID3v1 genre 17, the value minus 1"},
		{"----", "iTunSMPB", 1, []byte(smpb), "encoder delay 2112, padding 509, 1291715 samples"},
		{"----", "iTunNORM", 1, []byte(" 0000085B"), `" 0000085B"`},
		{"©nam", "", 1, []byte("Owner"), `"Owner"`},
		{"©nam", "", 2, []byte{0, 'O', 0, 'K'}, `"OK"`},
		{"tmpo", "", 21, []byte{0, 0xbe}, "190"},
		{"rtng", "", 21, []byte{0xff}, "-1"},
		{"plID", "", 22, []byte{0xff}, "255"},
		{"covr", "", 13, []byte{0xff, 0xd8}, ""},
	}
	for _, tc := range testCases {
		data, fields := dataBox(tc.dataType, tc.value, 100)
		ItemData(tc.item, tc.name, fields, data, 100)
		want := ""
		if tc.want != "" {
			want = " (" + tc.want + ")"
		}
		if fields[1].Value != want {
			t.Errorf("%s %s: value %q, want %q", tc.item, tc.name, fields[1].Value, want)
		}
	}
}
