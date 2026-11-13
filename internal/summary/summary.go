// Package summary gives an overview of a file, as ffprobe and MP4Box -info do: the brands and structure of the
// file, the movie, the metadata items, the chapters, and for each track its codec, duration, samples and bitrate.
package summary

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/Eyevinn/mp4lens/internal/boxview"
	"github.com/Eyevinn/mp4lens/internal/layout"
)

// Summary is an overview of a file, in sections of named values.
type Summary struct {
	Sections []Section `json:"sections"`
}

// Section is a part of a summary, such as the movie or a track.
type Section struct {
	Title string `json:"title"`
	// Key is the key of the box that the section is about, such as a trak box, or empty.
	Key  string `json:"key,omitempty"`
	Rows []Row  `json:"rows"`
}

// Row is a named value of a section.
type Row struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// Key is the key of the box that the value comes from, or empty.
	Key string `json:"key,omitempty"`
	// Image is where the image is in the file, for a value that is an image, such as cover art, or nil.
	Image *Image `json:"image,omitempty"`
}

// Image is an image in the file.
type Image struct {
	// Pos is the position of the image in the file, and Size its number of bytes.
	Pos  uint64 `json:"pos"`
	Size uint64 `json:"size"`
	// Type is the media type of the image, such as image/jpeg.
	Type string `json:"type"`
}

// imageTypes are the media types of the images of the well-known types of a data box.
var imageTypes = map[int]string{13: "image/jpeg", 14: "image/png", 27: "image/bmp"}

// imageEntries are the media types of the samples of sample entries whose samples are images, as those of a
// chapter track with chapter images.
var imageEntries = map[string]string{"jpeg": "image/jpeg", "mjpg": "image/jpeg", "png ": "image/png"}

// maxValue is the number of characters of a value that a summary keeps.
const maxValue = 300

// add adds a row with the value from the box of n, which may be nil. White space in the value, such as line
// breaks, becomes single spaces, and a long value is cut. An empty value is left out.
func (s *Section) add(name, value string, n *boxview.Node) {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return
	}
	if runes := []rune(value); len(runes) > maxValue {
		value = string(runes[:maxValue]) + "…"
	}
	r := Row{Name: name, Value: value}
	if n != nil {
		r.Key = n.Key
	}
	s.Rows = append(s.Rows, r)
}

// maxChapters is the largest number of chapters that a summary lists from a chapter track.
const maxChapters = 1000

// maxChapterSample is the largest number of bytes of a chapter sample that a summary reads.
const maxChapterSample = 4096

// Of returns the summary of the file of t, whose bytes are in r.
func Of(t *boxview.Tree, r io.ReaderAt) *Summary {
	s := &Summary{Sections: []Section{fileSection(t)}}
	moov := topBox(t, "moov")
	frags := fragmentStats(t, moov)
	if moov == nil {
		for _, id := range sortedIDs(frags) {
			s.Sections = append(s.Sections, fragmentTrackSection(id, frags[id]))
		}
		return s
	}
	if sec, ok := movieSection(moov); ok {
		s.Sections = append(s.Sections, sec)
	}
	if sec := metadataSection(moov); len(sec.Rows) > 0 {
		s.Sections = append(s.Sections, sec)
	}
	if sec := imagesSection(t, r); len(sec.Rows) > 0 {
		s.Sections = append(s.Sections, sec)
	}
	if chpl := child(child(moov, "udta"), "chpl"); chpl != nil {
		if sec := chplSection(chpl); len(sec.Rows) > 0 {
			s.Sections = append(s.Sections, sec)
		}
	}
	traks := children(moov, "trak")
	chapterOf := chapterTracks(traks)
	for _, trak := range traks {
		s.Sections = append(s.Sections, trackSection(trak, moov, chapterOf, frags))
	}
	for _, trak := range traks {
		tkhd := boxOf[*mp4.TkhdBox](child(trak, "tkhd"))
		if tkhd == nil || len(chapterOf[tkhd.TrackID]) == 0 {
			continue
		}
		sec := Section{Title: fmt.Sprintf("Chapters (track %d)", tkhd.TrackID), Key: trak.Key}
		if isTextTrack(trak) {
			sec.Rows = trackChapters(trak, r)
		} else {
			sec.Title = fmt.Sprintf("Chapter images (track %d)", tkhd.TrackID)
			sec.Rows = chapterImages(trak)
		}
		if len(sec.Rows) > 0 {
			s.Sections = append(s.Sections, sec)
		}
	}
	return s
}

