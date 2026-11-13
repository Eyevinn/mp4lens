package boxview

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Eyevinn/mp4ff/mp4"
)

// quickTimeBoxes are notes of box types that are in the QuickTime File Format, but not in ISOBMFF.
var quickTimeBoxes = map[string]string{
	"wide":             "QuickTime atom that makes room for a 64-bit size of the next atom",
	"wave":             "QuickTime sound information atom, with the decoder configuration of a sound sample description",
	"chan":             "QuickTime audio channel layout; ISOBMFF has chnl",
	"keys":             "QuickTime metadata keys",
	"tmcd":             "QuickTime timecode",
	"gmhd":             "QuickTime base media information header",
	"gmin":             "QuickTime base media information",
	"tapt":             "QuickTime track aperture mode dimensions",
	"clef":             "QuickTime clean aperture dimensions",
	"prof":             "QuickTime production aperture dimensions",
	"enof":             "QuickTime encoded pixels dimensions",
	"fiel":             "QuickTime field handling",
	"gama":             "QuickTime gamma level",
	"alis":             "QuickTime alias data reference",
	"rsrc":             "QuickTime resource alias data reference",
	"ctab":             "QuickTime color table",
	"load":             "QuickTime track load settings",
	"imap":             "QuickTime track input map",
	"clip":             "QuickTime clipping",
	"crgn":             "QuickTime clipping region",
	"matt":             "QuickTime track matte",
	"kmat":             "QuickTime compressed matte",
	"pnot":             "QuickTime preview",
	"sowt":             "QuickTime little-endian PCM sample entry; ISOBMFF has ipcm",
	"twos":             "QuickTime big-endian PCM sample entry; ISOBMFF has ipcm",
	"in24":             "QuickTime 24-bit PCM sample entry; ISOBMFF has ipcm",
	"in32":             "QuickTime 32-bit PCM sample entry; ISOBMFF has ipcm",
	"fl32":             "QuickTime 32-bit float PCM sample entry; ISOBMFF has fpcm",
	"fl64":             "QuickTime 64-bit float PCM sample entry; ISOBMFF has fpcm",
	"lpcm":             "QuickTime PCM sample entry; ISOBMFF has ipcm and fpcm",
	"raw ":             "QuickTime uncompressed sample entry",
	"\x00\x00\x00\x00": "QuickTime terminator atom, which ends a list of atoms, such as in wave",
	"cmov":             "QuickTime compressed movie atom",
	"mebx":             "QuickTime timed metadata sample entry",
	"XMP_":             "XMP metadata in QuickTime user data",
	"SVQ1":             "Sorenson Video sample entry (QuickTime)", "SVQ3": "Sorenson Video 3 sample entry (QuickTime)",
	"cvid": "Cinepak sample entry (QuickTime)", "rle ": "QuickTime Animation sample entry",
	"8BPS": "QuickTime Planar RGB sample entry", "rpza": "QuickTime Video sample entry",
	"apch": "Apple ProRes 422 HQ sample entry", "apcn": "Apple ProRes 422 sample entry",
	"apcs": "Apple ProRes 422 LT sample entry", "apco": "Apple ProRes 422 Proxy sample entry",
	"ap4h": "Apple ProRes 4444 sample entry", "ap4x": "Apple ProRes 4444 XQ sample entry",
}

