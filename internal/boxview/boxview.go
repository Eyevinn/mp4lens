// Package boxview prepares the box tree of an MP4 file for viewing. It decodes the tree with the boxtree
// package of mp4ff, links each box to its parent, gives it a key and a long name, splits its Info lines into
// fields, adds the structures inside boxes, such as parameter sets, as parts, marks what is specific to
// QuickTime, finds the byte layout of a box, and searches the tree.
package boxview

import (
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/Eyevinn/mp4ff/boxtree"
	"github.com/Eyevinn/mp4ff/mp4"

	"github.com/Eyevinn/mp4lens/internal/parts"
)

// Tree is the box tree of a file.
type Tree struct {
	// Size is the size of the file.
	Size int64
	// Boxes are the top-level boxes.
	Boxes []*Node
	// Nodes are all boxes, in file order.
	Nodes []*Node
	// ReadErr is the error that stopped the reading of the file, if any. The boxes before it are in the tree.
	ReadErr error
	// QuickTime tells why the file is a QuickTime file, or is empty if it is not one.
	QuickTime string
}

// Node is a box in the tree.
type Node struct {
	*boxtree.Node
	// Key identifies the box by its index among its siblings at each level, as "1/0/2".
	Key string
	// Parent is the parent box, or nil for a top-level box.
	Parent *Node
	// Children are the child boxes. The field hides the Children field of the embedded boxtree.Node.
	Children []*Node
	// Depth is 0 for a top-level box, 1 for its children, and so on.
	Depth int
	// Part is the structure that the node is, for a node that is not a box but a part of its parent box.
	Part *parts.Part
	// QuickTime tells what is specific to QuickTime in the box or part, or is empty.
	QuickTime string
}

// Field is a line of the Info output of a box, other than its first line.
type Field struct {
	// Key is the name of the field. It is empty for a line that is not of the form "- key: value".
	Key string `json:"key,omitempty"`
	// Value is the value of the field, or the whole line, trimmed, if Key is empty.
	Value string `json:"value"`
	// Indent is the nesting level of the line within the box.
	Indent int `json:"indent,omitempty"`
}

// Decode decodes the box tree of the first size bytes of r. infoLevels sets the details of the
// Info lines, as for mp4ff-info, for example "all:1".
func Decode(r io.ReaderAt, size int64, infoLevels string) *Tree {
	bt, err := boxtree.Decode(r, size, infoLevels)
	t := New(bt, r)
	t.ReadErr = err
	return t
}

// New makes the tree of bt. The parts of the boxes are read from r, the file, and left out if r is nil.
func New(bt *boxtree.Tree, r io.ReaderAt) *Tree {
	t := &Tree{Size: bt.Size}
	t.Boxes = t.add(bt.Boxes, nil, "", r)
	t.QuickTime = fileQuickTime(t)
	return t
}

// maxPartsBox is the largest box whose parts are found.
const maxPartsBox = 1 << 20

func (t *Tree) add(nodes []*boxtree.Node, parent *Node, prefix string, r io.ReaderAt) []*Node {
	out := make([]*Node, 0, len(nodes))
	for i, bn := range nodes {
		n := &Node{Node: bn, Key: prefix + strconv.Itoa(i), Parent: parent}
		if parent != nil {
			n.Depth = parent.Depth + 1
		}
		n.QuickTime = boxQuickTime(n)
		t.Nodes = append(t.Nodes, n)
		n.Children = t.add(bn.Children, n, n.Key+"/", r)
		if ps := t.partsOf(n, r); len(ps) > 0 {
			if (n.Err != nil || n.ChildErr) && ps[0].QuickTime != "" {
				// mp4ff decodes a QuickTime user data text atom as a metadata item container, which fails
				// on a child box that is not there. The text items replace those children.
				n.Children, n.ChildErr, n.Err = nil, false, errQuickTimeText
				t.dropAfter(n)
			}
			n.Children = append(n.Children, t.addParts(ps, n, len(n.Children))...)
		}
		out = append(out, n)
	}
	return out
}

// errQuickTimeText is the error of a QuickTime user data text atom, which mp4ff cannot decode.
var errQuickTimeText = errors.New("mp4ff decodes this QuickTime user data text atom as a metadata item, and fails")

// dropAfter removes the nodes after n from Nodes, which are its children when they have just been added.
func (t *Tree) dropAfter(n *Node) {
	for i := len(t.Nodes) - 1; i >= 0; i-- {
		if t.Nodes[i] == n {
			t.Nodes = t.Nodes[:i+1]
			return
		}
	}
}

// partsOf returns the parts of the box of n, read from r.
func (t *Tree) partsOf(n *Node, r io.ReaderAt) []*parts.Part {
	if r == nil || n.HdrLen == 0 || n.Size > maxPartsBox || n.End() > uint64(t.Size) {
		return nil
	}
	quickTimeText := n.QuickTime != "" && n.Parent != nil && n.Parent.Type == "udta"
	if n.Box == nil && !quickTimeText {
		return nil
	}
	data := make([]byte, n.Size)
	if _, err := r.ReadAt(data, int64(n.Pos)); err != nil {
		return nil
	}
	parentType := ""
	if n.Parent != nil {
		parentType = n.Parent.Type
	}
	return parts.Of(n.Type, n.Box, data, n.Pos, parentType)
}