// Text returns the summary as lines of text.
func (s *Summary) Text() string {
	var b strings.Builder
	for i, sec := range s.Sections {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(sec.Title + "\n")
		for _, r := range sec.Rows {
			fmt.Fprintf(&b, "  %s: %s\n", r.Name, r.Value)
		}
	}
	return b.String()
}

// fileSection returns the brands and the structure of the file.
func fileSection(t *boxview.Tree) Section {
	s := Section{Title: "File"}
	s.add("size", fmt.Sprintf("%d bytes", t.Size), nil)
	for _, typ := range []string{"ftyp", "styp"} {
		n := topBox(t, typ)
		if n == nil {
			continue
		}
		var b interface {
			MajorBrand() string
			MinorVersion() uint32
			CompatibleBrands() []string
		}
		switch x := n.Box.(type) {
		case *mp4.FtypBox:
			b = x
		case *mp4.StypBox:
			b = x
		default:
			continue
		}
		var compatible []string
		for _, c := range b.CompatibleBrands() {
			compatible = append(compatible, brandText(c))
		}
		s.add("brands", fmt.Sprintf("major %s, minor version %d, compatible %s", brandText(b.MajorBrand()),
			b.MinorVersion(), strings.Join(compatible, ", ")), n)
	}
	if t.QuickTime != "" {
		s.add("QuickTime", t.QuickTime, nil)
	}
	s.add("structure", structure(t), nil)
	if errs := t.Errors(); len(errs) > 0 {
		what := fmt.Sprintf("%d boxes or parts", len(errs))
		if len(errs) == 1 {
			what = "1 box or part"
		}
		s.add("errors", fmt.Sprintf("%s could not be decoded, the first one %s", what, errs[0].Path()), errs[0])
	}
	return s
}

// structure says how the boxes of the file of t are arranged, such as moov before mdat.
func structure(t *boxview.Tree) string {
	moov, mdat, moofs, sidx := -1, -1, 0, 0
	for i, n := range t.Boxes {
		switch n.Type {
		case "moov":
			if moov < 0 {
				moov = i
			}
		case "mdat":
			if mdat < 0 {
				mdat = i
			}
		case "moof":
			moofs++
		case "sidx":
			sidx++
		}
	}
	index := ""
	if sidx > 0 {
		index = ", " + plural(sidx, "sidx box")
	}
	switch {
	case moov >= 0 && moofs > 0:
		return fmt.Sprintf("fragmented: moov and %s%s", plural(moofs, "fragment"), index)
	case moofs > 0:
		return fmt.Sprintf("media segment: %s without moov%s", plural(moofs, "fragment"), index)
	case moov >= 0 && child(t.Boxes[moov], "mvex") != nil:
		return "init segment of a fragmented file, with no fragments"
	case moov >= 0 && mdat < 0:
		return "moov without media data"
	case moov >= 0 && moov < mdat:
		return "progressive, moov before mdat"
	case moov >= 0:
		return "moov after mdat"
	}
	return "no moov"
}

