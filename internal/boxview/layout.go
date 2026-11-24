package boxview

import (
	"io"

	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/Eyevinn/mp4lens/internal/layout"
)

// maxLayoutRead is the largest box that Layout decodes. Larger boxes, and mdat boxes, get the layout of their
// header.
const maxLayoutRead = 64 << 20

// MaxLayoutFields is the largest number of fields in a layout.
const MaxLayoutFields = 20000

// Layout returns the byte layout of the box of n, read from r: the fields that its mp4ff decoder reads, without
// those of its child boxes. It returns nil for a part and for bytes without a box header.
func (t *Tree) Layout(n *Node, r io.ReaderAt) *layout.Result {
	if n.Part != nil || n.HdrLen == 0 || r == nil || n.Pos >= uint64(t.Size) {
		return nil
	}
	avail := min(n.Size, uint64(t.Size)-n.Pos)
	if n.Type == "mdat" || avail > maxLayoutRead {
		hdr := make([]byte, min(avail, 16))
		if _, err := r.ReadAt(hdr, int64(n.Pos)); err != nil {
			return &layout.Result{Err: err}
		}
		return layout.Header(hdr, n.Pos)
	}
	var skip []layout.Range
	for _, c := range n.Children {
		if c.IsBox() {
			skip = append(skip, layout.Range{Start: c.Pos, End: c.End()})
		}
	}
	if outer, syntax := decodedBy(n); outer != nil {
		return t.layoutWithin(n, outer, syntax, skip, r)
	}
	data := make([]byte, avail)
	if _, err := r.ReadAt(data, int64(n.Pos)); err != nil {
		return &layout.Result{Err: err}
	}
	res := layout.Box(data, n.Pos, skip, MaxLayoutFields)
	switch {
	case n.Type == "data" && n.Parent != nil && n.Parent.Parent != nil && n.Parent.Parent.Type == "ilst":
		layout.ItemData(n.Parent.Type, freeformName(n.Parent), res.Fields, data, n.Pos)
	case n.Type == "tkhd":
		layout.AddDuration(res.Fields, "Duration", movieTimescale(n))
	case n.Type == "mehd":
		layout.AddDuration(res.Fields, "FragmentDuration", movieTimescale(n))
	}
	return res
}

// decodedBy returns the box that decodes the box of n, when it is not n itself, and the key of the syntax of the
// box of n: the iref box for an item reference, whose type is a reference type, such as cdsc, that has another
// layout as a track reference, and the mebx sample entry for its key table, which has another layout than the
// keys box of QuickTime metadata, and for the keys in it, whose types are key IDs.
func decodedBy(n *Node) (*Node, string) {
	switch p := n.Parent; {
	case p == nil:
		return nil, ""
	case p.Type == "iref":
		return p, "itemRef"
	case n.Type == "keys" && inMebx(n):
		return p, "mebxKeys"
	case p.Type == "keys" && inMebx(p):
		return p.Parent, "mebxKey"
	}
	return nil, ""
}

// layoutWithin returns the layout of the box of n, which the box of outer decodes, read from r. The fields in skip
// are left out.
func (t *Tree) layoutWithin(n, outer *Node, syntax string, skip []layout.Range, r io.ReaderAt) *layout.Result {
	if outer.End() > uint64(t.Size) || outer.Size > maxLayoutRead {
		return nil
	}
	data := make([]byte, outer.Size)
	if _, err := r.ReadAt(data, int64(outer.Pos)); err != nil {
		return &layout.Result{Err: err}
	}
	return layout.Within(data, outer.Pos, layout.Range{Start: n.Pos, End: n.End()}, skip, syntax, MaxLayoutFields)
}

// movieTimescale returns the time scale of the mvhd box in the moov box two levels up from n, or 0.
func movieTimescale(n *Node) uint64 {
	if n.Parent == nil || n.Parent.Parent == nil {
		return 0
	}
	for _, c := range n.Parent.Parent.Children {
		if mvhd, ok := c.Box.(*mp4.MvhdBox); ok {
			return uint64(mvhd.Timescale)
		}
	}
	return 0
}

// freeformName returns the name of a freeform metadata item ----, such as iTunSMPB, or "".
func freeformName(item *Node) string {
	if item.Type != "----" {
		return ""
	}
	for _, c := range item.Children {
		if name, ok := c.Box.(*mp4.NameBox); ok {
			return name.Name
		}
	}
	return ""
}
