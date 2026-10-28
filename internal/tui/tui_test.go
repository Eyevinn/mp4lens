package tui

import (
	"bytes"
	"encoding/binary"
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