// movieSection returns the duration and times of the movie, from mvhd and mehd.
func movieSection(moov *boxview.Node) (Section, bool) {
	n := child(moov, "mvhd")
	mvhd := boxOf[*mp4.MvhdBox](n)
	if mvhd == nil {
		return Section{}, false
	}
	s := Section{Title: "Movie", Key: moov.Key}
	s.add("duration", ticksText(mvhd.Duration, mvhd.Timescale, mvhd.Version == 0), n)
	if mehdNode := child(child(moov, "mvex"), "mehd"); mehdNode != nil {
		if mehd := boxOf[*mp4.MehdBox](mehdNode); mehd != nil {
			s.add("fragment duration", ticksText(uint64(mehd.FragmentDuration), mvhd.Timescale, false), mehdNode)
		}
	}
	if mvhd.CreationTime != 0 {
		s.add("created", time.Unix(mvhd.CreationTimeS(), 0).UTC().Format(time.RFC3339), n)
	}
	if mvhd.ModificationTime != 0 && mvhd.ModificationTime != mvhd.CreationTime {
		s.add("modified", time.Unix(mvhd.ModificationTimeS(), 0).UTC().Format(time.RFC3339), n)
	}
	return s, true
}

// metadataSection returns the metadata items of the movie: the items of the ilst boxes in moov/udta/meta and
// moov/meta, with the names of their keys for QuickTime metadata, and the user data text atoms in moov/udta.
func metadataSection(moov *boxview.Node) Section {
	s := Section{Title: "Metadata"}
	udta := child(moov, "udta")
	for _, meta := range []*boxview.Node{child(udta, "meta"), child(moov, "meta")} {
		keys := boxOf[*mp4.KeysBox](child(meta, "keys"))
		for _, item := range kids(child(meta, "ilst")) {
			addItem(&s, item, keys)
		}
	}
	addUserData(&s, udta)
	return s
}

// addItem adds the values of a metadata item in ilst to s.
func addItem(s *Section, item *boxview.Node, keys *mp4.KeysBox) {
	name := item.LongName()
	if name == "" {
		name = item.Name()
	}
	if keys != nil {
		if k, ok := keys.KeyOfItem(item.Type); ok {
			name = k.Value
		}
	}
	freeform, domain := "", ""
	var values []string
	var images []Row
	for _, c := range item.Children {
		switch b := c.Box.(type) {
		case *mp4.MeanBox:
			domain = b.Domain
		case *mp4.NameBox:
			freeform = b.Name
		case *mp4.DataBox:
			v := dataValue(item.Type, freeform, b)
			if mime := imageTypes[int(b.DataType&0xffffff)]; mime != "" {
				// The value of a data box comes after its header, type indicator and locale.
				images = append(images, Row{Value: v, Key: c.Key,
					Image: &Image{Pos: c.Pos + uint64(c.HdrLen) + 8, Size: uint64(len(b.Data)), Type: mime}})
				continue
			}
			values = append(values, v)
		}
	}
	if freeform != "" {
		name = freeform
		if domain != "" {
			name += " (" + domain + ")"
		}
	}
	s.add(name, strings.Join(values, "; "), item)
	for i, img := range images {
		img.Name = name
		if len(images) > 1 {
			img.Name = fmt.Sprintf("%s %d", name, i+1)
		}
		s.Rows = append(s.Rows, img)
	}
}

// dataValue returns the value of a data box of a metadata item of type item, or of name freeform for a freeform
// item, as text.
func dataValue(item, freeform string, b *mp4.DataBox) string {
	dataType := int(b.DataType & 0xffffff)
	switch dataType {
	case 13, 14, 27:
		kind := map[int]string{13: "JPEG", 14: "PNG", 27: "BMP"}[dataType]
		return fmt.Sprintf("%s image, %d bytes", kind, len(b.Data))
	}
	if v := layout.ItemValue(item, freeform, dataType, b.Data); v != "" {
		return v
	}
	if len(b.Data) <= 16 {
		return fmt.Sprintf("%x", b.Data)
	}
	return fmt.Sprintf("%d bytes", len(b.Data))
}

// addUserData adds the QuickTime user data text atoms and name atoms in udta to s.
func addUserData(s *Section, udta *boxview.Node) {
	for _, c := range kids(udta) {
		switch b := c.Box.(type) {
		case *mp4.UserDataTextBox:
			name := c.LongName()
			if name == "" {
				name = c.Name()
			}
			var texts []string
			for _, t := range b.Texts {
				texts = append(texts, t.Text)
			}
			s.add(name, strings.Join(texts, "; "), c)
		case *mp4.NameBox:
			s.add("name", b.Name, c)
		}
	}
}

