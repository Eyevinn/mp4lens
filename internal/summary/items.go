package summary

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/Eyevinn/mp4ff/hevc"
	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/Eyevinn/mp4lens/internal/boxview"
)

// auxTypes are the names of the types of auxiliary images, from their URNs in auxC.
var auxTypes = map[string]string{
	"urn:mpeg:mpegB:cicp:systems:auxiliary:alpha": "alpha plane",
	"urn:mpeg:hevc:2015:auxid:1":                  "alpha plane",
	"urn:mpeg:mpegB:cicp:systems:auxiliary:depth": "depth map",
	"urn:mpeg:hevc:2015:auxid:2":                  "depth map",
	"urn:com:apple:photo:2020:aux:hdrgainmap":     "HDR gain map",
}

// maxGridData is the largest data of a grid item that a summary reads.
const maxGridData = 64

// itemsSection returns the items of a still image file, such as HEIF, HEIC or AVIF, from the meta box at the top
// level of the file, whose bytes are in r: the primary item, and for each item its type, size, codec and what it
// is for. The tiles of grids are summarized in their grids.
func itemsSection(t *boxview.Tree, r io.ReaderAt) Section {
	metaNode := topBox(t, "meta")
	meta := boxOf[*mp4.MetaBox](metaNode)
	if meta == nil || meta.Iinf == nil {
		return Section{}
	}
	s := Section{Title: "Items", Key: metaNode.Key}
	var primary uint32
	if meta.Pitm != nil {
		primary = meta.Pitm.ItemID
		s.add("primary item", fmt.Sprintf("item %d", primary), child(metaNode, "pitm"))
	}
	infeNodes := children(child(metaNode, "iinf"), "infe")
	tiles := map[uint32]bool{}
	for _, infe := range meta.Iinf.Infes {
		if infe.ItemType == "grid" {
			for _, id := range toItems(meta, "dimg", infe.ItemID) {
				tiles[id] = true
			}
		}
	}
	for i, infe := range meta.Iinf.Infes {
		if tiles[infe.ItemID] {
			continue
		}
		var n *boxview.Node
		if i < len(infeNodes) {
			n = infeNodes[i]
		}
		s.add(fmt.Sprintf("item %d", infe.ItemID), itemText(t, meta, infe, primary, r), n)
		if mime := fileImageType(t); infe.ItemID == primary && t.Size <= maxImage && mime != "" {
			s.Rows[len(s.Rows)-1].Image = &Image{Pos: 0, Size: uint64(t.Size), Type: mime}
		}
	}
	return s
}

// fileImageType returns the media type of a still image file that browsers may display as a whole, from the
// brands of its ftyp box: image/avif for AVIF, which most browsers display, and image/heic for HEIC, which Safari
// displays. It returns "" for other files.
func fileImageType(t *boxview.Tree) string {
	switch {
	case hasBrand(t, "avif"):
		return "image/avif"
	case hasBrand(t, "heic"), hasBrand(t, "heix"):
		return "image/heic"
	}
	return ""
}

// itemText describes an item: its type or codec, the image of an image item, what it is for and its size.
func itemText(t *boxview.Tree, meta *mp4.MetaBox, infe *mp4.InfeBox, primary uint32, r io.ReaderAt) string {
	id := infe.ItemID
	img := imageOf(meta, id)
	head := infe.ItemType
	switch {
	case infe.ItemType == "mime":
		head = "mime " + infe.ContentType
	case infe.ItemType == "uri ":
		head = "uri " + infe.ItemURIType
	case img.codec != "":
		head = img.codec
	}
	if img.size != "" {
		head += " " + img.size
	}
	out := []string{head}
	if infe.ItemType == "grid" {
		out = append(out, gridText(t, meta, id, r))
	}
	out = append(out, img.details...)
	switch {
	case id == primary:
		out = append(out, "primary image")
	case infe.IsHidden():
		out = append(out, "hidden")
	}
	for _, ref := range []struct{ name, role string }{{"thmb", "thumbnail"}, {"auxl", "auxiliary image"},
		{"cdsc", "metadata"}} {
		if ids := toItems(meta, ref.name, id); len(ids) > 0 {
			out = append(out, fmt.Sprintf("%s of %s", ref.role, itemList(ids)))
		}
	}
	if size, ok := itemSize(meta, id); ok && infe.ItemType != "grid" {
		out = append(out, plural(int(size), "byte"))
	}
	return strings.Join(out, ", ")
}

// itemImage is what the properties of an item say about its image.
type itemImage struct {
	codec   string   // the RFC 6381 codecs parameter, such as hvc1.1.6.L93.B0, or ""
	size    string   // the width and height, as 600x338, or ""
	details []string // the type of an auxiliary image, bits per channel, transformations and colour
}

// imageOf returns what the properties of item id say about its image.
func imageOf(meta *mp4.MetaBox, id uint32) itemImage {
	var img itemImage
	if meta.Iprp == nil {
		return img
	}
	var aux, colour []string
	for _, p := range meta.Iprp.GetItemProperties(id) {
		switch b := p.Property.(type) {
		case *mp4.IspeBox:
			img.size = fmt.Sprintf("%dx%d", b.ImageWidth, b.ImageHeight)
		case *mp4.HvcCBox:
			img.codec = hevcCodec(b)
		case *mp4.Av1CBox:
			img.codec = b.CodecString("av01")
		case *mp4.AuxCBox:
			name := auxTypes[b.AuxType]
			if name == "" {
				name = b.AuxType
			}
			aux = append(aux, name)
		case *mp4.PixiBox:
			img.details = append(img.details, bitsText(b.BitsPerChannel))
		case *mp4.IrotBox:
			if b.Angle&3 != 0 {
				img.details = append(img.details, fmt.Sprintf("rotated %d° anti-clockwise", int(b.Angle&3)*90))
			}
		case *mp4.ImirBox:
			if b.Axis&1 == 0 {
				img.details = append(img.details, "mirrored top to bottom")
			} else {
				img.details = append(img.details, "mirrored left to right")
			}
		case *mp4.ColrBox:
			if text := colrText(b); text != "" {
				colour = append(colour, text)
			}
		}
	}
	img.details = append(append(aux, img.details...), colour...)
	return img
}

