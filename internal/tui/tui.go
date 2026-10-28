// Package tui is the terminal user interface of mp4lens. It shows the box tree of a file on the left and the
// selected box on the right, with its fields and bytes, and searches the boxes.
package tui

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Eyevinn/mp4lens/internal/boxview"
	"github.com/Eyevinn/mp4lens/internal/layout"
	"github.com/Eyevinn/mp4lens/internal/summary"
)

// Options are the settings of the user interface.
type Options struct {
	// Name is the file name to show.
	Name string
	// Levels are the info levels of the decoded fields, as for mp4ff-info, for example "all:1".
	Levels string
	// Query is a search to run at the start.
	Query string
}

// allLevels are the info levels that the a key switches to.
const allLevels = "all:1"

// hexStart is the number of bytes of a box that the hex dump shows at first.
const hexStart = 1024

// layoutStart is the number of layout fields that the box pane shows at first.
const layoutStart = 64

// autoExpandLimit is the largest number of boxes for which the first two levels start expanded.
const autoExpandLimit = 400

type pane int

const (
	treePane pane = iota
	detailPane
)

// Model is the state of the user interface.
type Model struct {
	r      io.ReaderAt
	size   int64
	name   string
	levels string
	tree   *boxview.Tree
	index  map[*boxview.Node]int // index in tree.Nodes
	errs   []*boxview.Node

	expanded map[*boxview.Node]bool
	rows     []*boxview.Node // the visible rows of the tree
	cursor   int             // the selected row
	top      int             // the first row in view

	focus       pane
	detail      viewport.Model
	hexLimit    int
	layoutLimit int
	showHelp    bool
	showSummary bool
	sum         *summary.Summary // the summary of the file, made when first shown

	layoutNode *boxview.Node // the box of layoutRes
	layoutRes  *layout.Result

	input     textinput.Model
	searching bool
	query     string
	result    *boxview.Result
	matched   map[*boxview.Node]bool
	onlyMatch bool

	status        string
	width, height int
	treeW, bodyH  int
}

// searchDoneMsg carries the result of a search.
type searchDoneMsg struct {
	query  string
	result *boxview.Result
	err    error
}

// New decodes the file in the first size bytes of r and returns the user interface for it.
func New(r io.ReaderAt, size int64, opts Options) *Model {
	m := &Model{r: r, size: size, name: opts.Name, levels: opts.Levels, query: opts.Query, hexLimit: hexStart,
		layoutLimit: layoutStart, width: 100, height: 30}
	m.input = textinput.New()
	m.input.Prompt = "/"
	m.input.Placeholder = "type:trun  trackID=1  @1234  hex:000001  is:error  (? for help)"
	m.detail = viewport.New()
	m.detail.SoftWrap = true
	m.setTree(boxview.Decode(r, size, m.levels), false)
	m.showSummary = len(m.errs) == 0 && opts.Query == "" && len(m.tree.Boxes) > 0
	m.layout()
	return m
}

// setTree shows tree t. If keep is true, the boxes with the same keys as the expanded and selected boxes
// of the current tree are expanded and selected.
func (m *Model) setTree(t *boxview.Tree, keep bool) {
	var expandedKeys []string
	selKey := ""
	if keep {
		for n := range m.expanded {
			expandedKeys = append(expandedKeys, n.Key)
		}
		if s := m.selected(); s != nil {
			selKey = s.Key
		}
	}
	m.tree, m.errs, m.sum = t, t.Errors(), nil
	m.index = make(map[*boxview.Node]int, len(t.Nodes))
	for i, n := range t.Nodes {
		m.index[n] = i
	}
	m.expanded = make(map[*boxview.Node]bool)
	m.result, m.matched, m.onlyMatch = nil, nil, false
	m.rows, m.cursor = nil, 0
	m.layoutNode, m.layoutRes = nil, nil
	var sel *boxview.Node
	if keep {
		for _, key := range expandedKeys {
			if n := t.ByKey(key); n != nil {
				m.expanded[n] = true
			}
		}
		sel = t.ByKey(selKey)
	} else {
		if len(t.Nodes) <= autoExpandLimit {
			for _, n := range t.Nodes {
				if n.Depth < 2 && len(n.Children) > 0 {
					m.expanded[n] = true
				}
			}
		}
		for _, e := range m.errs {
			m.reveal(e)
		}
		if len(m.errs) > 0 {
			sel = m.errs[0]
		}
	}
	if sel == nil && len(t.Nodes) > 0 {
		sel = t.Nodes[0]
	}
	m.rebuildRows()
	m.selectNode(sel)
}

