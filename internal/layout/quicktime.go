package layout

import (
	"strconv"
	"strings"

	"github.com/Eyevinn/mp4ff/mp4"
)

// quickTimeField is the note of a field that mp4ff names as a QuickTime field.
const quickTimeField = "QuickTime field; reserved or pre_defined in ISOBMFF"

// quickTimeNotes are the notes of fields that have a QuickTime meaning in bytes that ISOBMFF reserves, by box
// type or sample entry kind, and field name. Such a field gets its note, or quickTimeField if it has none here,
// only if it is not 0, since a 0 is what ISOBMFF has there.
var quickTimeNotes = map[string]map[string]string{
	"mvhd": {
		"QuickTimePreviewTime":       "QuickTime preview time; pre_defined (0) in ISOBMFF",
		"QuickTimePreviewDuration":   "QuickTime preview duration; pre_defined (0) in ISOBMFF",
		"QuickTimePosterTime":        "QuickTime poster time; pre_defined (0) in ISOBMFF",
		"QuickTimeSelectionTime":     "QuickTime selection time; pre_defined (0) in ISOBMFF",
		"QuickTimeSelectionDuration": "QuickTime selection duration; pre_defined (0) in ISOBMFF",
		"QuickTimeCurrentTime":       "QuickTime current time; pre_defined (0) in ISOBMFF",
	},
	"mdhd": {
		"QuickTimeQuality": "QuickTime media quality; pre_defined (0) in ISOBMFF",
	},
	"hdlr": {
		"PreDefined":  "QuickTime component type, such as mhlr or dhlr; pre_defined (0) in ISOBMFF",
		"Reserved[0]": "QuickTime component manufacturer; reserved (0) in ISOBMFF",
		"Reserved[1]": "QuickTime component flags; reserved (0) in ISOBMFF",
		"Reserved[2]": "QuickTime component flags mask; reserved (0) in ISOBMFF",
	},
	"visual": {
		"QuickTimeVersion":         "QuickTime sample description version; pre_defined (0) in ISOBMFF",
		"QuickTimeRevisionLevel":   "QuickTime revision level; reserved (0) in ISOBMFF",
		"QuickTimeVendor":          "QuickTime vendor; pre_defined (0) in ISOBMFF",
		"QuickTimeTemporalQuality": "QuickTime temporal quality; pre_defined (0) in ISOBMFF",
		"QuickTimeSpatialQuality":  "QuickTime spatial quality; pre_defined (0) in ISOBMFF",
	},
	"audio": {
		"QuickTimeVersion":       "QuickTime sound description version; reserved (0) in ISOBMFF",
		"QuickTimeRevisionLevel": "QuickTime revision level; reserved (0) in ISOBMFF",
		"QuickTimeVendor":        "QuickTime vendor; reserved (0) in ISOBMFF",
		"CompressionID":          "QuickTime compression ID; pre_defined (0) in ISOBMFF",
		"PacketSize":             "QuickTime packet size; reserved (0) in ISOBMFF",
	},
}

// countedNameNote is the note of the name of a QuickTime handler that is a counted string.
const countedNameNote = "QuickTime component name, a counted string: a byte with the length, then the name; " +
	"zero-terminated UTF-8 in ISOBMFF"

// handlerFields gives the fields of a handler the keys of the Info lines of mp4ff, which has QuickTime names for
// the fields of a QuickTime handler, and decodes the name of a QuickTime handler that is a counted string. The
// QuickTime notes must have been added.
func handlerFields(b *mp4.HdlrBox, fields []Field) {
	qt := b.QuickTimeComponentType() != ""
	name, counted := b.QuickTimeComponentName()
	keys := map[string]string{"bytesLeft": "handlerName"}
	if qt {
		keys = map[string]string{"PreDefined": "componentType", "Reserved[0]": "componentManufacturer",
			"Reserved[1]": "componentFlags", "Reserved[2]": "componentFlagsMask", "bytesLeft": "handlerName"}
		if counted {
			keys["bytesLeft"] = "componentName"
		}
	}
	for i := range fields {
		f := &fields[i]
		f.Key = keys[f.Name]
		if f.Name == "bytesLeft" && qt && counted && name != "" {
			f.Value += " (counted string " + strconv.Quote(name) + ")"
			f.QuickTime = countedNameNote
		}
	}
}

// soundDescriptionNote is the note of the fields of QuickTime sound descriptions of version 1 and 2, which
// ISOBMFF does not have at all.
const soundDescriptionNote = "QuickTime sound description field; not in ISOBMFF"

// addQuickTimeNotes sets the QuickTime notes of the fields of box, which may be nil if it could not be decoded.
func addQuickTimeNotes(box mp4.Box, fields []Field) {
	kind := ""
	switch box.(type) {
	case nil:
	case *mp4.VisualSampleEntryBox:
		kind = "visual"
	case *mp4.AudioSampleEntryBox:
		kind = "audio"
	default:
		kind = box.Type()
	}
	for i := range fields {
		f := &fields[i]
		if f.QuickTime == soundDescriptionNote {
			continue
		}
		note := quickTimeNotes[kind][f.Name]
		if note == "" {
			note = f.QuickTime
		}
		f.QuickTime = ""
		if note != "" && strings.Trim(f.Value, "0") != "" {
			f.QuickTime = note
		}
	}
}
