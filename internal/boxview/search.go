package boxview

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// QueryHelp describes the search queries of ParseQuery.
const QueryHelp = `A box matches a query when it matches all its terms. A term is one of:

  text           text in the box type, its name or its fields, ignoring case
  type:trun,tfdt the box type is one of these
  path:trak/mdia the box path, such as moov/trak/mdia/mdhd, contains this
  key=value      the box has a field with this key and value, such as trackID=2
  @1234 @0x4d2   the innermost box that holds the byte at this position
  size>1000      the box size compares so with the number; also <, <=, >= and =
  hex:000001b6   the innermost boxes that hold these bytes
  is:error       the box could not be decoded
  is:unknown     mp4ff does not know the box type
  is:truncated   the box continues past the end of the file
  is:quicktime   the box, or one of its fields, is specific to QuickTime
  is:part        a structure inside a box, such as an SPS or a descriptor

Put "-" before a term to match the boxes that do not match it, and quotes around text with spaces.`

// MaxHits is the largest number of positions that a hex term finds.
const MaxHits = 10000

// Query is a parsed search query.
type Query struct {
	terms []*term
}

// term is a term of a query. A hex term has pattern set and no match function.
type term struct {
	neg     bool
	match   func(t *Tree, n *Node) bool
	pattern []byte
}

// Span is a range of bytes in the file.
type Span struct {
	Pos uint64 `json:"pos"`
	Len int    `json:"len"`
}

// Result is the result of a search.
type Result struct {
	// Nodes are the matching boxes, in file order.
	Nodes []*Node
	// Hits are the positions of the bytes that hex terms found, for each matching box that holds some.
	Hits map[*Node][]Span
	// Capped is true if a hex term stopped after finding MaxHits positions.
	Capped bool
}

// Empty tells if the query has no terms.
func (q *Query) Empty() bool {
	return len(q.terms) == 0
}

var sizeTerm = regexp.MustCompile(`^size(<=|>=|<|>|=)(\S+)$`)

// ParseQuery parses a search query, as described by QueryHelp.
func ParseQuery(s string) (*Query, error) {
	q := &Query{}
	for _, tok := range tokenize(s) {
		if tok == "" {
			continue
		}
		t := &term{}
		if len(tok) > 1 && tok[0] == '-' {
			t.neg, tok = true, tok[1:]
		}
		if err := t.parse(tok); err != nil {
			return nil, err
		}
		if t.neg && t.pattern != nil {
			return nil, fmt.Errorf("a hex term cannot be negated")
		}
		q.terms = append(q.terms, t)
	}
	return q, nil
}

// tokenize splits s at spaces outside double quotes, and removes the quotes.
func tokenize(s string) []string {
	var toks []string
	var sb strings.Builder
	quoted, inTok := false, false
	for _, r := range s {
		switch {
		case r == '"':
			quoted, inTok = !quoted, true
		case !quoted && (r == ' ' || r == '\t' || r == '\n'):
			if inTok {
				toks = append(toks, sb.String())
				sb.Reset()
				inTok = false
			}
		default:
			sb.WriteRune(r)
			inTok = true
		}
	}
	if inTok {
		toks = append(toks, sb.String())
	}
	return toks
}

func (t *term) parse(tok string) error {
	if strings.HasPrefix(tok, "@") {
		return t.parsePos(tok[1:])
	}
	if m := sizeTerm.FindStringSubmatch(tok); m != nil {
		return t.parseSize(m[1], m[2])
	}
	if prefix, v, ok := strings.Cut(tok, ":"); ok {
		switch prefix {
		case "type":
			types := strings.Split(v, ",")
			t.match = func(_ *Tree, n *Node) bool {
				for _, typ := range types {
					if n.Name() == typ {
						return true
					}
				}
				return false
			}
			return nil
		case "path":
			t.match = func(_ *Tree, n *Node) bool { return strings.Contains(n.Path(), v) }
			return nil
		case "pos":
			return t.parsePos(v)
		case "hex":
			return t.parseHex(v)
		case "is":
			return t.parseIs(v)
		}
	}
	if key, v, ok := strings.Cut(tok, "="); ok && key != "" {
		t.match = func(_ *Tree, n *Node) bool { return hasField(n, key, v) }
		return nil
	}
	text := strings.ToLower(tok)
	t.match = func(_ *Tree, n *Node) bool { return hasText(n, text) }
	return nil
}

func (t *term) parsePos(v string) error {
	pos, err := strconv.ParseUint(v, 0, 64)
	if err != nil {
		return fmt.Errorf("bad position %q", v)
	}
	t.match = func(tr *Tree, n *Node) bool { return n == tr.At(pos) }
	return nil
}

func (t *term) parseSize(op, v string) error {
	size, err := strconv.ParseUint(v, 0, 64)
	if err != nil {
		return fmt.Errorf("bad size %q", v)
	}
	cmp := map[string]func(a, b uint64) bool{
		"<":  func(a, b uint64) bool { return a < b },
		"<=": func(a, b uint64) bool { return a <= b },
		">":  func(a, b uint64) bool { return a > b },
		">=": func(a, b uint64) bool { return a >= b },
		"=":  func(a, b uint64) bool { return a == b },
	}[op]
	t.match = func(_ *Tree, n *Node) bool { return (n.HdrLen > 0 || n.Part != nil) && cmp(n.Size, size) }
	return nil
}