// Init starts the search given in the options, if any.
func (m *Model) Init() tea.Cmd {
	if m.query == "" {
		return nil
	}
	m.input.SetValue(m.query)
	return m.search(m.query)
}

// selected returns the selected box, or nil if there are no boxes.
func (m *Model) selected() *boxview.Node {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor]
}

// reveal expands the boxes above n.
func (m *Model) reveal(n *boxview.Node) {
	for p := n.Parent; p != nil; p = p.Parent {
		m.expanded[p] = true
	}
}

// rebuildRows makes the list of visible rows, and keeps the selected box selected if it is still visible.
func (m *Model) rebuildRows() {
	sel := m.selected()
	m.rows = nil
	var onPath map[*boxview.Node]bool
	if m.onlyMatch && m.result != nil {
		onPath = make(map[*boxview.Node]bool)
		for _, n := range m.result.Nodes {
			for x := n; x != nil; x = x.Parent {
				onPath[x] = true
			}
		}
	}
	var walk func(nodes []*boxview.Node)
	walk = func(nodes []*boxview.Node) {
		for _, n := range nodes {
			if onPath != nil && !onPath[n] {
				continue
			}
			m.rows = append(m.rows, n)
			if len(n.Children) > 0 && (m.expanded[n] || onPath != nil) {
				walk(n.Children)
			}
		}
	}
	walk(m.tree.Boxes)
	m.cursor = 0
	if sel != nil {
		m.cursor = m.rowOf(sel)
	}
}

// rowOf returns the row of n, or of the nearest box above it that is visible, or 0.
func (m *Model) rowOf(n *boxview.Node) int {
	for x := n; x != nil; x = x.Parent {
		for i, r := range m.rows {
			if r == x {
				return i
			}
		}
	}
	return 0
}

// selectNode makes n visible and selects it.
func (m *Model) selectNode(n *boxview.Node) {
	if n == nil {
		m.cursor = 0
		m.updateDetail()
		return
	}
	if i := m.rowOf(n); i >= len(m.rows) || m.rows[i] != n {
		m.reveal(n)
		if m.onlyMatch && !m.matched[n] {
			m.onlyMatch = false
		}
		m.rebuildRows()
	}
	m.setCursor(m.rowOf(n))
}

// setCursor selects row i and scrolls it into view.
func (m *Model) setCursor(i int) {
	prev := m.selected()
	m.cursor = max(0, min(i, len(m.rows)-1))
	h := m.treeRows()
	if m.cursor < m.top {
		m.top = m.cursor
	} else if m.cursor >= m.top+h {
		m.top = m.cursor - h + 1
	}
	m.top = max(0, min(m.top, len(m.rows)-h))
	if m.selected() != prev {
		m.hexLimit, m.layoutLimit = hexStart, layoutStart
		m.showSummary = false
		m.updateDetail()
		m.detail.GotoTop()
	}
}

// treeRows returns the number of tree rows in view.
func (m *Model) treeRows() int {
	return max(1, m.bodyH-2)
}

func (m *Model) layout() {
	m.treeW = max(34, min(56, m.width*34/100))
	if m.width < 80 {
		m.treeW = m.width / 2
	}
	m.bodyH = max(4, m.height-2)
	m.detail.SetWidth(max(1, m.width-m.treeW-2))
	m.detail.SetHeight(max(1, m.bodyH-2))
	m.input.SetWidth(max(10, m.width-2))
	m.setCursor(m.cursor)
	m.updateDetail()
}

