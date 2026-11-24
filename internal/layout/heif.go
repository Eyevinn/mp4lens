package layout

import (
	"fmt"
	"strconv"

	"github.com/Eyevinn/mp4ff/mp4"
)

// The decoders of the iloc and ipma boxes of HEIF and AVIF images read fields whose sizes the box gives, with
// helpers that leave the reads without names, and pack several fields in a byte. The functions here make the
// fields of these boxes again from the decoded box, and name the fields of item references.

// fieldWriter appends the fields of a box, read from data, which starts at position base in the file.
type fieldWriter struct {
	data   []byte
	base   uint64
	off    int
	max    int
	out    []Field
	capped bool
	short  bool // the box ends before a field
}

// uint reads an unsigned integer of size bytes, 0 to 8, and adds it as a field. A size of 0 adds nothing.
func (w *fieldWriter) uint(size int, name, spec, about string) uint64 {
	if size == 0 || w.short || w.capped {
		return 0
	}
	if w.off+size > len(w.data) {
		w.short = true
		return 0
	}
	var v uint64
	for _, c := range w.data[w.off : w.off+size] {
		v = v<<8 | uint64(c)
	}
	if len(w.out) >= w.max {
		w.capped = true
		return v
	}
	w.out = append(w.out, Field{Pos: w.base + uint64(w.off), Size: size, Kind: fmt.Sprintf("u%d", 8*size), Name: name,
		Value: strconv.FormatUint(v, 10), Spec: spec, About: about})
	w.off += size
	return v
}

// note adds text to the value of the last field.
func (w *fieldWriter) note(text string) {
	if len(w.out) > 0 && !w.capped {
		w.out[len(w.out)-1].Value += " (" + text + ")"
	}
}

// key sets the key of the Info line that shows the last field.
func (w *fieldWriter) key(key string) {
	if len(w.out) > 0 && !w.capped {
		w.out[len(w.out)-1].Key = key
	}
}

// startAfter returns a fieldWriter that keeps the fields before the field named name, and goes on from its
// position, or nil if there is no such field.
func startAfter(name string, fields []Field, data []byte, base uint64, maxFields int) *fieldWriter {
	for i, f := range fields {
		if f.Name == name {
			return &fieldWriter{data: data, base: base, off: int(f.Pos - base), max: maxFields,
				out: append([]Field(nil), fields[:i]...)}
		}
	}
	return nil
}

// constructionMethods are the names of the construction methods of iloc.
var constructionMethods = map[uint64]string{0: "file offset", 1: "idat offset", 2: "item offset"}