// imageScan is the number of bytes at the start of a payload in which an image signature is looked for, to also
// find an image after a version and flags or a child box header.
const imageScan = 16

// maxImage is the largest box in which an image is looked for.
const maxImage = 32 << 20

// imageSignatures are the signatures of the images that imagesSection finds.
var imageSignatures = []struct {
	sig  []byte
	mime string
}{
	{[]byte{0xff, 0xd8, 0xff}, "image/jpeg"},
	{[]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, "image/png"},
}

// imagesSection returns the images found by their signatures at the start of the payloads of boxes without
// child boxes, such as the thumbnails that cameras write in udta. It leaves out media data, fragments, sample
// tables and the data boxes of metadata items, whose cover art the metadata section has.
func imagesSection(t *boxview.Tree, r io.ReaderAt) Section {
	s := Section{Title: "Images"}
	if r == nil {
		return s
	}
	for _, n := range t.Nodes {
		start := n.Pos + uint64(n.HdrLen)
		if !n.IsBox() || n.HdrLen == 0 || n.Size > maxImage || n.End() > uint64(t.Size) || start+imageScan >= n.End() ||
			hasChildBoxes(n) || !imageCandidate(n) {
			continue
		}
		head := make([]byte, min(n.End()-start, imageScan+8))
		if _, err := r.ReadAt(head, int64(start)); err != nil {
			continue
		}
		if off, mime := findImage(head); mime != "" {
			pos := start + uint64(off)
			s.Rows = append(s.Rows, Row{Name: n.Path(), Key: n.Key,
				Value: fmt.Sprintf("%s image, %d bytes", strings.ToUpper(strings.TrimPrefix(mime, "image/")), n.End()-pos),
				Image: &Image{Pos: pos, Size: n.End() - pos, Type: mime}})
		}
	}
	return s
}

// imageCandidate tells if an image is looked for in the box of n: not in media data, fragments, sample tables
// or metadata values.
func imageCandidate(n *boxview.Node) bool {
	if _, ok := n.Box.(*mp4.DataBox); ok {
		return false
	}
	for p := n; p != nil; p = p.Parent {
		switch p.Type {
		case "mdat", "moof", "stbl", "free", "skip", "wide":
			return false
		}
	}
	return true
}

// hasChildBoxes tells if n has child boxes.
func hasChildBoxes(n *boxview.Node) bool {
	for _, c := range n.Children {
		if c.IsBox() {
			return true
		}
	}
	return false
}

// findImage returns the offset and media type of the first image signature in the first imageScan bytes of head,
// or "" if there is none.
func findImage(head []byte) (int, string) {
	for off := 0; off <= imageScan && off < len(head); off++ {
		for _, sig := range imageSignatures {
			if bytes.HasPrefix(head[off:], sig.sig) {
				return off, sig.mime
			}
		}
	}
	return 0, ""
}

// chplSection returns the chapters of a Nero chapter list.
func chplSection(n *boxview.Node) Section {
	s := Section{Title: "Chapters (chpl)", Key: n.Key}
	if chpl := boxOf[*mp4.ChplBox](n); chpl != nil {
		for _, c := range chpl.Chapters {
			s.add(clock(c.StartTime()), c.Title, n)
		}
	}
	return s
}

// chapterTracks returns the IDs of the tracks that the chap track references of traks refer to, with the IDs of
// the tracks that refer to them.
func chapterTracks(traks []*boxview.Node) map[uint32][]uint32 {
	chapterOf := map[uint32][]uint32{}
	for _, trak := range traks {
		tkhd := boxOf[*mp4.TkhdBox](child(trak, "tkhd"))
		if tkhd == nil {
			continue
		}
		for _, c := range kids(child(trak, "tref")) {
			if ref := boxOf[*mp4.TrefTypeBox](c); ref != nil && ref.Name == "chap" {
				for _, id := range ref.TrackIDs {
					chapterOf[id] = append(chapterOf[id], tkhd.TrackID)
				}
			}
		}
	}
	return chapterOf
}