func (m *Model) updateDetail() {
	if m.showHelp {
		m.detail.SetContent(helpText())
		return
	}
	if m.showSummary {
		if m.sum == nil {
			m.sum = summary.Of(m.tree, m.r)
		}
		m.detail.SetContent(summaryText(m.sum))
		return
	}
	n := m.selected()
	if n == nil {
		m.detail.SetContent(styleFaint.Render("The file has no boxes."))
		return
	}
	m.detail.SetContent(m.detailContent(n, m.detail.Width()))
}

// Update handles a message.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
	case searchDoneMsg:
		m.searchDone(msg)
	case tea.KeyPressMsg:
		if m.searching {
			return m, m.searchKey(msg)
		}
		return m, m.key(msg)
	case tea.MouseWheelMsg:
		m.wheel(msg)
	case tea.MouseClickMsg:
		m.click(msg)
	}
	return m, nil
}

func (m *Model) searchKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		m.searching = false
		m.input.Blur()
		return m.search(strings.TrimSpace(m.input.Value()))
	case "esc", "ctrl+c":
		m.searching = false
		m.input.Blur()
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

// search starts a search for query, or clears the search if query is empty.
func (m *Model) search(query string) tea.Cmd {
	if query == "" {
		m.clearSearch()
		return nil
	}
	q, err := boxview.ParseQuery(query)
	if err != nil {
		m.status = styleErr.Render("Search: " + err.Error())
		return nil
	}
	m.status = "Searching…"
	t, r := m.tree, m.r
	return func() tea.Msg {
		res, err := t.Search(q, r)
		return searchDoneMsg{query: query, result: res, err: err}
	}
}

func (m *Model) clearSearch() {
	m.query, m.result, m.matched, m.status = "", nil, nil, ""
	if m.onlyMatch {
		m.onlyMatch = false
		m.rebuildRows()
	}
	m.updateDetail()
}

func (m *Model) searchDone(msg searchDoneMsg) {
	if msg.err != nil {
		m.status = styleErr.Render("Search: " + msg.err.Error())
		return
	}
	m.query, m.result = msg.query, msg.result
	m.matched = make(map[*boxview.Node]bool, len(msg.result.Nodes))
	for _, n := range msg.result.Nodes {
		m.matched[n] = true
		m.reveal(n)
	}
	m.rebuildRows()
	if len(msg.result.Nodes) == 0 {
		m.status = styleWarn.Render("No boxes match " + strconv.Quote(m.query))
		m.updateDetail()
		return
	}
	m.status = ""
	m.selectNode(msg.result.Nodes[0])
	m.updateDetail()
}

// next selects the first box of list after the selected box in file order, or the last one before it
// if back is true. list is in file order.
func (m *Model) next(list []*boxview.Node, back bool, what string) {
	if len(list) == 0 {
		m.status = "No " + what
		return
	}
	cur := -1
	if s := m.selected(); s != nil {
		cur = m.index[s]
	}
	target := list[0]
	if back {
		target = list[len(list)-1]
		for i := len(list) - 1; i >= 0; i-- {
			if m.index[list[i]] < cur {
				target = list[i]
				break
			}
		}
	} else {
		for _, n := range list {
			if m.index[n] > cur {
				target = n
				break
			}
		}
	}
	m.status = ""
	m.selectNode(target)
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		m.updateDetail()
		m.detail.GotoTop()
		return nil
	case "s":
		m.showSummary = !m.showSummary
		m.showHelp = false
		m.updateDetail()
		m.detail.GotoTop()
		return nil
	case "esc":
		if m.showHelp || m.showSummary {
			m.showHelp, m.showSummary = false, false
			m.updateDetail()
		} else if m.result != nil {
			m.clearSearch()
		}
		return nil
	case "/":
		m.searching = true
		m.input.SetValue(m.query)
		m.input.CursorEnd()
		return m.input.Focus()
	case "n", "N":
		if m.result == nil {
			m.status = "No search; press / to search"
			return nil
		}
		m.next(m.result.Nodes, k == "N", "boxes match "+strconv.Quote(m.query))
		return nil
	case "e", "E":
		m.next(m.errs, k == "E", "boxes with errors")
		return nil
	case "f":
		if m.result == nil || len(m.result.Nodes) == 0 {
			m.status = "No matching boxes to show; press / to search"
			return nil
		}
		m.onlyMatch = !m.onlyMatch
		m.rebuildRows()
		m.setCursor(m.cursor)
		return nil
	case "a":
		m.toggleLevels()
		return nil
	case "m":
		m.hexLimit *= 4
		m.layoutLimit *= 4
		m.updateDetail()
		return nil
	case "tab", "shift+tab":
		m.focus = 1 - m.focus
		return nil
	}
	if m.focus == detailPane {
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return cmd
	}
	m.treeKey(k)
	return nil
}

