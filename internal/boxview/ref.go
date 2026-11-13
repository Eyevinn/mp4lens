package boxview

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// RefHelp tells how Find finds boxes and parts.
const RefHelp = `A box is named by its type and its order number among the boxes of that type in the file, counted
from 0, as moof[0] for the first moof box and moof[-1] for the last. A box that is the only one of its type
needs no number, as moov. Parts, such as SPS[1], are named in the same way. -find and -extract print the
path of each box with these names, as moov/trak[1]/mdia[1]/minf[1]/stbl[1]/stsd[1], and -extract takes such
a path as well as the last name of it.`

// index lists the nodes by name and gives each its order number among the nodes with the same name.
func (t *Tree) index() {
	t.byName = make(map[string][]*Node)
	for _, n := range t.Nodes {
		name := n.Name()
		n.order = len(t.byName[name])
		t.byName[name] = append(t.byName[name], n)
	}
}

// Ref returns the name of n with its order number among the boxes or parts with that name in the file, such as
// moof[2] for the third moof box, or just the name if n is the only one. Find finds n by it.
func (t *Tree) Ref(n *Node) string {
	name := n.Name()
	if len(t.byName[name]) == 1 {
		return name
	}
	return fmt.Sprintf("%s[%d]", name, n.order)
}

// RefPath returns the path of n with the order numbers of the boxes on it, such as
// moov/trak[1]/mdia[1]/minf[1]/stbl[1]/stsd[1].
func (t *Tree) RefPath(n *Node) string {
	var refs []string
	for x := n; x != nil; x = x.Parent {
		refs = append(refs, t.Ref(x))
	}
	slices.Reverse(refs)
	return strings.Join(refs, "/")
}

// Find returns the box or part named by ref, as described in RefHelp.
func (t *Tree) Find(ref string) (*Node, error) {
	parentPath, last := "", ref
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		parentPath, last = ref[:i], ref[i+1:]
	}
	name, number := last, ""
	if strings.HasSuffix(last, "]") {
		if i := strings.LastIndex(last, "["); i > 0 {
			name, number = last[:i], last[i+1:len(last)-1]
		}
	}
	if name == "" {
		return nil, fmt.Errorf("%q does not name a box", ref)
	}
	nodes := t.byName[name]
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%s: there is no %s in the file", ref, name)
	}
	var n *Node
	if number == "" {
		if len(nodes) > 1 {
			return nil, fmt.Errorf("%s: there are %d of them, so give the order number of one, from %s[0] to %s[%d]",
				ref, len(nodes), name, name, len(nodes)-1)
		}
		n = nodes[0]
	} else {
		i, err := strconv.Atoi(number)
		if err != nil {
			return nil, fmt.Errorf("%s: the order number is not an integer", ref)
		}
		if i < 0 {
			i += len(nodes)
		}
		if i < 0 || i >= len(nodes) {
			return nil, fmt.Errorf("%s: there are %d of them, from %s[0] to %s[%d]", ref, len(nodes), name, name,
				len(nodes)-1)
		}
		n = nodes[i]
	}
	if parentPath != "" && (n.Parent == nil || parentPath != n.Parent.Path() && parentPath != t.RefPath(n.Parent)) {
		return nil, fmt.Errorf("%s: the path of %s is %s", ref, t.Ref(n), t.RefPath(n))
	}
	return n, nil
}
