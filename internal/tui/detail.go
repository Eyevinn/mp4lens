package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Eyevinn/mp4lens/internal/boxview"
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

	styleHexSize  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleHexType  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleHexVF    = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	styleHexChild = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleHexHit   = lipgloss.NewStyle().Reverse(true)
)

// maxKeyWidth is the widest that the key column of the fields gets.
const maxKeyWidth = 28

// detailContent returns the details of n, for a pane width cells wide.
func (m *Model) detailContent(n *boxview.Node, width int) string {
	var lines []string
	add := func(s ...string) { lines = append(lines, s...) }

	add(styleFaint.Render(strings.ReplaceAll(n.Path(), "/", " › ")))
	head := styleTitle.Render(n.Name())
	if ln := n.LongName(); ln != "" {
		head += "  " + ln + " box"
	}
	add(head, m.statusText(n), "")

	facts := [][2]string{
		{"Position", fmt.Sprintf("%d (%#x)", n.Pos, n.Pos)},
		{"Size", byteCount(n.Size)},
		{"Ends at", fmt.Sprintf("%d (%#x)", n.End(), n.End())},
		{"Header", headerText(n)},
	}
	if v, f, ok := n.VersionFlags(); ok {
		facts = append(facts, [2]string{"Version", fmt.Sprint(v)}, [2]string{"Flags", fmt.Sprintf("%06x", f)})
	}
	if len(n.Children) > 0 {
		facts = append(facts, [2]string{"Child boxes", fmt.Sprint(len(n.Children))})
	}
	add(factLines(facts, width)...)

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

	add("", styleHead.Render("Fields"))
	add(fieldLines(n.Fields())...)
	add("")
	add(m.hexLines(n, width)...)
	return strings.Join(lines, "\n")
}

func (m *Model) statusText(n *boxview.Node) string {
	switch {
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

func fieldLines(fields []boxview.Field) []string {
	if len(fields) == 0 {
		return []string{styleFaint.Render("No fields")}
	}
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
		lines = append(lines, styleFaint.Render(key)+strings.Repeat(" ", max(1, keyW+2-ansi.StringWidth(key)))+f.Value)
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
)

// hexLines returns the hex dump of the first bytes of n, with its header and the headers of its children marked.
func (m *Model) hexLines(n *boxview.Node, width int) []string {
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
		return []string{title, styleFaint.Render("No bytes of this box are in the file.")}
	case count < total:
		title += styleFaint.Render(fmt.Sprintf("  first %d of %d", count, total))
	case total < n.Size:
		title += styleFaint.Render(fmt.Sprintf("  %d of %d are in the file", total, n.Size))
	}
	lines := []string{title, hexLegend(n)}
	if err != nil && k < int(count) {
		lines = append(lines, styleErr.Render("Read error: "+err.Error()))
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
		start := int(c.Pos - n.Pos)
		for i := start; i < min(start+max(8, c.HdrLen), len(buf)); i++ {
			kinds[i] = kindChild
		}
	}
	hit := m.hitMask(n.Pos, len(buf))

	perRow := 16
	if width < 76 {
		perRow = 8
	}
	for r := 0; r < len(buf); r += perRow {
		var hx, asc strings.Builder
		for j := 0; j < perRow; j++ {
			i := r + j
			if j == 8 {
				hx.WriteByte(' ')
			}
			if i >= len(buf) {
				hx.WriteString("   ")
				continue
			}
			b := buf[i]
			ch := "."
			if b >= 0x20 && b < 0x7f {
				ch = string(rune(b))
			}
			st := hexStyle(kinds[i], hit[i])
			hx.WriteString(" " + st.Render(fmt.Sprintf("%02x", b)))
			asc.WriteString(st.Render(ch))
		}
		lines = append(lines, styleFaint.Render(fmt.Sprintf("%08x", n.Pos+uint64(r)))+" "+hx.String()+"  "+asc.String())
	}
	if count < total {
		lines = append(lines, styleFaint.Render("Press m for more bytes."))
	}
	return lines
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

func hexStyle(kind byte, hit bool) lipgloss.Style {
	if hit {
		return styleHexHit
	}
	switch kind {
	case kindSize:
		return styleHexSize
	case kindType:
		return styleHexType
	case kindVF:
		return styleHexVF
	case kindChild:
		return styleHexChild
	}
	return lipgloss.NewStyle()
}

func hexLegend(n *boxview.Node) string {
	parts := []string{styleHexSize.Render("size"), styleHexType.Render("type")}
	if _, _, ok := n.VersionFlags(); ok {
		parts = append(parts, styleHexVF.Render("version+flags"))
	}
	if len(n.Children) > 0 {
		parts = append(parts, styleHexChild.Render("child box header"))
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
		{"tab", "switch between the tree and the box pane, which then scrolls"},
		{"/", "search; enter runs it, an empty search clears it"},
		{"n N", "next and previous matching box"},
		{"f", "show only the matching boxes"},
		{"e E", "next and previous box that could not be decoded"},
		{"a", "switch between the main fields and all fields, such as all samples"},
		{"m", "show more bytes"},
		{"esc", "close the help, or clear the search"},
		{"q", "quit"},
	}
	var sb strings.Builder
	sb.WriteString(styleHead.Render("Keys") + "\n")
	for _, k := range keys {
		sb.WriteString(styleKey.Render(fmt.Sprintf("%-15s", k[0])) + k[1] + "\n")
	}
	sb.WriteString("\n" + styleHead.Render("Search") + "\n" + boxview.QueryHelp + "\n")
	return sb.String()
}