// toggleLevels switches between the main fields and all fields, and decodes the file again.
func (m *Model) toggleLevels() {
	if m.levels == "" {
		m.levels = allLevels
	} else {
		m.levels = ""
	}
	query, onlyMatch := m.query, m.onlyMatch
	m.setTree(boxview.Decode(m.r, m.size, m.levels), true)
	m.status = "Fields: main"
	if m.levels != "" {
		m.status = "Fields: all (" + m.levels + ")"
	}
	if query == "" {
		return
	}
	// The boxes are new, so the search runs again on them.
	q, err := boxview.ParseQuery(query)
	if err != nil {
		return
	}
	res, err := m.tree.Search(q, m.r)
	if err != nil {
		return
	}
	sel, status := m.selected(), m.status
	m.searchDone(searchDoneMsg{query: query, result: res})
	m.onlyMatch = onlyMatch
	m.rebuildRows()
	m.selectNode(sel)
	m.status = status
}

func (m *Model) treeKey(k string) {
	n := m.selected()
	if n == nil {
		return
	}
	switch k {
	case "up", "k":
		m.setCursor(m.cursor - 1)
	case "down", "j":
		m.setCursor(m.cursor + 1)
	case "pgup", "ctrl+b":
		m.setCursor(m.cursor - m.treeRows())
	case "pgdown", "ctrl+f":
		m.setCursor(m.cursor + m.treeRows())
	case "home", "g":
		m.setCursor(0)
	case "end", "G":
		m.setCursor(len(m.rows) - 1)
	case "right", "l":
		if len(n.Children) == 0 {
			return
		}
		if !m.expanded[n] && !m.onlyMatch {
			m.toggle(n)
		} else if m.cursor+1 < len(m.rows) && m.rows[m.cursor+1].Parent == n {
			m.setCursor(m.cursor + 1)
		}
	case "left", "h":
		if len(n.Children) > 0 && m.expanded[n] && !m.onlyMatch {
			m.toggle(n)
		} else if n.Parent != nil {
			m.selectNode(n.Parent)
		}
	case "enter", "space":
		if len(n.Children) > 0 {
			m.toggle(n)
		}
	case "+", "=":
		for _, x := range m.tree.Nodes {
			if len(x.Children) > 0 {
				m.expanded[x] = true
			}
		}
		m.rebuildRows()
		m.setCursor(m.cursor)
	case "-":
		clear(m.expanded)
		m.rebuildRows()
		m.setCursor(m.cursor)
	}
}

func (m *Model) toggle(n *boxview.Node) {
	m.expanded[n] = !m.expanded[n]
	m.rebuildRows()
	m.setCursor(m.cursor)
}

func (m *Model) wheel(msg tea.MouseWheelMsg) {
	delta := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -3
	case tea.MouseWheelDown:
		delta = 3
	default:
		return
	}
	if msg.X >= m.treeW {
		if delta < 0 {
			m.detail.ScrollUp(-delta)
		} else {
			m.detail.ScrollDown(delta)
		}
		return
	}
	m.top = max(0, min(m.top+delta, len(m.rows)-m.treeRows()))
}

