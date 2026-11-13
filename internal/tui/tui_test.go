package tui

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const testW, testH = 120, 40

// newModel opens a file in testdata, changed by edit if it is not nil.
func newModel(t *testing.T, name string, edit func([]byte), opts Options) *Model {
	t.Helper()
	data, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(data)
	}
	opts.Name = name
	m := New(bytes.NewReader(data), int64(len(data)), opts)
	m.Update(tea.WindowSizeMsg{Width: testW, Height: testH})
	if cmd := m.Init(); cmd != nil {
		run(m, cmd)
	}
	return m
}

// run runs cmd and gives its message to m, as the program would.
func run(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if msg := cmd(); msg != nil {
		m.Update(msg)
	}
}

// press sends keys to m. A key is a key name, such as "enter", or a character.
func press(m *Model, keys ...string) {
	codes := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "up": tea.KeyUp,
		"down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight, "space": tea.KeySpace}
	for _, k := range keys {
		msg := tea.KeyPressMsg{Code: codes[k]}
		if msg.Code == 0 {
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		_, cmd := m.Update(msg)
		if k == "enter" {
			run(m, cmd) // The search. Other commands, such as the cursor blink of the input, are left out.
		}
	}
}

// typeText types s into the search input.
func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func screen(m *Model) string {
	return ansi.Strip(m.View().Content)
}

func corruptStsz(data []byte) {
	pos := bytes.Index(data, []byte("stsz")) - 4
	binary.BigEndian.PutUint32(data[pos+16:], 1)
}

func selectedPath(m *Model) string {
	if s := m.selected(); s != nil {
		return s.Path()
	}
	return ""
}

func TestViewSize(t *testing.T) {
	for _, name := range []string{"init.mp4", "1.m4s", "prog_8s.mp4", "init_truncated.mp4"} {
		m := newModel(t, name, nil, Options{})
		for _, size := range [][2]int{{testW, testH}, {60, 15}, {200, 60}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			lines := strings.Split(m.View().Content, "\n")
			if len(lines) != size[1] {
				t.Errorf("%s at %v: %d lines", name, size, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != size[0] {
					t.Errorf("%s at %v: line %d is %d wide: %q", name, size, i, w, ansi.Strip(l))
					break
				}
			}
		}
	}
}

func TestStartsAtError(t *testing.T) {
	m := newModel(t, "init.mp4", corruptStsz, Options{})
	if got := selectedPath(m); got != "moov/trak/mdia/minf/stbl/stsz" {
		t.Errorf("selected %s", got)
	}
	s := screen(m)
	for _, want := range []string{"1 box could not be decoded", "● could not be decoded", "Error: "} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	press(m, "g", "e")
	if got := selectedPath(m); got != "moov/trak/mdia/minf/stbl/stsz" {
		t.Errorf("e selected %s", got)
	}
}

func TestNavigation(t *testing.T) {
	m := newModel(t, "init.mp4", nil, Options{})
	steps := []struct {
		keys []string
		want string
	}{
		{nil, "ftyp"},
		{[]string{"j", "j"}, "moov"},
		{[]string{"down"}, "moov/mvhd"},
		{[]string{"j", "right"}, "moov/trak/tkhd"}, // trak starts expanded
		{[]string{"left", "left"}, "moov/trak"},    // to the parent, then close it
		{[]string{"j"}, "moov/mvex"},
		{[]string{"k", "right", "j", "j", "right"}, "moov/trak/mdia"},
		{[]string{"right", "j", "j", "right", "right"}, "moov/trak/mdia/minf/vmhd"},
		{[]string{"G"}, "moov/mvex/trex"},
		{[]string{"-", "g", "G"}, "moov"},
		{[]string{"+", "G"}, "moov/mvex/trex"},
	}
	for _, s := range steps {
		press(m, s.keys...)
		if got := selectedPath(m); got != s.want {
			t.Fatalf("after %v: selected %s, want %s", s.keys, got, s.want)
		}
	}
}

func TestSearch(t *testing.T) {
	m := newModel(t, "1.m4s", nil, Options{})
	press(m, "/")
	typeText(m, "type:tfhd,trun")
	press(m, "enter")
	if got := selectedPath(m); got != "moof/traf/tfhd" {
		t.Fatalf("selected %s", got)
	}
	if s := screen(m); !strings.Contains(s, "/type:tfhd,trun  1 of 2") {
		t.Errorf("no match count:\n%s", s)
	}
	press(m, "n")
	if got := selectedPath(m); got != "moof/traf/trun" {
		t.Errorf("n selected %s", got)
	}
	press(m, "n")
	if got := selectedPath(m); got != "moof/traf/tfhd" {
		t.Errorf("n did not wrap: %s", got)
	}
	press(m, "N")
	if got := selectedPath(m); got != "moof/traf/trun" {
		t.Errorf("N selected %s", got)
	}
	press(m, "f")
	if len(m.rows) != 4 { // moof, traf, tfhd, trun
		t.Errorf("filter shows %d rows", len(m.rows))
	}
	press(m, "esc")
	if m.result != nil || m.onlyMatch || len(m.rows) != 8 {
		t.Errorf("esc did not clear the search: %d rows", len(m.rows))
	}
	press(m, "/")
	typeText(m, "is:big")
	press(m, "enter")
	if s := screen(m); !strings.Contains(s, "Search: unknown is:big") {
		t.Errorf("no error for bad query:\n%s", s)
	}
	press(m, "/")
	typeText(m, "type:nope")
	press(m, "enter")
	if s := screen(m); !strings.Contains(s, `No boxes match "type:nope"`) {
		t.Errorf("no message for no matches:\n%s", s)
	}
}

func TestHexHits(t *testing.T) {
	m := newModel(t, "init.mp4", nil, Options{Query: "hex:69736f35"})
	if got := selectedPath(m); got != "ftyp" {
		t.Fatalf("selected %s", got)
	}
	mask := m.hitMask(0, 32)
	for i, hit := range mask {
		want := (i >= 8 && i < 12) || (i >= 20 && i < 24)
		if hit != want {
			t.Errorf("byte %d: hit=%t", i, hit)
		}
	}
}

func TestAllFields(t *testing.T) {
	m := newModel(t, "1.m4s", nil, Options{Query: "type:trun"})
	if strings.Contains(screen(m), "sample[1]") {
		t.Fatal("samples shown at the main level")
	}
	press(m, "a")
	if got := selectedPath(m); got != "moof/traf/trun" {
		t.Errorf("selected %s after a", got)
	}
	s := screen(m)
	if !strings.Contains(s, "sample[1]") || !strings.Contains(s, "fields: all:1") {
		t.Errorf("no samples after a:\n%s", s)
	}
	if m.result == nil || len(m.result.Nodes) != 1 || m.result.Nodes[0] != m.selected() {
		t.Error("the search was not run on the new tree")
	}
}

func TestMouse(t *testing.T) {
	m := newModel(t, "init.mp4", nil, Options{})
	m.Update(tea.MouseClickMsg{X: 10, Y: 4, Button: tea.MouseLeft}) // third row: moov
	if got := selectedPath(m); got != "moov" {
		t.Fatalf("click selected %s", got)
	}
	m.Update(tea.MouseClickMsg{X: 1, Y: 4, Button: tea.MouseLeft}) // twisty of moov
	if m.expanded[m.selected()] || len(m.rows) != 3 {
		t.Errorf("click on the twisty did not close moov: %d rows", len(m.rows))
	}
	m.Update(tea.MouseClickMsg{X: 80, Y: 10, Button: tea.MouseLeft})
	if m.focus != detailPane {
		t.Error("click on the box pane did not focus it")
	}
}

func TestHelpAndQuit(t *testing.T) {
	m := newModel(t, "init.mp4", nil, Options{})
	press(m, "?")
	if s := screen(m); !strings.Contains(s, "next and previous matching box") {
		t.Errorf("no help:\n%s", s)
	}
	press(m, "esc")
	if m.showHelp {
		t.Error("esc did not close the help")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q does not quit")
	}
}

func TestNoMatchesAndEmptyFile(t *testing.T) {
	m := newModel(t, "init.mp4", corruptStsz, Options{Query: "type:nope"})
	press(m, "f", "e", "n", "j")
	if m.onlyMatch || selectedPath(m) != "moov/trak/mdia/minf/stbl/stco" {
		t.Errorf("onlyMatch=%t selected=%s", m.onlyMatch, selectedPath(m))
	}

	m = New(bytes.NewReader(nil), 0, Options{Name: "empty.mp4"})
	m.Update(tea.WindowSizeMsg{Width: testW, Height: testH})
	press(m, "j", "e", "/", "x", "enter", "f", "a", "m", "+")
	if s := screen(m); !strings.Contains(s, "The file has no boxes.") {
		t.Errorf("empty file:\n%s", s)
	}
}

func TestLayoutAndQuickTime(t *testing.T) {
	m := newModel(t, "qt_h264_aac.mov", nil, Options{Query: "type:mp4a"})
	s := screen(m) + ansi.Strip(m.detail.GetContent())
	for _, want := range []string{"QuickTime file", "QuickTime: QuickTime sound description version 1",
		"Layout  16 fields as mp4ff reads them", "samples per packet   1024  QT", "samplerate           3145728000 (48000)",
		"QT SamplesPerPacket: QuickTime sound description field; not in ISOBMFF", "compression_id        -2  QT"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	m = newModel(t, "init.mp4", nil, Options{Query: "type:SPS"})
	s = screen(m)
	for _, want := range []string{"SPS  Sequence Parameter Set, in avcC", "✓ decoded part of avcC", "Width"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "QuickTime") || strings.Contains(s, "Layout") {
		t.Errorf("ISOBMFF part shows QuickTime or layout:\n%s", s)
	}
}

func TestSummaryView(t *testing.T) {
	m := newModel(t, "chapters_subs.m4a", nil, Options{})
	if s := screen(m); !strings.Contains(s, "Track 1: audio") || !strings.Contains(s, "mp4a.40.2") {
		t.Fatalf("no summary at the start:\n%s", s)
	}
	press(m, "down")
	if s := screen(m); strings.Contains(s, "Track 1: audio") {
		t.Errorf("summary still shown after a move:\n%s", s)
	}
	press(m, "s")
	if s := screen(m); !strings.Contains(s, "Chapters (chpl)") {
		t.Errorf("no summary after s:\n%s", s)
	}
	press(m, "esc")
	if s := screen(m); strings.Contains(s, "Chapters (chpl)") {
		t.Errorf("summary still shown after esc:\n%s", s)
	}
}

// lineOf returns the first line of the box pane that shows a field or layout line of kind for field i, or -1.
func lineOf(m *Model, kind lineKind, i int) int {
	for k, r := range m.lines {
		if r.kind == kind && r.index == i {
			return k
		}
	}
	return -1
}

// hexCell returns the line of the hex dump with the byte at offset off of the selected box, and the column of its
// first hex digit.
func hexCell(m *Model, off int) (int, int) {
	for k, r := range m.lines {
		if r.kind == lineHex && off >= r.index && off < r.index+r.count {
			j := off - r.index
			col := 9 + 3*j + 1
			if j >= 8 {
				col++
			}
			return k, col
		}
	}
	return -1, 0
}

// cellOf scrolls the box pane to show column col of line k, and returns the screen cell of it.
func cellOf(m *Model, k, col int) (int, int) {
	w := m.detail.Width()
	m.detail.SetYOffset(m.lineRows[k] + col/w)
	return m.treeW + 1 + col%w, 2 + m.lineRows[k] + col/w - m.detail.YOffset()
}

// footer returns the last line of the screen.
func footer(m *Model) string {
	lines := strings.Split(screen(m), "\n")
	return lines[len(lines)-1]
}

func TestMarkField(t *testing.T) {
	m := newModel(t, "init.mp4", nil, Options{Query: "type:tkhd"})
	press(m, "tab", "j")
	if f := footer(m); m.mark != 0 || !strings.Contains(f, "field 1 of 25 · size · u32 · 4 bytes at 0xc1 (+0) · 92") {
		t.Errorf("mark %d, footer %q", m.mark, f)
	}
	press(m, "j", "j", "down", "j")
	content := m.detail.GetContent()
	for _, want := range []string{styleMarked.Render("d0 eb cc 19"), styleMarked.Render("creation time")} {
		if !strings.Contains(content, want) {
			t.Errorf("content lacks %q", want)
		}
	}
	if f := footer(m); !strings.Contains(f, "field 5 of 25 · creation_time · u32 · 4 bytes at 0xcd (+12)") {
		t.Errorf("footer %q", f)
	}
	press(m, "k")
	if !strings.Contains(m.detail.GetContent(), styleMarked.Render("00 00 07")) {
		t.Error("the bytes of the flags are not marked")
	}
	press(m, "esc")
	if f := footer(m); m.mark != -1 || m.result == nil || !strings.Contains(f, "/type:tkhd  1 of 1") {
		t.Errorf("esc: mark %d, footer %q", m.mark, f)
	}
	press(m, "G")
	if m.mark != 24 {
		t.Errorf("G marked %d, not the last field", m.mark)
	}
	press(m, "g")
	if m.mark != 0 {
		t.Errorf("g marked %d, not the first field", m.mark)
	}
	press(m, "tab", "j")
	if got := selectedPath(m); m.mark != -1 || got != "moov/trak/mdia" || strings.Contains(footer(m), "field 1 of") {
		t.Errorf("j in the tree selected %s, and the mark %d stayed", got, m.mark)
	}

	// A container has no layout fields, so j scrolls.
	m = newModel(t, "init.mp4", nil, Options{Query: "type:moov"})
	press(m, "tab", "j")
	if m.mark != -1 || m.detail.YOffset() != 1 {
		t.Errorf("container: mark %d, scrolled to %d", m.mark, m.detail.YOffset())
	}
}

func TestPointAtField(t *testing.T) {
	m := newModel(t, "init.mp4", nil, Options{Query: "type:tkhd"})
	k, col := hexCell(m, 13) // in creation_time
	x, y := cellOf(m, k, col)
	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	if f := footer(m); m.hover != 4 || !strings.Contains(f, "0xce · 206 · creation_time · u32 · 3505114137") {
		t.Errorf("byte: hover %d, footer %q", m.hover, f)
	}
	if !strings.Contains(m.detail.GetContent(), styleMarked.Render("d0 eb cc 19")) {
		t.Error("the bytes of the field are not marked")
	}
	x, y = cellOf(m, k, 9+49+2+13) // the same byte in the text
	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	if m.hover != 4 {
		t.Errorf("text of the byte: hover %d", m.hover)
	}
	x, y = cellOf(m, lineOf(m, lineLayout, 6), 20)
	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	if f := footer(m); m.hover != 6 || !strings.Contains(f, "track_ID · u32 · 4 bytes at 0xd5 (+20) · 2") {
		t.Errorf("layout row: hover %d, footer %q", m.hover, f)
	}
	k = lineOf(m, lineField, 6)
	if k < 0 {
		t.Fatal("no field linked to track_ID")
	}
	x, y = cellOf(m, k, 2)
	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	markOn, _, _ := strings.Cut(styleMarked.Render("x"), "x")
	if row := strings.Split(m.detail.GetContent(), "\n")[lineOf(m, lineLayout, 6)]; m.hover != 6 ||
		!strings.HasPrefix(row, markOn+"000000d5") {
		t.Errorf("field entry: hover %d, layout row %q", m.hover, row)
	}
	m.Update(tea.MouseMotionMsg{X: 5, Y: 5}) // the tree
	if m.hover != -1 || m.hoverText != "" {
		t.Errorf("the tree: hover %d, text %q", m.hover, m.hoverText)
	}

	// The mouse does not remove a mark of the keys when it leaves the fields.
	press(m, "tab", "j")
	k, col = hexCell(m, 13)
	x, y = cellOf(m, k, col)
	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	m.Update(tea.MouseMotionMsg{X: 5, Y: 5})
	if f := footer(m); m.mark != 0 || !strings.Contains(f, "field 1 of 25") {
		t.Errorf("mark %d, footer %q", m.mark, f)
	}

	m = newModel(t, "init.mp4", nil, Options{Query: "type:trak"})
	k, col = hexCell(m, 9) // in the header of tkhd
	x, y = cellOf(m, k, col)
	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	if f := footer(m); !strings.Contains(f, "0xc2 · 194 · tkhd +1 (click to open)") {
		t.Errorf("child box byte: footer %q", f)
	}
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if got := selectedPath(m); got != "moov/trak/tkhd" {
		t.Errorf("click on a byte of tkhd selected %s", got)
	}
}

// TestPointerCells checks that each cell of the hex dump that the mouse can point at shows the byte that it
// points at, also when the rows wrap.
func TestPointerCells(t *testing.T) {
	data, err := os.ReadFile("../../testdata/init.mp4")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{testW, testH}, {60, 15}} {
		m := newModel(t, "init.mp4", nil, Options{Query: "type:tkhd"})
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		n := m.selected()
		seen := map[int]bool{}
		for off := 0; ; off++ {
			m.detail.SetYOffset(off)
			if m.detail.YOffset() != off {
				break
			}
			rows := strings.Split(screen(m), "\n")
			for y := 2; y < 2+m.detail.Height(); y++ {
				row := []rune(rows[y])
				for x := m.treeW + 1; x < m.treeW+1+m.detail.Width(); x++ {
					ref, col, ok := m.lineAt(x, y)
					if !ok || ref.kind != lineHex {
						continue
					}
					i, ok := hexByteAt(ref, col)
					if !ok {
						continue
					}
					b := data[n.Pos+uint64(i)]
					hx, text := fmt.Sprintf("%02x", b), '.'
					if b >= 0x20 && b < 0x7f {
						text = rune(b)
					}
					if ch := row[x]; ch != ' ' && ch != rune(hx[0]) && ch != rune(hx[1]) && ch != text {
						t.Fatalf("%v: cell %d,%d shows %q, not byte %d: %s", size, x, y, ch, i, hx)
					}
					seen[i] = true
				}
			}
		}
		if len(seen) != int(n.Size) {
			t.Errorf("%v: the mouse finds %d of %d bytes", size, len(seen), n.Size)
		}
	}
}

func TestDetailPathOrderNumbers(t *testing.T) {
	m := newModel(t, "prog_8s.mp4", nil, Options{Query: "type:stsd"})
	press(m, "n")
	if s, want := screen(m), "moov › trak[1] › mdia[1] › minf[1] › stbl[1] › stsd[1]"; !strings.Contains(s, want) {
		t.Errorf("screen lacks %q:\n%s", want, s)
	}
}