// boxQuickTime returns what is specific to QuickTime in the box of n, or "".
func boxQuickTime(n *Node) string {
	if note, ok := quickTimeBoxes[n.Type]; ok {
		return note
	}
	if len(n.Type) == 4 && n.Type[0] == 0xa9 && n.Parent != nil && n.Parent.Type == "udta" {
		return "QuickTime user data text atom; ISOBMFF files keep such metadata in meta/ilst"
	}
	switch b := n.Box.(type) {
	case *mp4.QuickTimeTextSampleEntryBox:
		return "QuickTime text sample entry"
	case *mp4.TextSampleEntryBox:
		if b.Type() == "text" {
			return "QuickTime text sample entry type with the 3GPP timed text layout of tx3g, as ffmpeg writes it " +
				"for chapter tracks"
		}
	case *mp4.UnknownBox:
		if n.Type == "text" && n.Parent != nil && n.Parent.Type == "gmhd" {
			return "Undocumented QuickTime atom of text tracks, which Apple players require for chapters; ffmpeg " +
				"writes its bytes, an identity matrix, verbatim"
		}
	case *mp4.NameBox:
		if !b.FullBox {
			return "QuickTime user data name of the movie or track"
		}
	case *mp4.UserDataTextBox:
		if n.Type == "name" {
			return "QuickTime name of the source tape of the timecode, as a user data text"
		}
	case *mp4.FtypBox:
		if b.MajorBrand() == "qt  " {
			return "QuickTime brand qt as major brand"
		}
	case *mp4.MetaBox:
		if b.IsQuickTime() {
			return "QuickTime meta atom, without the version and flags of the ISOBMFF meta box"
		}
	case *mp4.UdtaBox:
		if len(b.TrailingBytes) > 0 {
			return fmt.Sprintf("Ends with %d bytes after the last atom, such as the 32-bit zero that ends QuickTime user data",
				len(b.TrailingBytes))
		}
	case *mp4.HdlrBox:
		inMinf := n.Parent != nil && n.Parent.Type == "minf"
		switch componentType := b.QuickTimeComponentType(); {
		case componentType == mp4.QuickTimeDataHandler || inMinf && b.PreDefined == 0:
			return fmt.Sprintf("QuickTime data handler in minf, which ISOBMFF does not have; its handler type %q is "+
				"the type of data reference that it reads, such as alis for the alis entries of dref", b.HandlerType)
		case componentType == mp4.QuickTimeMediaHandler:
			return "QuickTime media handler (component type mhlr, pre_defined 0 in ISOBMFF); the handler type is the " +
				"media type, as in ISOBMFF"
		case b.PreDefined != 0:
			return fmt.Sprintf("QuickTime component type %q, which is pre_defined (0) in ISOBMFF", fourCC(b.PreDefined))
		}
	case *mp4.MvhdBox:
		if b.QuickTimePreviewTime|b.QuickTimePreviewDuration|b.QuickTimePosterTime|b.QuickTimeSelectionTime|
			b.QuickTimeSelectionDuration|b.QuickTimeCurrentTime != 0 {
			return "QuickTime preview, poster, selection or current time set, which are pre_defined (0) in ISOBMFF"
		}
	case *mp4.MdhdBox:
		if b.QuickTimeQuality != 0 {
			return "QuickTime media quality set, which is pre_defined (0) in ISOBMFF"
		}
	case *mp4.VisualSampleEntryBox:
		if b.QuickTimeVersion != 0 || b.QuickTimeRevisionLevel != 0 || b.QuickTimeVendor != 0 ||
			b.QuickTimeTemporalQuality != 0 || b.QuickTimeSpatialQuality != 0 {
			return "QuickTime version, vendor or quality set, which are pre_defined (0) in ISOBMFF"
		}
	case *mp4.AudioSampleEntryBox:
		switch {
		case b.QuickTimeV2 != nil:
			return "QuickTime sound description version 2"
		case b.QuickTimeV1 != nil:
			return "QuickTime sound description version 1"
		case b.QuickTimeVersion != 0 || b.QuickTimeRevisionLevel != 0 || b.QuickTimeVendor != 0 || b.CompressionID != 0:
			return "QuickTime version, vendor or compression ID set, which are reserved (0) in ISOBMFF"
		}
	}
	return ""
}

// fileQuickTime returns why the file of t is a QuickTime file, or "".
func fileQuickTime(t *Tree) string {
	if len(t.Boxes) == 0 {
		return ""
	}
	for _, n := range t.Boxes {
		if ftyp, ok := n.Box.(*mp4.FtypBox); ok {
			switch {
			case ftyp.MajorBrand() == "qt  ":
				return "the major brand is qt"
			case slices.Contains(ftyp.CompatibleBrands(), "qt  "):
				return "qt is a compatible brand"
			}
			return ""
		}
	}
	for _, n := range t.Boxes {
		if n.Type == "moov" {
			return "the file has no ftyp box, as old QuickTime files"
		}
	}
	return ""
}

// hasQuickTime tells if n, or a field of n, is specific to QuickTime.
func hasQuickTime(n *Node) bool {
	if n.QuickTime != "" {
		return true
	}
	for _, f := range n.Fields() {
		if strings.HasPrefix(normalKey(f.Key), "quicktime") {
			return true
		}
	}
	return false
}

// normalKey returns key in lower case without spaces and underscores, so that "quickTimeVendor",
// "quicktime_vendor" and "QuickTimeVendor" are the same.
func normalKey(key string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "_", "").Replace(key))
}

func fourCC(v uint32) string {
	return string([]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}