// addParts makes nodes of the parts ps of the box or part parent, and adds them to Nodes. The first gets index
// first among the children of parent.
func (t *Tree) addParts(ps []*parts.Part, parent *Node, first int) []*Node {
	out := make([]*Node, 0, len(ps))
	for i, p := range ps {
		bn := &boxtree.Node{Type: p.Name, Pos: p.Pos, Size: p.Size, Info: p.Info, Err: p.Err}
		n := &Node{Node: bn, Key: parent.Key + "/" + strconv.Itoa(first+i), Parent: parent, Depth: parent.Depth + 1,
			Part: p, QuickTime: p.QuickTime}
		t.Nodes = append(t.Nodes, n)
		n.Children = t.addParts(p.Children, n, 0)
		for _, c := range n.Children {
			if c.Err != nil || c.ChildErr {
				n.ChildErr = true
			}
		}
		if n.Err != nil || n.ChildErr {
			for x := parent; x != nil && !x.ChildErr; x = x.Parent {
				x.ChildErr = true
			}
		}
		out = append(out, n)
	}
	return out
}

// Errors returns the boxes that could not be decoded, in file order.
func (t *Tree) Errors() []*Node {
	var errs []*Node
	for _, n := range t.Nodes {
		if n.Err != nil {
			errs = append(errs, n)
		}
	}
	return errs
}

// ByKey returns the box with key, or nil if there is none.
func (t *Tree) ByKey(key string) *Node {
	nodes := t.Boxes
	var n *Node
	for _, part := range strings.Split(key, "/") {
		i, err := strconv.Atoi(part)
		if err != nil || i < 0 || i >= len(nodes) {
			return nil
		}
		n = nodes[i]
		nodes = n.Children
	}
	return n
}

// At returns the innermost box that holds the byte at pos, or nil if no box holds it.
func (t *Tree) At(pos uint64) *Node {
	var found *Node
	for nodes := t.Boxes; ; {
		var next *Node
		for _, n := range nodes {
			if pos >= n.Pos && pos < n.End() {
				next = n
				break
			}
		}
		if next == nil {
			return found
		}
		found, nodes = next, next.Children
	}
}

// End returns the position after the box. In a truncated file, it can be beyond the end of the file.
func (n *Node) End() uint64 {
	return n.Pos + n.Size
}

// Name returns the name of a part, such as "SPS", and otherwise the name of the box from the embedded node.
func (n *Node) Name() string {
	if n.Part != nil {
		return n.Part.Name
	}
	return n.Node.Name()
}

// LongName returns the name of the box type, such as "Movie" for moov, or what a part is, or "" if it is not known.
func (n *Node) LongName() string {
	if n.Part != nil {
		return n.Part.LongName
	}
	if n.HdrLen == 0 {
		return ""
	}
	return boxNames[n.Type]
}

// IsBox tells if the node is a box, which may be bytes without a box header, and not a part of a box.
func (n *Node) IsBox() bool {
	return n.Part == nil
}

// Path returns the names of the boxes from the top-level box down to n, as "moov/trak/mdia".
func (n *Node) Path() string {
	var parts []string
	for x := n; x != nil; x = x.Parent {
		parts = append(parts, x.Name())
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "/")
}

// Unknown tells if mp4ff decoded the box without interpreting its payload, since it does not know the box type.
func (n *Node) Unknown() bool {
	_, ok := n.Box.(*mp4.UnknownBox)
	return ok
}

// Truncated tells if the box continues beyond the end of the file of size bytes.
func (n *Node) Truncated(size int64) bool {
	return n.End() > uint64(size)
}

var versionFlags = regexp.MustCompile(` version=(\d+) flags=([0-9a-f]{6})\b`)

// VersionFlags returns the version and flags of a full box, from the first Info line.
// ok is false for other boxes.
func (n *Node) VersionFlags() (version int, flags uint32, ok bool) {
	if len(n.Info) == 0 {
		return 0, 0, false
	}
	m := versionFlags.FindStringSubmatch(n.Info[0])
	if m == nil {
		return 0, 0, false
	}
	v, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}
	f, err := strconv.ParseUint(m[2], 16, 32)
	if err != nil {
		return 0, 0, false
	}
	return v, uint32(f), true
}

var fieldLine = regexp.MustCompile(`^(\s*)- (.+?): (.*)$`)

// Fields returns the Info lines of the box after the first one, split into keys and values.
func (n *Node) Fields() []Field {
	if len(n.Info) < 2 {
		return nil
	}
	fields := make([]Field, 0, len(n.Info)-1)
	for _, line := range n.Info[1:] {
		if m := fieldLine.FindStringSubmatch(line); m != nil {
			fields = append(fields, Field{Key: m[2], Value: m[3], Indent: indentLevel(m[1])})
			continue
		}
		lead := len(line) - len(strings.TrimLeft(line, " "))
		fields = append(fields, Field{Value: strings.TrimPrefix(strings.TrimSpace(line), "- "), Indent: indentLevel(line[:lead])})
	}
	return fields
}

// indentLevel returns the nesting level of a field line with the leading spaces lead. Fields of the box
// itself start with one space, as " - key: value", and each level below adds two spaces.
func indentLevel(lead string) int {
	return max(0, (len(lead)-1)/2)
}
