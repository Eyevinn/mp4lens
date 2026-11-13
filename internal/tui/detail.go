package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Eyevinn/mp4lens/internal/boxview"
	"github.com/Eyevinn/mp4lens/internal/layout"
	"github.com/Eyevinn/mp4lens/internal/summary"
)

// The styles use the 16 ANSI colors, so that they follow the color theme of the terminal.
var (
	styleTitle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleBold        = lipgloss.NewStyle().Bold(true)
	styleFaint       = lipgloss.NewStyle().Faint(true)
	styleErr         = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleWarn        = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleOK          = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleMatch       = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	styleKey         = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleHead        = lipgloss.NewStyle().Bold(true).Underline(true)
	styleSel         = lipgloss.NewStyle().Reverse(true)
	styleSelBlur     = lipgloss.NewStyle().Underline(true).Bold(true)
	styleBorder      = lipgloss.NewStyle().Faint(true)
	styleBorderFocus = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleQT          = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	stylePart        = lipgloss.NewStyle().Italic(true)
	styleContainer   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Faint(true)

	styleHexSize  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleHexType  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleHexVF    = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	styleHexChild = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	// A search hit is yellow, as on the web page, so that it differs from the marked field.
	styleHexHit = lipgloss.NewStyle().Background(lipgloss.Color("3")).Foreground(lipgloss.Color("0"))
	// styleMarked marks a field of the layout: its row, its entry in the fields and its bytes.
	styleMarked = lipgloss.NewStyle().Reverse(true)
)

// maxKeyWidth is the widest that the key column of the fields gets.
const maxKeyWidth = 28

// detailContent returns the details of n, for a pane width cells wide.
func (m *Model) detailContent(n *boxview.Node, width int) *paneText {
	p := &paneText{}
	add := func(s ...string) { p.add(lineRef{}, s...) }

	add(styleFaint.Render(strings.ReplaceAll(m.tree.RefPath(n), "/", " › ")))
	head := styleTitle.Render(n.Name())
	switch ln := n.LongName(); {
	case n.Part != nil:
		head += "  " + ln + styleFaint.Render(", in "+n.Parent.Name())
	case ln != "":
		head += "  " + ln + " box"
	}
	if n.PlainContainer() {
		head += "  " + styleContainer.Render("container")
	}
	add(head, m.statusText(n), "")

	facts := [][2]string{
		{"Position", fmt.Sprintf("%d (%#x)", n.Pos, n.Pos)},
		{"Size", byteCount(n.Size)},
		{"Ends at", fmt.Sprintf("%d (%#x)", n.End(), n.End())},
	}
	if n.IsBox() {
		facts = append(facts, [2]string{"Header", headerText(n)})
	}
	if v, f, ok := n.VersionFlags(); ok {
		facts = append(facts, [2]string{"Version", fmt.Sprint(v)}, [2]string{"Flags", fmt.Sprintf("%06x", f)})
	}
	boxes, nparts := 0, 0
	for _, c := range n.Children {
		if c.IsBox() {
			boxes++
		} else {
			nparts++
		}
	}
	if boxes > 0 {
		facts = append(facts, [2]string{"Child boxes", fmt.Sprint(boxes)})
	}
	if nparts > 0 {
		facts = append(facts, [2]string{"Parts", fmt.Sprint(nparts)})
	}
	add(factLines(facts, width)...)
	if n.QuickTime != "" {
		add("", styleQT.Render("QuickTime: ")+n.QuickTime)
	}

	if n.Err != nil {
		add("", styleErr.Render("Error: ")+n.Err.Error())
	}
	if n.ChildErr {
		add("", styleWarn.Render("A box inside could not be decoded: ")+firstError(n).Path())
	}
	if n.Truncated(m.size) {
		add("", styleWarn.Render(fmt.Sprintf("The box continues %d bytes past the end of the file.", n.End()-uint64(m.size))))
	}
	if n.Unknown() {
		add("", styleFaint.Render("mp4ff does not know this box type, so only its header is decoded."))
	}

	// A plain container has no fields of its own, so its layout is only the box header.
	res := m.boxLayout(n)
	shown := m.shownField(n)
	if fields := n.Fields(); len(fields) > 0 {
		add("", styleHead.Render("Fields"))
		linked, shownKey := linkedFields(res), ""
		if shown != nil {
			shownKey = fieldKey(*shown)
		}
		for i, line := range fieldLines(fields, quickTimeKeys(res), shownKey) {
			ref := lineRef{}
			if j, ok := linked[normalKey(fields[i].Key)]; ok && fields[i].Key != "" {
				ref = lineRef{kind: lineField, index: j}
			}
			p.add(ref, line)
		}
	}
	if res != nil && (len(res.Fields) > 0 || res.Err != nil) {
		add("")
		m.layoutLines(p, n, res, shown)
	}
	add("")
	m.hexLines(p, n, width, shown)
	// The meanings come after the bytes, so that the layout and the bytes are close.
	if res != nil {
		add(aboutLines(res.Fields)...)
	}
	return p
}

