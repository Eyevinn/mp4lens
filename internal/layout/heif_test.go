package layout

import (
	"bytes"
	"strings"
	"testing"
)

// fieldNamedIn returns the field called name in fields, or fails.
func fieldNamedIn(t *testing.T, fields []Field, name string) Field {
	t.Helper()
	for _, f := range fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no field %s in %s", name, names(fields))
	return Field{}
}

// checkCovered checks that the fields cover the box in data, at position pos, without gaps, and have names.
func checkCovered(t *testing.T, res *Result, data []byte, pos uint64) {
	t.Helper()
	if res.Err != nil || res.Capped {
		t.Fatalf("error %v, capped %v", res.Err, res.Capped)
	}
	next := pos
	for _, f := range res.Fields {
		if f.Pos != next || f.Name == "" {
			t.Fatalf("field %+v, expected at %d with a name", f, next)
		}
		next += uint64(f.Size)
	}
	if next != pos+uint64(len(data)) {
		t.Errorf("fields end at %d, the box at %d", next, pos+uint64(len(data)))
	}
}

func TestIloc(t *testing.T) {
	// Version 1, with construction methods, and the grids, items 3 and 6, in idat.
	data, pos := boxAt(t, "gradient_grid_alpha.heic", "iloc", 0)
	res := Box(data, pos, nil, 1000)
	checkCovered(t, res, data, pos)
	if res.Spec != isobmff+" 8.11.3" {
		t.Errorf("spec %q", res.Spec)
	}
	sizes := fieldNamedIn(t, res.Fields, "OffsetSize")
	if sizes.Value != "68 (offset_size=4 length_size=4)" || sizes.Key != "offsetSize" {
		t.Errorf("sizes: %+v", sizes)
	}
	method := fieldNamedIn(t, res.Fields, "Items[2].ConstructionMethod")
	if method.Spec != "reserved, construction_method[3]" || method.Value != "1 (construction_method=1 idat offset)" {
		t.Errorf("construction method: %+v", method)
	}
	length := fieldNamedIn(t, res.Fields, "Items[2].Extents[0].Length")
	if length.Spec != "extent_length[3][1]" || length.Kind != "u32" || length.Value != "8" {
		t.Errorf("extent length: %+v", length)
	}

	// Version 0, with a base offset.
	data, pos = boxAt(t, "ex60.avif", "iloc", 0)
	res = Box(data, pos, nil, 1000)
	checkCovered(t, res, data, pos)
	want := "size,boxType,version,flags,OffsetSize,BaseOffsetSize,itemCount,Items[0].ItemID," +
		"Items[0].DataReferenceIndex,Items[0].BaseOffset,Items[0].extentCount,Items[0].Extents[0].Offset," +
		"Items[0].Extents[0].Length"
	if got := names(res.Fields); got != want {
		t.Errorf("names:\n got %s\nwant %s", got, want)
	}
	if f := fieldNamedIn(t, res.Fields, "Items[0].BaseOffset"); f.Value != "250" || f.Spec != "base_offset[1]" {
		t.Errorf("base offset: %+v", f)
	}

	// The fields stop at the limit, which the split of the version and flags passes by one.
	data, pos = boxAt(t, "gradient_grid_alpha.heic", "iloc", 0)
	if res := Box(data, pos, nil, 10); !res.Capped || len(res.Fields) != 11 {
		t.Errorf("capped %v with %d fields", res.Capped, len(res.Fields))
	}
}

func TestIpma(t *testing.T) {
	data, pos := boxAt(t, "gradient_grid_alpha.heic", "ipma", 0)
	res := Box(data, pos, nil, 1000)
	checkCovered(t, res, data, pos)
	f := fieldNamedIn(t, res.Fields, "Entries[0].Associations[0]")
	if f.Value != "131 (essential=1 property_index=3)" || f.Spec != "essential[1][1], property_index[1][1]" {
		t.Errorf("association: %+v", f)
	}
	if res.Spec != isobmff+" 8.11.14" {
		t.Errorf("spec %q", res.Spec)
	}
}

