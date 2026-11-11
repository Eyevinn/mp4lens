package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Eyevinn/mp4lens/internal/boxview"
	"github.com/Eyevinn/mp4lens/internal/layout"
)

// A field of the layout of the selected box can be marked, as on the web page: its row in the layout, its entry
// in the fields and its bytes are highlighted, and the footer says what it is. In the box pane, the j and k keys,
// or the arrows, step through the fields, and the mouse marks the field that it points at, by its row, its entry
// or one of its bytes.

// lineKind is what a line of the box pane shows.
type lineKind uint8

const (
	lineOther  lineKind = iota
	lineField           // an entry of the fields, with a field of the layout of the same name
	lineLayout          // a row of the layout
	lineHex             // a row of the hex dump
)

// lineRef tells what a line of the box pane shows, so that the mouse can point at it.
type lineRef struct {
	kind lineKind
	// index is the field of the layout of a field or layout line, or the offset in the box of the first byte
	// of a hex line.
	index int
	// count is the number of bytes of a hex line, and perRow the number of bytes of a full row.
	count, perRow int
}

// paneText is the content of the box pane, with what each line shows.
type paneText struct {
	lines []string
	refs  []lineRef
}

// add adds lines that show ref. A line with line breaks is split, as the viewport splits it.
func (p *paneText) add(ref lineRef, lines ...string) {
	for _, l := range lines {
		for s := range strings.SplitSeq(strings.ReplaceAll(l, "\r\n", "\n"), "\n") {
			p.lines = append(p.lines, s)
			p.refs = append(p.refs, ref)
		}
	}
}

// setLines keeps what the lines of p show, and the screen rows that they start at when the viewport wraps them.
func (m *Model) setLines(p *paneText) {
	w := max(1, m.detail.Width())
	m.lines = p.refs
	m.lineRows = make([]int, len(p.lines)+1)
	for i, l := range p.lines {
		m.lineRows[i+1] = m.lineRows[i] + max(1, (ansi.StringWidth(l)+w-1)/w)
	}
}

// lineAt returns what the line of the box pane at cell x, y of the screen shows, and the column of the cell in
// the line. It returns false outside the box pane, and for the help and the summary.
func (m *Model) lineAt(x, y int) (lineRef, int, bool) {
	w := m.detail.Width()
	col, row := x-m.treeW-1, y-2 // inside the border, below the title line
	if col < 0 || col >= w || row < 0 || row >= m.detail.Height() || len(m.lines) == 0 {
		return lineRef{}, 0, false
	}
	v := m.detail.YOffset() + row
	i := sort.SearchInts(m.lineRows, v+1) - 1
	if i < 0 || i >= len(m.lines) {
		return lineRef{}, 0, false
	}
	return m.lines[i], (v-m.lineRows[i])*w + col, true
}

// hexByteAt returns the offset in the box of the byte at column col of a hex line, in the hex or in the text.
func hexByteAt(ref lineRef, col int) (int, bool) {
	// The line is the position in 8 hex digits and a space, then " xx" for each byte, with one more space
	// before the ninth, then two spaces and the text.
	hexW := 3 * ref.perRow
	if ref.perRow > 8 {
		hexW++
	}
	j := -1
	switch c := col - 9; {
	case c >= 0 && c < 24:
		j = c / 3
	case ref.perRow > 8 && c > 24 && c < hexW:
		j = (c - 1) / 3
	case c >= hexW+2 && c < hexW+2+ref.perRow:
		j = c - hexW - 2
	}
	if j < 0 || j >= ref.count {
		return 0, false
	}
	return ref.index + j, true
}

// clearMark removes the mark and what the mouse points at, for another box or tree.
func (m *Model) clearMark() {
	m.mark, m.hover, m.hoverText = -1, -1, ""
}

// boxLayout returns the layout of n, or nil if n has none, such as a part or a plain container.
func (m *Model) boxLayout(n *boxview.Node) *layout.Result {
	if n == nil || n.PlainContainer() {
		return nil
	}
	return m.layoutOf(n)
}

// shownField returns the field of the layout of n that is shown: the one that the mouse points at, or else the
// marked one, or nil.
func (m *Model) shownField(n *boxview.Node) *layout.Field {
	i := m.hover
	if i < 0 {
		i = m.mark
	}
	if i < 0 || m.layoutNode != n || m.layoutRes == nil || i >= len(m.layoutRes.Fields) {
		return nil
	}
	return &m.layoutRes.Fields[i]
}

// fieldKey handles key k in the box pane, if the box has a layout: j and k, or the arrows, mark the next and
// previous field of the layout, from the field that is shown, and g and G, or home and end, the first and last.
// The box pane scrolls to show the bytes of the field, and its row too if they fit. It returns false for other
// keys, and for the help, the summary and boxes without layout fields, where the keys scroll the pane.
func (m *Model) fieldKey(k string) bool {
	if m.showHelp || m.showSummary {
		return false
	}
	n := m.selected()
	res := m.boxLayout(n)
	if res == nil || len(res.Fields) == 0 {
		return false
	}
	last, i := len(res.Fields)-1, m.hover
	if i < 0 {
		i = m.mark
	}
	switch k {
	case "down", "j":
		i = min(last, i+1)
	case "up", "k":
		i = max(0, i-1)
	case "home", "g":
		i = 0
	case "end", "G":
		i = last
	default:
		return false
	}
	m.mark, m.hover, m.hoverText, m.status = i, -1, "", ""
	for m.layoutLimit <= i {
		m.layoutLimit *= 4
	}
	off := res.Fields[i].Pos - n.Pos
	for uint64(m.hexLimit) <= off && m.hexLimit < markHexMax {
		m.hexLimit *= 4
	}
	m.updateDetail()
	m.scrollToField(i, off)
	return true
}