// trackSample is a sample of a track: its start time, and where it is in the file.
type trackSample struct {
	start     time.Duration
	pos, size uint64
}

// trackSamples returns the first maxChapters samples of the track of trak, from its sample table.
func trackSamples(trak *boxview.Node) []trackSample {
	mdia := child(trak, "mdia")
	mdhd := boxOf[*mp4.MdhdBox](child(mdia, "mdhd"))
	stbl := child(child(mdia, "minf"), "stbl")
	stsz := boxOf[*mp4.StszBox](child(stbl, "stsz"))
	stsc := boxOf[*mp4.StscBox](child(stbl, "stsc"))
	stts := boxOf[*mp4.SttsBox](child(stbl, "stts"))
	offset := chunkOffsets(stbl)
	if mdhd == nil || mdhd.Timescale == 0 || stsz == nil || stsc == nil || stts == nil || offset == nil {
		return nil
	}
	var out []trackSample
	for nr := 1; nr <= int(min(stsz.GetNrSamples(), maxChapters)); nr++ {
		chunk, first, err := stsc.ChunkNrFromSampleNr(nr)
		if err != nil {
			break
		}
		pos, err := offset(chunk)
		if err != nil {
			break
		}
		for i := first; i < nr; i++ {
			pos += uint64(stsz.GetSampleSize(i))
		}
		start, _, err := stts.GetDecodeTime(uint32(nr))
		if err != nil {
			break
		}
		out = append(out, trackSample{start: seconds(start, mdhd.Timescale), pos: pos,
			size: uint64(stsz.GetSampleSize(nr))})
	}
	return out
}

// trackChapters returns the start times and titles of the samples of a chapter track, read from r.
func trackChapters(trak *boxview.Node, r io.ReaderAt) []Row {
	if r == nil {
		return nil
	}
	var rows []Row
	for _, smp := range trackSamples(trak) {
		buf := make([]byte, min(smp.size, maxChapterSample))
		if _, err := r.ReadAt(buf, int64(smp.pos)); err != nil {
			break
		}
		rows = append(rows, Row{Name: clock(smp.start), Value: sampleText(buf), Key: trak.Key})
	}
	return rows
}

// chapterImages returns the start times and images of the samples of a chapter track whose samples are images,
// such as the JPEG images of the chapters of a podcast, or nil if its samples are not images.
func chapterImages(trak *boxview.Node) []Row {
	entries := kids(child(child(child(child(trak, "mdia"), "minf"), "stbl"), "stsd"))
	if len(entries) == 0 || imageEntries[entries[0].Type] == "" {
		return nil
	}
	mime := imageEntries[entries[0].Type]
	var rows []Row
	for _, smp := range trackSamples(trak) {
		rows = append(rows, Row{Name: clock(smp.start), Key: trak.Key,
			Value: fmt.Sprintf("%s image, %d bytes", strings.ToUpper(strings.TrimPrefix(mime, "image/")), smp.size),
			Image: &Image{Pos: smp.pos, Size: smp.size, Type: mime}})
	}
	return rows
}

// isTextTrack tells if the samples of the track of trak are texts, as those of a chapter track with chapter
// titles. A chapter track can also have images.
func isTextTrack(trak *boxview.Node) bool {
	mdia := child(trak, "mdia")
	if hdlr := boxOf[*mp4.HdlrBox](child(mdia, "hdlr")); hdlr != nil {
		switch hdlr.HandlerType {
		case "text", "sbtl", "subt":
			return true
		}
	}
	return false
}

// chunkOffsets returns a function that returns the file offset of a chunk of the stco or co64 box of stbl, or nil.
func chunkOffsets(stbl *boxview.Node) func(chunk int) (uint64, error) {
	if stco := boxOf[*mp4.StcoBox](child(stbl, "stco")); stco != nil {
		return stco.GetOffset
	}
	if co64 := boxOf[*mp4.Co64Box](child(stbl, "co64")); co64 != nil {
		return co64.GetOffset
	}
	return nil
}