func TestItemReferences(t *testing.T) {
	data, pos := boxAt(t, "gradient_grid_alpha.heic", "iref", 0)
	// The first cdsc reference, from item 7 to item 3.
	i := bytes.Index(data, []byte("cdsc")) - 4
	keep := Range{Start: pos + uint64(i), End: pos + uint64(i) + 14}
	res := Within(data, pos, keep, nil, "itemRef", 100)
	checkCovered(t, res, data[i:i+14], keep.Start)
	if got := names(res.Fields); got != "size,boxType,FromItemID,refCount,ToItemIDs #1" {
		t.Errorf("names: %s", got)
	}
	var specs []string
	for _, f := range res.Fields {
		specs = append(specs, f.Spec+"="+strings.SplitN(f.Value, " ", 2)[0])
	}
	if got := strings.Join(specs, " "); got != "size=14 type=\"cdsc\" from_item_ID=7 reference_count=1 to_item_ID[1]=3" {
		t.Errorf("specs: %s", got)
	}
	if res.Spec != isobmff+" 8.11.12" {
		t.Errorf("spec %q", res.Spec)
	}
	// On its own, cdsc is a track reference, which has another layout.
	if res := Box(data[i:i+14], keep.Start, nil, 100); res.Err == nil {
		t.Errorf("cdsc on its own decoded without error: %s", names(res.Fields))
	}
}

func TestImageProperties(t *testing.T) {
	testCases := []struct {
		file, box, field, spec, value string
	}{
		{"ex60.avif", "ispe", "ImageWidth", "image_width", "600"},
		{"ex60.avif", "pixi", "BitsPerChannel", "bits_per_channel", "080808"},
		{"ex60.avif", "pitm", "ItemID", "item_ID", "1"},
		{"ex60.avif", "infe", "ItemType", "item_type", `"av01"`},
		{"gradient_grid_alpha.heic", "irot", "Angle", "reserved, angle", "0 (0 degrees anti-clockwise)"},
		{"gradient_grid_alpha.heic", "auxC", "AuxType", "aux_type", `"urn:mpeg:hevc:2015:auxid:1"`},
		{"gradient_grid_alpha.heic", "idat", "Data", "data", "00000001040000400000000104000040"},
	}
	for _, tc := range testCases {
		data, pos := boxAt(t, tc.file, tc.box, 0)
		res := Box(data, pos, nil, 100)
		f := fieldNamedIn(t, res.Fields, tc.field)
		if f.Spec != tc.spec || f.Value != tc.value || res.Spec == "" {
			t.Errorf("%s %s: spec %q value %q, box spec %q", tc.box, tc.field, f.Spec, f.Value, res.Spec)
		}
	}
}

// mebxEntry is a mebx sample entry of an iPhone movie, with one key.
const mebxEntry = "\x00\x00\x00umebx\x00\x00\x00\x00\x00\x00\x00\x01\x00\x00\x00ekeys\x00\x00\x00]\x00\x00\x00\x01" +
	"\x00\x00\x001keydmdtacom.apple.quicktime.video-orientation\x00\x00\x00\x10dtyp\x00\x00\x00\x00\x00\x00\x00B" +
	"\x00\x00\x00\x14sdpd\x00\x00\x00\x0csdpi\x00\x00\x00\x00"

func TestMebx(t *testing.T) {
	data := []byte(mebxEntry)
	res := Box(data, 0, []Range{{16, 117}}, 100)
	if got := names(res.Fields); got != "size,boxType,reserved,DataReferenceIndex" || res.Spec != isobmff+" 12.8.4.1" {
		t.Errorf("mebx: %s, spec %q", got, res.Spec)
	}
	// The key table, without the key in it.
	res = Within(data, 0, Range{16, 117}, []Range{{24, 117}}, "mebxKeys", 100)
	if got := names(res.Fields); got != "size,boxType" || res.Err != nil || res.Spec != isobmff+" 12.8.4.2" {
		t.Errorf("keys: %s, error %v, spec %q", got, res.Err, res.Spec)
	}
	res = Within(data, 0, Range{24, 117}, []Range{{32, 117}}, "mebxKey", 100)
	if got := names(res.Fields); got != "size,boxType" || res.Fields[1].Value != "00000001" ||
		res.Spec != isobmff+" 12.8.4.3" {
		t.Errorf("key: %+v, spec %q", res.Fields, res.Spec)
	}
	res = Box(data[32:81], 32, nil, 100)
	if f := fieldNamedIn(t, res.Fields, "Value"); f.Spec != "key_value" ||
		f.Value != `"com.apple.quicktime.video-orientation"` {
		t.Errorf("keyd: %+v", f)
	}
}