func (m *Model) click(msg tea.MouseClickMsg) {
	if msg.Button != tea.MouseLeft {
		return
	}
	if msg.X >= m.treeW {
		m.focus = detailPane
		return
	}
	m.focus = treePane
	i := m.top + msg.Y - 2 // below the title line and the top border
	if msg.Y < 2 || msg.Y >= m.bodyH || i >= len(m.rows) {
		return
	}
	n := m.rows[i]
	twisty := 1 + 2*n.Depth
	if len(n.Children) > 0 && msg.X >= twisty && msg.X < twisty+2 {
		m.setCursor(i)
		m.toggle(n)
		return
	}
	m.setCursor(i)
}

// View draws the user interface.
func (m *Model) View() tea.View {
	var sb strings.Builder
	sb.WriteString(m.titleLine())
	sb.WriteByte('\n')
	boxes := 0
	for _, n := range m.tree.Nodes {
		if n.IsBox() {
			boxes++
		}
	}
	treeTitle := fmt.Sprintf(" Boxes %d ", boxes)
	if m.onlyMatch {
		treeTitle = fmt.Sprintf(" Matching boxes %d ", len(m.result.Nodes))
	}
	left := frame(treeTitle, m.treeLines(), m.treeW, m.bodyH, m.focus == treePane)
	detailTitle := " Box "
	if m.showHelp {
		detailTitle = " Help "
	}
	right := frame(detailTitle, strings.Split(m.detail.View(), "\n"), m.width-m.treeW, m.bodyH, m.focus == detailPane)
	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, right))
	sb.WriteByte('\n')
	sb.WriteString(m.footer())
	v := tea.NewView(sb.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "mp4lens " + m.name
	return v
}

func (m *Model) titleLine() string {
	parts := []string{styleTitle.Render("mp4lens"), styleBold.Render(m.name), sizeText(uint64(m.size))}
	switch n := len(m.errs); {
	case m.tree.ReadErr != nil:
		parts = append(parts, styleErr.Render("read error: "+m.tree.ReadErr.Error()))
	case n == 0:
		parts = append(parts, styleOK.Render("all boxes decoded"))
	case n == 1:
		parts = append(parts, styleErr.Render("1 box could not be decoded"))
	default:
		parts = append(parts, styleErr.Render(fmt.Sprintf("%d boxes could not be decoded", n)))
	}
	if m.tree.QuickTime != "" {
		parts = append(parts, styleQT.Render("QuickTime file"))
	}
	if m.levels != "" {
		parts = append(parts, styleFaint.Render("fields: "+m.levels))
	}
	return fit(" "+strings.Join(parts, styleFaint.Render(" · ")), m.width)
}

func (m *Model) footer() string {
	if m.searching {
		return fit(m.input.View(), m.width)
	}
	left := m.status
	if left == "" && m.result != nil {
		pos := 0
		if s := m.selected(); s != nil && m.matched[s] {
			for i, n := range m.result.Nodes {
				if n == s {
					pos = i + 1
				}
			}
		}
		count := len(m.result.Nodes)
		left = fmt.Sprintf("/%s  %s of %d", m.query, map[bool]string{true: strconv.Itoa(pos), false: "-"}[pos > 0], count)
		if m.result.Capped {
			left += fmt.Sprintf(" (byte search stopped at %d hits)", boxview.MaxHits)
		}
		left = styleMatch.Render(left)
	}
	hints := keyHints([][2]string{{"/", "search"}, {"n/N", "match"}, {"e", "error"}, {"f", "filter"},
		{"a", "all fields"}, {"s", "summary"}, {"tab", "pane"}, {"?", "help"}, {"q", "quit"}})
	space := m.width - ansi.StringWidth(left) - 2
	if space < 20 {
		return fit(" "+left, m.width)
	}
	hints = ansi.Truncate(hints, space, "…")
	pad := m.width - 1 - ansi.StringWidth(left) - ansi.StringWidth(hints)
	return " " + left + strings.Repeat(" ", max(1, pad)) + hints
}

