package boxview

import (
	"io"

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
	data := make([]byte, avail)
	if _, err := r.ReadAt(data, int64(n.Pos)); err != nil {
		return &layout.Result{Err: err}
	}
	var skip []layout.Range
	for _, c := range n.Children {
		if c.IsBox() {
			skip = append(skip, layout.Range{Start: c.Pos, End: c.End()})
		}
	}
	return layout.Box(data, n.Pos, skip, MaxLayoutFields)
}