// ilocFields returns the fields of an iloc box, made from the decoded box b and data, the bytes of the box,
// which starts at position base in the file. Fields beyond maxFields are left out.
func ilocFields(b *mp4.IlocBox, fields []Field, data []byte, base uint64, maxFields int) ([]Field, bool) {
	w := startAfter("sizes", fields, data, base, maxFields)
	if w == nil {
		return fields, false
	}
	v := w.uint(1, "OffsetSize", "offset_size, length_size", "sizes in bytes of extent_offset and extent_length, "+
		"4 bits each")
	w.note(fmt.Sprintf("offset_size=%d length_size=%d", v>>4, v&0xf))
	w.key("offsetSize")
	indexSpec, indexAbout := "base_offset_size, reserved", "size in bytes of base_offset, and 4 reserved bits"
	if b.Version >= 1 {
		indexSpec, indexAbout = "base_offset_size, index_size", "sizes in bytes of base_offset and extent_index, "+
			"4 bits each"
	}
	v = w.uint(1, "BaseOffsetSize", indexSpec, indexAbout)
	w.note(fmt.Sprintf("base_offset_size=%d index_size=%d", v>>4, v&0xf))
	w.key("baseOffsetSize")
	idSize := 2
	if b.Version == 2 {
		idSize = 4
	}
	w.uint(idSize, "itemCount", "item_count", "number of items that follow")
	for i, item := range b.Items {
		n := strconv.Itoa(i + 1)
		w.uint(idSize, fmt.Sprintf("Items[%d].ItemID", i), "item_ID["+n+"]", "identifier of the item")
		if b.Version >= 1 {
			v := w.uint(2, fmt.Sprintf("Items[%d].ConstructionMethod", i), "reserved, construction_method["+n+"]",
				"12 reserved bits and the construction method in 4 bits")
			w.note(fmt.Sprintf("construction_method=%d %s", v&0xf, constructionMethods[v&0xf]))
		}
		w.uint(2, fmt.Sprintf("Items[%d].DataReferenceIndex", i), "data_reference_index["+n+"]",
			"data entry in dref of the data, or 0 for this file")
		w.uint(int(b.BaseOffsetSize), fmt.Sprintf("Items[%d].BaseOffset", i), "base_offset["+n+"]",
			"offset that the extent offsets start from")
		w.uint(2, fmt.Sprintf("Items[%d].extentCount", i), "extent_count["+n+"]", "number of extents of the item")
		for j := range item.Extents {
			m := "[" + n + "][" + strconv.Itoa(j+1) + "]"
			if b.Version >= 1 {
				w.uint(int(b.IndexSize), fmt.Sprintf("Items[%d].Extents[%d].ItemReferenceIndex", i, j),
					"extent_index"+m, "the item, by its index in the iloc references of the item, whose data the "+
						"extent is in, for construction method 2")
			}
			w.uint(int(b.OffsetSize), fmt.Sprintf("Items[%d].Extents[%d].Offset", i, j), "extent_offset"+m,
				"offset of the extent from the base offset")
			w.uint(int(b.LengthSize), fmt.Sprintf("Items[%d].Extents[%d].Length", i, j), "extent_length"+m,
				"length of the extent in bytes, or 0 for all the data")
		}
	}
	return w.out, w.capped
}

// ipmaFields returns the fields of an ipma box, made from the decoded box b and data, the bytes of the box,
// which starts at position base in the file. Fields beyond maxFields are left out.
func ipmaFields(b *mp4.IpmaBox, fields []Field, data []byte, base uint64, maxFields int) ([]Field, bool) {
	w := startAfter("entryCount", fields, data, base, maxFields)
	if w == nil {
		return fields, false
	}
	w.uint(4, "entryCount", "entry_count", "number of items that follow")
	idSize, assocSize := 2, 1
	if b.Version >= 1 {
		idSize = 4
	}
	if b.Flags&1 != 0 {
		assocSize = 2
	}
	for i, e := range b.Entries {
		n := strconv.Itoa(i + 1)
		w.uint(idSize, fmt.Sprintf("Entries[%d].ItemID", i), "item_ID["+n+"]", "identifier of the item")
		w.uint(1, fmt.Sprintf("Entries[%d].associationCount", i), "association_count["+n+"]",
			"number of properties of the item that follow")
		for j := range e.Associations {
			m := "[" + n + "][" + strconv.Itoa(j+1) + "]"
			v := w.uint(assocSize, fmt.Sprintf("Entries[%d].Associations[%d]", i, j),
				"essential"+m+", property_index"+m, "1 bit that tells if the property is essential, and the "+
					"index of the property in ipco, from 1, or 0 for none")
			bits := uint(8*assocSize - 1)
			w.note(fmt.Sprintf("essential=%d property_index=%d", v>>bits, v&(1<<bits-1)))
		}
	}
	return w.out, w.capped
}

// nameItemRef names the fields of an item reference in iref, which starts at position pos: the item that refers
// and the items that it refers to.
func nameItemRef(fields []Field, pos uint64) {
	to := 0
	for i := range fields {
		f := &fields[i]
		if f.Name != "" || f.Pos < pos+8 {
			continue
		}
		if to == 0 && (i+1 < len(fields) && fields[i+1].Name == "refCount") {
			f.Name = "FromItemID"
			continue
		}
		to++
		f.Name = "ToItemIDs #" + strconv.Itoa(to)
	}
}