func (m *Model) treeLines() []string {
	h := m.treeRows()
	inner := m.treeW - 2
	lines := make([]string, 0, h)
	for i := m.top; i < len(m.rows) && i < m.top+h; i++ {
		lines = append(lines, m.treeLine(m.rows[i], inner, i == m.cursor))
	}
	return lines
}

// treeLine draws the row of n in width cells.
func (m *Model) treeLine(n *boxview.Node, width int, selected bool) string {
	twisty := "  "
	if len(n.Children) > 0 {
		twisty = "▸ "
		if m.expanded[n] || m.onlyMatch {
			twisty = "▾ "
		}
	}
	name := n.Name()
	mark := ""
	switch {
	case n.Err != nil:
		mark = " ●"
	case n.ChildErr:
		mark = " ○"
	}
	qt := ""
	if n.QuickTime != "" {
		qt = " QT"
	}
	left := strings.Repeat("  ", n.Depth) + twisty + name + mark + qt
	right := sizeText(n.Size) + " @" + strconv.FormatUint(n.Pos, 10)
	if ansi.StringWidth(left)+1+ansi.StringWidth(right) > width {
		right = sizeText(n.Size)
		if ansi.StringWidth(left)+1+ansi.StringWidth(right) > width {
			right = ""
		}
	}
	gap := max(1, width-ansi.StringWidth(left)-ansi.StringWidth(right))
	if selected {
		st := styleSel
		if m.focus != treePane {
			st = styleSelBlur
		}
		return st.Render(fit(left+strings.Repeat(" ", gap)+right, width))
	}
	nameStyle := styleBold
	if n.Part != nil {
		nameStyle = stylePart
	}
	switch {
	case n.Err != nil:
		nameStyle = styleErr.Bold(true)
	case m.matched[n]:
		nameStyle = styleMatch.Bold(true)
	}
	markStyle := styleErr
	if n.Err == nil {
		markStyle = styleWarn
	}
	line := strings.Repeat("  ", n.Depth) + styleFaint.Render(twisty) + nameStyle.Render(name) + markStyle.Render(mark) +
		styleQT.Render(qt) + strings.Repeat(" ", gap) + styleFaint.Render(right)
	return fit(line, width)
}

// frame draws lines in a box with a rounded border, w cells wide and h lines high, with title in the top border.
func frame(title string, lines []string, w, h int, focused bool) string {
	bs := styleBorder
	if focused {
		bs = styleBorderFocus
	}
	inner := w - 2
	title = ansi.Truncate(title, max(0, inner-1), "")
	var sb strings.Builder
	sb.WriteString(bs.Render("╭─" + title + strings.Repeat("─", max(0, inner-1-ansi.StringWidth(title))) + "╮"))
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		sb.WriteString("\n" + bs.Render("│") + fit(line, inner) + bs.Render("│"))
	}
	sb.WriteString("\n" + bs.Render("╰"+strings.Repeat("─", max(0, inner))+"╯"))
	return sb.String()
}

// fit truncates or pads s to w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

func keyHints(hints [][2]string) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = styleKey.Render(h[0]) + " " + styleFaint.Render(h[1])
	}
	return strings.Join(parts, "  ")
}

// sizeText writes a size in bytes, with binary units from 1 KiB.
func sizeText(n uint64) string {
	if n < 1024 {
		return strconv.FormatUint(n, 10) + " B"
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	v, u := float64(n)/1024, 0
	for v >= 1024 && u < len(units)-1 {
		v /= 1024
		u++
	}
	if v < 10 {
		return fmt.Sprintf("%.2f %s", v, units[u])
	}
	return fmt.Sprintf("%.1f %s", v, units[u])
}