func (m *Model) statusText(n *boxview.Node) string {
	switch {
	case n.Part != nil && n.Err == nil && !n.ChildErr:
		return styleOK.Render("✓ decoded part of " + n.Parent.Name())
	case n.Err != nil:
		return styleErr.Render("● could not be decoded")
	case n.ChildErr:
		return styleWarn.Render("○ a box inside could not be decoded")
	case n.Unknown():
		return styleFaint.Render("? unknown box type")
	case n.HdrLen == 0:
		return styleWarn.Render("bytes without a box header")
	}
	return styleOK.Render("✓ decoded")
}

// byteCount writes n bytes, with the size in binary units from 1 KiB.
func byteCount(n uint64) string {
	if n < 1024 {
		return fmt.Sprintf("%d bytes", n)
	}
	return fmt.Sprintf("%d (%s)", n, sizeText(n))
}

func headerText(n *boxview.Node) string {
	switch n.HdrLen {
	case 0:
		return "none"
	case 16:
		return "16 bytes, 64-bit size"
	}
	return fmt.Sprintf("%d bytes", n.HdrLen)
}

// firstError returns the first box below n that could not be decoded, or n if there is none.
func firstError(n *boxview.Node) *boxview.Node {
	for _, c := range n.Children {
		if c.Err != nil {
			return c
		}
		if c.ChildErr {
			return firstError(c)
		}
	}
	return n
}