func (t *term) parseHex(v string) error {
	s := strings.NewReplacer(" ", "", "0x", "", "0X", "").Replace(v)
	p, err := hex.DecodeString(s)
	if err != nil || len(p) == 0 {
		return fmt.Errorf("bad hex bytes %q", v)
	}
	t.pattern = p
	return nil
}

func (t *term) parseIs(v string) error {
	switch v {
	case "error":
		t.match = func(_ *Tree, n *Node) bool { return n.Err != nil }
	case "unknown":
		t.match = func(_ *Tree, n *Node) bool { return n.Unknown() }
	case "truncated":
		t.match = func(tr *Tree, n *Node) bool { return n.Truncated(tr.Size) }
	case "quicktime":
		t.match = func(_ *Tree, n *Node) bool { return hasQuickTime(n) }
	case "part":
		t.match = func(_ *Tree, n *Node) bool { return n.Part != nil }
	default:
		return fmt.Errorf("unknown is:%s, use is:error, is:unknown, is:truncated, is:quicktime or is:part", v)
	}
	return nil
}

// hasField tells if n has a field with key, ignoring case, whose value is v, or starts with v and a space,
// as "65536  (1.0)" does for v "65536".
func hasField(n *Node, key, v string) bool {
	for _, f := range n.Fields() {
		if !strings.EqualFold(f.Key, key) {
			continue
		}
		if strings.EqualFold(f.Value, v) {
			return true
		}
		if len(f.Value) > len(v) && f.Value[len(v)] == ' ' && strings.EqualFold(f.Value[:len(v)], v) {
			return true
		}
	}
	return false
}

// hasText tells if the box type, long name or fields of n contain text, which is in lower case.
// The first Info line, with the size, version and flags, is left out.
func hasText(n *Node, text string) bool {
	if strings.Contains(strings.ToLower(n.Name()), text) || strings.Contains(strings.ToLower(n.LongName()), text) {
		return true
	}
	for _, line := range n.Info[min(1, len(n.Info)):] {
		if strings.Contains(strings.ToLower(line), text) {
			return true
		}
	}
	return false
}

// Search returns the boxes that match q. r holds the bytes of the file, which hex terms search.
func (t *Tree) Search(q *Query, r io.ReaderAt) (*Result, error) {
	res := &Result{}
	var hexHits []map[*Node][]Span
	for _, tm := range q.terms {
		if tm.pattern == nil {
			continue
		}
		hits, capped, err := t.findBytes(r, tm.pattern)
		if err != nil {
			return nil, err
		}
		res.Capped = res.Capped || capped
		hexHits = append(hexHits, hits)
	}
	for _, n := range t.Nodes {
		if q.matches(t, n, hexHits) {
			res.Nodes = append(res.Nodes, n)
		}
	}
	if len(hexHits) > 0 {
		res.Hits = make(map[*Node][]Span)
		for _, n := range res.Nodes {
			for _, hits := range hexHits {
				res.Hits[n] = append(res.Hits[n], hits[n]...)
			}
		}
	}
	return res, nil
}

func (q *Query) matches(t *Tree, n *Node, hexHits []map[*Node][]Span) bool {
	if q.Empty() {
		return false
	}
	for _, hits := range hexHits {
		if len(hits[n]) == 0 {
			return false
		}
	}
	for _, tm := range q.terms {
		if tm.match != nil && tm.match(t, n) == tm.neg {
			return false
		}
	}
	return true
}

// chunkSize is the number of bytes that findBytes reads at a time.
const chunkSize = 1 << 20

// findBytes finds pattern in the file and returns the positions where it starts, for the innermost box that
// holds each. It stops after MaxHits positions, and then returns true.
func (t *Tree) findBytes(r io.ReaderAt, pattern []byte) (map[*Node][]Span, bool, error) {
	hits := make(map[*Node][]Span)
	if r == nil {
		return nil, false, errors.New("no file to search for bytes")
	}
	found := 0
	buf := make([]byte, chunkSize+len(pattern)-1)
	for off := int64(0); off < t.Size; off += chunkSize {
		n, err := r.ReadAt(buf[:min(int64(len(buf)), t.Size-off)], off)
		if err != nil && (!errors.Is(err, io.EOF) || n == 0) {
			return nil, false, fmt.Errorf("read at %d: %w", off, err)
		}
		data := buf[:n]
		for i := 0; ; {
			j := bytes.Index(data[i:], pattern)
			if j < 0 || i+j >= chunkSize {
				break
			}
			pos := uint64(off) + uint64(i+j)
			if box := t.At(pos); box != nil {
				hits[box] = append(hits[box], Span{Pos: pos, Len: len(pattern)})
			}
			found++
			if found == MaxHits {
				return hits, true, nil
			}
			i += j + 1
		}
	}
	return hits, false, nil
}