// bitsText returns the bits per channel of pixi, as "8 bits, 3 channels".
func bitsText(bits []byte) string {
	if len(bits) == 0 {
		return ""
	}
	for _, b := range bits[1:] {
		if b != bits[0] {
			var each []string
			for _, b := range bits {
				each = append(each, strconv.Itoa(int(b)))
			}
			return strings.Join(each, "/") + " bits"
		}
	}
	return fmt.Sprintf("%d bits, %s", bits[0], plural(len(bits), "channel"))
}

// hevcCodec returns the RFC 6381 codecs parameter of an HEVC image item, or hvc1 if its SPS cannot be parsed.
func hevcCodec(b *mp4.HvcCBox) string {
	if spss := b.GetNalusForType(hevc.NALU_SPS); len(spss) > 0 {
		if sps, err := hevc.ParseSPSNALUnit(spss[0]); err == nil {
			return hevc.CodecString("hvc1", sps)
		}
	}
	return "hvc1"
}

// gridText describes a grid item: its rows and columns, and its tiles.
func gridText(t *boxview.Tree, meta *mp4.MetaBox, id uint32, r io.ReaderAt) string {
	tiles := toItems(meta, "dimg", id)
	text := "tiles"
	if data := itemData(t, meta, id, r); data != nil {
		if g, err := mp4.DecodeImageGrid(data); err == nil {
			text = fmt.Sprintf("%s and %s of tiles", plural(int(g.Rows), "row"), plural(int(g.Columns), "column"))
		}
	}
	if len(tiles) == 0 {
		return text
	}
	if tile := imageOf(meta, tiles[0]); tile.codec != "" || tile.size != "" {
		text += strings.TrimRight(" "+tile.codec+" "+tile.size, " ")
	}
	var size uint64
	for _, tile := range tiles {
		s, _ := itemSize(meta, tile)
		size += s
	}
	return fmt.Sprintf("%s (%s, %s)", text, itemList(tiles), plural(int(size), "byte"))
}

// toItems returns the items that item id refers to with references of type name, or nil.
func toItems(meta *mp4.MetaBox, name string, id uint32) []uint32 {
	if meta.Iref == nil {
		return nil
	}
	return meta.Iref.GetToItemIDs(name, id)
}

// itemList writes item IDs, with runs of consecutive IDs as ranges, as "items 1-48, 50".
func itemList(ids []uint32) string {
	if len(ids) == 1 {
		return fmt.Sprintf("item %d", ids[0])
	}
	var parts []string
	for i := 0; i < len(ids); {
		j := i
		for j+1 < len(ids) && ids[j+1] == ids[j]+1 {
			j++
		}
		switch j {
		case i:
			parts = append(parts, strconv.Itoa(int(ids[i])))
		case i + 1:
			parts = append(parts, strconv.Itoa(int(ids[i])), strconv.Itoa(int(ids[j])))
		default:
			parts = append(parts, fmt.Sprintf("%d-%d", ids[i], ids[j]))
		}
		i = j + 1
	}
	return "items " + strings.Join(parts, ", ")
}

// itemSize returns the number of bytes of the data of item id, the sum of its extents. ok is false if iloc does
// not have the item, or an extent has length 0, which means the rest of the data.
func itemSize(meta *mp4.MetaBox, id uint32) (uint64, bool) {
	if meta.Iloc == nil {
		return 0, false
	}
	item := meta.Iloc.GetItem(id)
	if item == nil || len(item.Extents) == 0 {
		return 0, false
	}
	var size uint64
	for _, e := range item.Extents {
		if e.Length == 0 {
			return 0, false
		}
		size += e.Length
	}
	return size, true
}

// itemData returns the data of item id, read from r or from the idat box, or nil if it is not in this file, is
// larger than maxGridData bytes, or cannot be read.
func itemData(t *boxview.Tree, meta *mp4.MetaBox, id uint32, r io.ReaderAt) []byte {
	if meta.Iloc == nil || r == nil {
		return nil
	}
	item := meta.Iloc.GetItem(id)
	size, ok := itemSize(meta, id)
	if item == nil || !ok || size > maxGridData || item.DataReferenceIndex != 0 {
		return nil
	}
	var data []byte
	for _, e := range item.Extents {
		start := item.BaseOffset + e.Offset
		switch item.ConstructionMethod {
		case mp4.IlocConstructionFile:
			if start+e.Length > uint64(t.Size) {
				return nil
			}
			b := make([]byte, e.Length)
			if _, err := r.ReadAt(b, int64(start)); err != nil {
				return nil
			}
			data = append(data, b...)
		case mp4.IlocConstructionIdat:
			if meta.Idat == nil || start+e.Length > uint64(len(meta.Idat.Data)) {
				return nil
			}
			data = append(data, meta.Idat.Data[start:start+e.Length]...)
		default:
			return nil
		}
	}
	return data
}

// hasBrand tells if brand is the major brand or a compatible brand of the ftyp box of the file of t.
func hasBrand(t *boxview.Tree, brand string) bool {
	ftyp := boxOf[*mp4.FtypBox](topBox(t, "ftyp"))
	return ftyp != nil && (ftyp.MajorBrand() == brand || slices.Contains(ftyp.CompatibleBrands(), brand))
}