// factLines lays out facts in two columns if width allows it.
func factLines(facts [][2]string, width int) []string {
	const keyW, valW = 12, 22
	cell := func(f [2]string, w int) string {
		return styleFaint.Render(fmt.Sprintf("%-*s", keyW, f[0])) + fmt.Sprintf("%-*s", w, f[1])
	}
	var lines []string
	if width < 2*(keyW+valW) {
		for _, f := range facts {
			lines = append(lines, cell(f, 0))
		}
		return lines
	}
	for i := 0; i < len(facts); i += 2 {
		line := cell(facts[i], valW)
		if i+1 < len(facts) {
			line += cell(facts[i+1], 0)
		}
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return lines
}

// fieldLines lays out the fields, one line each, with a QT tag after those whose keys, in lower case without
// spaces and underscores, start with "quicktime" or are in qtKeys. The keys that are shownKey in that form are
// marked.
func fieldLines(fields []boxview.Field, qtKeys map[string]bool, shownKey string) []string {
	keyW := 0
	for _, f := range fields {
		keyW = max(keyW, min(maxKeyWidth, 2*f.Indent+ansi.StringWidth(f.Key)))
	}
	lines := make([]string, 0, len(fields))
	for _, f := range fields {
		indent := strings.Repeat("  ", f.Indent)
		if f.Key == "" {
			lines = append(lines, indent+f.Value)
			continue
		}
		key := indent + f.Key
		nk := normalKey(f.Key)
		keyStyle := styleFaint
		if nk == shownKey {
			keyStyle = styleMarked
		}
		line := indent + keyStyle.Render(f.Key) + strings.Repeat(" ", max(1, keyW+2-ansi.StringWidth(key))) + f.Value
		if strings.HasPrefix(nk, "quicktime") || qtKeys[nk] {
			line += "  " + styleQT.Render("QT")
		}
		lines = append(lines, line)
	}
	return lines
}

// byte kinds of the hex dump
const (
	kindNone = iota
	kindSize
	kindType
	kindVF
	kindChild
	kindHit    // found by the search
	kindMarked // a byte of the marked field
)

// hexStyles are the styles of the byte kinds.
var hexStyles = [...]lipgloss.Style{kindNone: lipgloss.NewStyle(), kindSize: styleHexSize, kindType: styleHexType,
	kindVF: styleHexVF, kindChild: styleHexChild, kindHit: styleHexHit, kindMarked: styleMarked}

// hexLines adds the hex dump of the first bytes of n to p, with its header, the headers of its children, the bytes
// that the search found and the bytes of the shown field marked.
func (m *Model) hexLines(p *paneText, n *boxview.Node, width int, shown *layout.Field) {
	total := uint64(0)
	if n.Pos < uint64(m.size) {
		total = min(n.Size, uint64(m.size)-n.Pos)
	}
	count := min(total, uint64(m.hexLimit))
	buf := make([]byte, count)
	k, err := m.r.ReadAt(buf, int64(n.Pos))
	buf = buf[:k]
	title := styleHead.Render("Bytes")
	switch {
	case total == 0:
		p.add(lineRef{}, title, styleFaint.Render("No bytes of this box are in the file."))
		return
	case count < total:
		title += styleFaint.Render(fmt.Sprintf("  first %d of %d", count, total))
	case total < n.Size:
		title += styleFaint.Render(fmt.Sprintf("  %d of %d are in the file", total, n.Size))
	}
	hit := m.hitMask(n.Pos, len(buf))
	p.add(lineRef{}, title, hexLegend(n, slices.Contains(hit, true)))
	if err != nil && k < int(count) {
		p.add(lineRef{}, styleErr.Render("Read error: "+err.Error()))
	}

	kinds := make([]byte, len(buf))
	if n.HdrLen > 0 {
		for i := range min(n.HdrLen, len(buf)) {
			kinds[i] = kindSize
			if i >= 4 && i < 8 {
				kinds[i] = kindType
			}
		}
		if _, _, ok := n.VersionFlags(); ok {
			for i := n.HdrLen; i < min(n.HdrLen+4, len(buf)); i++ {
				kinds[i] = kindVF
			}
		}
	}
	for _, c := range n.Children {
		if !c.IsBox() {
			continue
		}
		start := int(c.Pos - n.Pos)
		for i := start; i < min(start+max(8, c.HdrLen), len(buf)); i++ {
			kinds[i] = kindChild
		}
	}
	for i, h := range hit {
		if h {
			kinds[i] = kindHit
		}
	}
	if shown != nil && shown.Pos >= n.Pos {
		start := shown.Pos - n.Pos
		for i := start; i < min(start+uint64(shown.Size), uint64(len(buf))); i++ {
			kinds[i] = kindMarked
		}
	}

	perRow := 16
	if width < 76 {
		perRow = 8
	}
	for r := 0; r < len(buf); r += perRow {
		var hx, asc styledRuns
		for j := 0; j < perRow; j++ {
			i := r + j
			sep := " "
			if j == 8 {
				sep = "  "
			}
			if i >= len(buf) {
				hx.plain(sep + "  ")
				continue
			}
			b := buf[i]
			ch := "."
			if b >= 0x20 && b < 0x7f {
				ch = string(rune(b))
			}
			hx.add(kinds[i], sep, fmt.Sprintf("%02x", b))
			asc.add(kinds[i], "", ch)
		}
		line := styleFaint.Render(fmt.Sprintf("%08x", n.Pos+uint64(r))) + " " + hx.String() + "  " + asc.String()
		p.add(lineRef{kind: lineHex, index: r, count: min(perRow, len(buf)-r), perRow: perRow}, line)
	}
	if count < total {
		p.add(lineRef{}, styleFaint.Render("Press m for more bytes."))
	}
}

// styledRuns writes bytes of the hex dump in runs of the same kind, each rendered in its style at once. The
// separator before a byte of the same kind as the one before it is in the run, so that the bytes of a field or
// a search hit are marked as one block.
type styledRuns struct {
	sb, run strings.Builder
	kind    byte
}

// add writes text of kind, after sep.
func (r *styledRuns) add(kind byte, sep, text string) {
	if r.run.Len() > 0 && kind == r.kind {
		r.run.WriteString(sep + text)
		return
	}
	r.flush()
	r.sb.WriteString(sep)
	r.kind = kind
	r.run.WriteString(text)
}

// plain writes s without a style.
func (r *styledRuns) plain(s string) {
	r.flush()
	r.sb.WriteString(s)
}

func (r *styledRuns) flush() {
	if r.run.Len() == 0 {
		return
	}
	if r.kind == kindNone {
		r.sb.WriteString(r.run.String())
	} else {
		r.sb.WriteString(hexStyles[r.kind].Render(r.run.String()))
	}
	r.run.Reset()
}

// String returns what was written.
func (r *styledRuns) String() string {
	r.flush()
	return r.sb.String()
}

// hitMask marks the bytes from pos that the search found.
func (m *Model) hitMask(pos uint64, n int) []bool {
	mask := make([]bool, n)
	if m.result == nil {
		return mask
	}
	end := pos + uint64(n)
	for _, spans := range m.result.Hits {
		for _, s := range spans {
			for p := max(s.Pos, pos); p < min(s.Pos+uint64(s.Len), end); p++ {
				mask[p-pos] = true
			}
		}
	}
	return mask
}

// hexLegend says what the colors of the hex dump of n are, with the search hits if hits is true.
func hexLegend(n *boxview.Node, hits bool) string {
	parts := []string{styleHexSize.Render("size"), styleHexType.Render("type")}
	if _, _, ok := n.VersionFlags(); ok {
		parts = append(parts, styleHexVF.Render("version+flags"))
	}
	for _, c := range n.Children {
		if c.IsBox() {
			parts = append(parts, styleHexChild.Render("child box header"))
			break
		}
	}
	if hits {
		parts = append(parts, styleHexHit.Render("search hit"))
	}
	return strings.Join(parts, "  ")
}

// helpText is the content of the detail pane with the help.
func helpText() string {
	keys := [][2]string{
		{"↑ ↓ k j", "move in the tree"},
		{"← h", "close the box, or go to its parent"},
		{"→ l", "open the box, or go to its first child"},
		{"enter space", "open or close the box"},
		{"+ -", "open or close all boxes"},
		{"pgup pgdn g G", "page up and down, first and last box"},
		{"tab", "switch between the tree and the box pane"},
		{"/", "search; enter runs it, an empty search clears it"},
		{"n N", "next and previous matching box"},
		{"f", "show only the matching boxes"},
		{"e E", "next and previous box that could not be decoded"},
		{"a", "switch between the main fields and all fields, such as all samples"},
		{"m", "show more bytes and layout fields"},
		{"s", "show the summary of the file: brands, movie, metadata, chapters and tracks"},
		{"esc", "close the help or summary, remove the mark, or clear the search"},
		{"q", "quit"},
	}
	var sb strings.Builder
	sb.WriteString(styleHead.Render("Keys") + "\n")
	for _, k := range keys {
		sb.WriteString(styleKey.Render(fmt.Sprintf("%-15s", k[0])) + k[1] + "\n")
	}
	sb.WriteString("\n" + styleHead.Render("Box pane") + "\n")
	pane := [][2]string{
		{"↓ ↑ j k", "mark the next and previous field of the layout, and its bytes"},
		{"g G", "mark the first and last field"},
		{"pgup pgdn", "scroll; also b and space, u and d, and the mouse wheel"},
		{"esc", "remove the mark"},
	}
	for _, k := range pane {
		sb.WriteString(styleKey.Render(fmt.Sprintf("%-15s", k[0])) + k[1] + "\n")
	}
	sb.WriteString("A box without layout fields scrolls with ↓ ↑ j k.\n")
	sb.WriteString("\n" + styleHead.Render("Mouse") + "\n")
	sb.WriteString("Point at a field, a row of the layout or a byte to mark the field and its bytes; the footer says\n" +
		"what the byte or field is. Click a byte of a child box to open it.\n")
	sb.WriteString("\n" + styleHead.Render("Search") + "\n" + boxview.QueryHelp + "\n")
	return sb.String()
}

// summaryText returns the summary s for the box pane.
func summaryText(s *summary.Summary) string {
	var sb strings.Builder
	for i, sec := range s.Sections {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(styleHead.Render(sec.Title) + "\n")
		for _, r := range sec.Rows {
			sb.WriteString("  " + styleKey.Render(r.Name+":") + " " + r.Value + "\n")
		}
	}
	sb.WriteString("\n" + styleFaint.Render("Move in the tree, or press s, to see the boxes.") + "\n")
	return sb.String()
}

// layoutOf returns the layout of n, and keeps it for the next call.
func (m *Model) layoutOf(n *boxview.Node) *layout.Result {
	if m.layoutNode != n {
		m.layoutNode, m.layoutRes = n, m.tree.Layout(n, m.r)
	}
	return m.layoutRes
}

// maxLayoutName is the widest that the name column of the layout gets.
const maxLayoutName = 26

// layoutLines adds the fields of res, the layout of n, to p: the file position in hex, the offset in the box, the
// size, the kind of read, the name and the value, with a QT tag for QuickTime fields, whose notes follow. The row
// of the shown field is marked. The meanings of the fields are left to aboutLines.
func (m *Model) layoutLines(p *paneText, n *boxview.Node, res *layout.Result, shown *layout.Field) {
	title := styleHead.Render("Layout") + styleFaint.Render(fmt.Sprintf("  %d fields as mp4ff reads them", len(res.Fields)))
	if res.Capped {
		title += styleFaint.Render(", more not recorded")
	}
	if res.Spec != "" {
		title += styleFaint.Render(" · " + res.Spec)
	}
	p.add(lineRef{}, title)
	if res.Err != nil && !n.ChildErr {
		p.add(lineRef{}, styleErr.Render("The decoder stopped: ")+res.Err.Error())
	}
	nameW := 4
	for _, f := range res.Fields {
		nameW = max(nameW, min(maxLayoutName, ansi.StringWidth(fieldName(f))))
	}
	var notes []string
	seen := map[string]bool{}
	for i, f := range res.Fields {
		if i == m.layoutLimit {
			p.add(lineRef{}, styleFaint.Render(fmt.Sprintf("%d more fields. Press m for more.", len(res.Fields)-i)))
			break
		}
		name := fieldName(f)
		if name == "" {
			name = styleFaint.Render("(no name)")
		}
		line := styleFaint.Render(fmt.Sprintf("%08x +%-5d %5d %-6s ", f.Pos, f.Pos-n.Pos, f.Size, f.Kind)) +
			name + strings.Repeat(" ", max(1, nameW+1-ansi.StringWidth(name))) + f.Value
		if f.QuickTime != "" {
			line += "  " + styleQT.Render("QT")
			if !seen[f.QuickTime] {
				seen[f.QuickTime] = true
				notes = append(notes, styleQT.Render("QT ")+fieldName(f)+": "+f.QuickTime)
			}
		}
		if shown == &res.Fields[i] {
			line = styleMarked.Render(ansi.Strip(line))
		}
		p.add(lineRef{kind: lineLayout, index: i}, line)
	}
	p.add(lineRef{}, notes...)
}

// fieldName returns the name of a layout field in the specification, or else the name that mp4ff gives it.
func fieldName(f layout.Field) string {
	if f.Spec != "" {
		return f.Spec
	}
	return f.Name
}

// aboutLines say what each field of the layout is, once for all the repeats of a field.
func aboutLines(fields []layout.Field) []string {
	var lines []string
	seen := map[string]bool{}
	for _, f := range fields {
		if f.About == "" {
			continue
		}
		base, _, _ := strings.Cut(f.Spec, "[")
		if seen[base] {
			continue
		}
		seen[base] = true
		lines = append(lines, styleFaint.Render(base+": ")+f.About)
	}
	if len(lines) > 0 {
		lines = append([]string{"", styleHead.Render("Field meanings")}, lines...)
	}
	return lines
}

// quickTimeKeys returns the keys of the entries of the fields that show the QuickTime fields of res, as fieldKey
// writes them.
func quickTimeKeys(res *layout.Result) map[string]bool {
	keys := map[string]bool{}
	if res == nil {
		return keys
	}
	for _, f := range res.Fields {
		if f.QuickTime != "" {
			keys[fieldKey(f)] = true
		}
	}
	return keys
}

// normalKey returns key in lower case without spaces and underscores, so that "quickTimeVendor",
// "quicktime_vendor" and "QuickTimeVendor" are the same.
func normalKey(key string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "_", "").Replace(key))
}