// sampleText returns the text of a sample of a text track: a 16-bit length and the text, in UTF-8, or in UTF-16
// after a byte order mark. Line breaks and other white space become single spaces.
func sampleText(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	text := b[2:min(len(b), 2+int(binary.BigEndian.Uint16(b)))]
	s := string(text)
	if len(text) >= 2 && text[0] == 0xfe && text[1] == 0xff {
		u := make([]uint16, (len(text)-2)/2)
		for i := range u {
			u[i] = binary.BigEndian.Uint16(text[2+2*i:])
		}
		s = string(utf16.Decode(u))
	}
	return strings.Join(strings.Fields(s), " ")
}

// plural returns n and noun, with an s for other numbers than 1.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// brandText returns a brand as its characters, or in hex if they are not printable.
func brandText(b string) string {
	for _, r := range b {
		if r < 0x20 || r > 0x7e {
			return fmt.Sprintf("0x%x", b)
		}
	}
	return strings.TrimRight(b, " ")
}

// ticksText returns a duration in ticks of timescale as a time and as ticks. allOnes32 tells if a 32-bit value
// with all bits set means an unknown duration.
func ticksText(ticks uint64, timescale uint32, allOnes32 bool) string {
	if allOnes32 && ticks == 0xffffffff || ticks == 0xffffffffffffffff {
		return "unknown"
	}
	if timescale == 0 {
		return fmt.Sprintf("%d ticks, time scale 0", ticks)
	}
	return fmt.Sprintf("%s (%d at time scale %d)", seconds(ticks, timescale).Round(time.Millisecond), ticks,
		timescale)
}

// seconds returns a duration in ticks of timescale as a time.
func seconds(ticks uint64, timescale uint32) time.Duration {
	return time.Duration(float64(ticks) / float64(timescale) * float64(time.Second))
}

// clock writes a time as h:mm:ss.mmm.
func clock(d time.Duration) string {
	d = d.Round(time.Millisecond)
	h, m, s, ms := d/time.Hour, d/time.Minute%60, d/time.Second%60, d/time.Millisecond%1000
	return fmt.Sprintf("%d:%02d:%02d.%03d", h, m, s, ms)
}

// handlerName returns the name of a handler if it is printable, or "". The name of a QuickTime handler is a
// counted string, which starts with a byte with its length.
func handlerName(hdlr *mp4.HdlrBox) string {
	if hdlr.QuickTimeComponentType() != "" {
		if name, ok := hdlr.QuickTimeComponentName(); ok {
			return printableName(name)
		}
	}
	return printableName(hdlr.Name)
}

// printableName returns name without NUL characters and spaces at its ends, or "" if it is not printable UTF-8.
func printableName(name string) string {
	name = strings.Trim(name, "\x00 ")
	if !utf8.ValidString(name) {
		return ""
	}
	for _, r := range name {
		if r < 0x20 {
			return ""
		}
	}
	return name
}

// topBox returns the first top-level box of type typ in t, or nil.
func topBox(t *boxview.Tree, typ string) *boxview.Node {
	for _, n := range t.Boxes {
		if n.Type == typ {
			return n
		}
	}
	return nil
}

// child returns the first child box of n of type typ, or nil. n may be nil.
func child(n *boxview.Node, typ string) *boxview.Node {
	for _, c := range kids(n) {
		if c.Type == typ && c.IsBox() {
			return c
		}
	}
	return nil
}

// children returns the child boxes of n of type typ. n may be nil.
func children(n *boxview.Node, typ string) []*boxview.Node {
	var out []*boxview.Node
	for _, c := range kids(n) {
		if c.Type == typ && c.IsBox() {
			out = append(out, c)
		}
	}
	return out
}

// boxOf returns the box of n as a T, or the zero T if n is nil or its box is not a T.
func boxOf[T mp4.Box](n *boxview.Node) T {
	var zero T
	if n == nil {
		return zero
	}
	b, ok := n.Box.(T)
	if !ok {
		return zero
	}
	return b
}