// scrollToField scrolls the box pane to show the row of the hex dump with the byte at offset off of the box, and
// row i of the layout too, if both fit.
func (m *Model) scrollToField(i int, off uint64) {
	row, hex := -1, -1
	for k, r := range m.lines {
		switch {
		case row < 0 && r.kind == lineLayout && r.index == i:
			row = k
		case hex < 0 && r.kind == lineHex && off >= uint64(r.index) && off < uint64(r.index+r.count):
			hex = k
		}
	}
	y, h := m.detail.YOffset(), m.detail.Height()
	show := func(first, last int) bool {
		if first < 0 || m.lineRows[last+1]-m.lineRows[first] > h {
			return false
		}
		top, bottom := m.lineRows[first], m.lineRows[last+1]
		if top < y {
			y = top
		} else if bottom > y+h {
			y = bottom - h
		}
		return true
	}
	if row < 0 || hex < 0 || !show(min(row, hex), max(row, hex)) {
		if !show(hex, hex) {
			show(row, row)
		}
	}
	m.detail.SetYOffset(y)
}

// point shows what the mouse points at in the box pane, at cell x, y of the screen: a field of the layout, by its
// row, its entry in the fields or one of its bytes, or else a byte. A click on a byte of a child box opens the
// child box.
func (m *Model) point(x, y int, click bool) {
	n := m.selected()
	hover, text := -1, ""
	if ref, col, ok := m.lineAt(x, y); ok && n != nil {
		res := m.boxLayout(n)
		switch ref.kind {
		case lineField, lineLayout:
			if res != nil && ref.index < len(res.Fields) {
				hover = ref.index
				text = fieldText(n, res.Fields[hover])
			}
		case lineHex:
			i, ok := hexByteAt(ref, col)
			if !ok {
				break
			}
			pos := n.Pos + uint64(i)
			text = fmt.Sprintf("%#x · %d · ", pos, pos)
			if hover = fieldAt(res, pos); hover >= 0 {
				f := res.Fields[hover]
				text += styleBold.Render(layoutName(f)) + " · " + f.Kind + " · " + f.Value
			} else if c := childAt(n, pos); c != nil {
				if click {
					m.selectNode(c)
					return
				}
				text += fmt.Sprintf("%s +%d (click to open)", c.Name(), pos-c.Pos)
			} else {
				text += fmt.Sprintf("%s +%d", n.Name(), i)
			}
		}
	}
	m.hoverText = text
	if hover != m.hover {
		m.hover = hover
		m.updateDetail()
	}
}

// markText says what the marked field is, for the footer.
func (m *Model) markText() string {
	n := m.selected()
	if m.hover >= 0 || m.layoutNode != n || m.layoutRes == nil || m.mark >= len(m.layoutRes.Fields) {
		return ""
	}
	return fmt.Sprintf("field %d of %d · ", m.mark+1, len(m.layoutRes.Fields)) + fieldText(n, m.layoutRes.Fields[m.mark])
}

// fieldText says what field f of box n is: its name, kind, size, position and value.
func fieldText(n *boxview.Node, f layout.Field) string {
	return fmt.Sprintf("%s · %s · %d bytes at %#x (+%d) · %s", styleBold.Render(layoutName(f)), f.Kind, f.Size, f.Pos,
		f.Pos-n.Pos, f.Value)
}

// layoutName returns the name of a layout field, or says that it has none.
func layoutName(f layout.Field) string {
	if name := fieldName(f); name != "" {
		return name
	}
	return "field without a name"
}

// fieldAt returns the first field of res that holds the byte at pos, or -1.
func fieldAt(res *layout.Result, pos uint64) int {
	if res == nil {
		return -1
	}
	for i, f := range res.Fields {
		if pos >= f.Pos && pos < f.Pos+uint64(f.Size) {
			return i
		}
	}
	return -1
}

// childAt returns the child box of n that holds the byte at pos, or nil.
func childAt(n *boxview.Node, pos uint64) *boxview.Node {
	for _, c := range n.Children {
		if c.IsBox() && pos >= c.Pos && pos < c.End() {
			return c
		}
	}
	return nil
}

// fieldKey returns the name of a layout field as normalKey writes it, without the number of a repeated field,
// to match the key of an entry of the fields.
func fieldKey(name string) string {
	name, _, _ = strings.Cut(name, " #")
	return normalKey(name)
}

// linkedFields returns the first field of res for each name, as fieldKey writes it.
func linkedFields(res *layout.Result) map[string]int {
	linked := map[string]int{}
	if res == nil {
		return linked
	}
	for i, f := range res.Fields {
		if k := fieldKey(f.Name); k != "" {
			if _, ok := linked[k]; !ok {
				linked[k] = i
			}
		}
	}
	return linked
}
