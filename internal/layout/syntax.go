package layout

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The syntax tables give the fields of a layout the names that the specification of the box uses, such as
// entry_count, say what each field is, and split the values that pack several fields, such as the version and
// flags of a full box, or the sample flags of a track fragment.

// boxSyntax is the syntax of a box type.
type boxSyntax struct {
	// spec is the specification and clause that defines the box.
	spec string
	// flags are the names of the flag bits of a full box.
	flags []flagBit
	// fields are the fields, by the name that the layout gives them, without the number of a repeated field.
	fields map[string]fieldSyntax
}

// flagBit is a flag bit of a full box.
type flagBit struct {
	mask uint32
	name string
}

// fieldSyntax is the syntax of a field.
type fieldSyntax struct {
	// name is the name in the specification.
	name string
	// about says what the field is.
	about string
	// show writes the value of the field in its meaning, such as a date or a fixed-point number, or is nil.
	show func(v uint64) string
}

const isobmff = "ISO/IEC 14496-12:2026"

// Field syntaxes that several boxes share.
var (
	creationTime     = fieldSyntax{"creation_time", "creation time, in seconds since 1904-01-01 UTC", showMacTime}
	modificationTime = fieldSyntax{"modification_time", "last modification time, in seconds since 1904-01-01 UTC", showMacTime}
	matrix           = fieldSyntax{"matrix", "transformation matrix of the video, a b u c d v x y w, with u, v and w " +
		"in 2.30 and the others in 16.16 fixed point", nil}
	entryCount = fieldSyntax{"entry_count", "number of entries that follow", nil}
	trackID    = fieldSyntax{"track_ID", "identifier of the track, unique in the movie", nil}
	sampleFlag = func(name, about string) fieldSyntax { return fieldSyntax{name, about, showSampleFlags} }
)

var syntaxes = map[string]boxSyntax{
	"ftyp": {spec: isobmff + " 5.2", fields: brandFields},
	"styp": {spec: isobmff + " 8.14.2", fields: brandFields},
	"mvhd": {spec: isobmff + " 8.2.2", fields: map[string]fieldSyntax{
		"CreationTime": creationTime, "ModificationTime": modificationTime,
		"Timescale":            {"timescale", "number of time units per second for the times of the movie", nil},
		"Duration":             {"duration", "duration of the longest track, in the movie time scale", nil},
		"Rate":                 {"rate", "preferred playback rate, in 16.16 fixed point", showFixed16_16},
		"Volume":               {"volume", "preferred volume, in 8.8 fixed point", showFixed8_8},
		"Reserved bytes":       {"reserved", "reserved, 0", nil},
		"Matrix[]":             matrix,
		"QuickTimePreviewTime": {"pre_defined", "0 in ISOBMFF", nil}, "QuickTimePreviewDuration": {"pre_defined", "0 in ISOBMFF", nil},
		"QuickTimePosterTime": {"pre_defined", "0 in ISOBMFF", nil}, "QuickTimeSelectionTime": {"pre_defined", "0 in ISOBMFF", nil},
		"QuickTimeSelectionDuration": {"pre_defined", "0 in ISOBMFF", nil}, "QuickTimeCurrentTime": {"pre_defined", "0 in ISOBMFF", nil},
		"NextTrackID": {"next_track_ID", "a track ID larger than any track ID in use", nil},
	}},
	"tkhd": {spec: isobmff + " 8.3.2",
		flags: []flagBit{{0x1, "track_enabled"}, {0x2, "track_in_movie"}, {0x4, "track_in_preview"},
			{0x8, "track_size_is_aspect_ratio"}},
		fields: map[string]fieldSyntax{
			"CreationTime": creationTime, "ModificationTime": modificationTime, "TrackID": trackID,
			"ReservedAfterTrackID": {"reserved", "reserved, 0", nil},
			"Duration":             {"duration", "duration of the track, in the movie time scale", nil},
			"Reserved 8 x 0":       {"reserved", "reserved, 0", nil},
			"Layer":                {"layer", "front-to-back order of video tracks; lower is closer to the viewer", nil},
			"AlternateGroup":       {"alternate_group", "group of tracks that are alternatives to each other, or 0", nil},
			"Volume":               {"volume", "volume of an audio track, in 8.8 fixed point", showFixed8_8},
			"Matrix[]":             matrix,
			"Width":                {"width", "width of the presentation, in 16.16 fixed point", showFixed16_16},
			"Height":               {"height", "height of the presentation, in 16.16 fixed point", showFixed16_16},
		}},
	"mdhd": {spec: isobmff + " 8.4.2", fields: map[string]fieldSyntax{
		"CreationTime": creationTime, "ModificationTime": modificationTime,
		"Timescale":        {"timescale", "number of time units per second for the times of the media", nil},
		"Duration":         {"duration", "duration of the media, in its time scale", nil},
		"Language":         {"language", "a pad bit and an ISO 639-2/T code in three 5-bit letters", showLanguage},
		"QuickTimeQuality": {"pre_defined", "0 in ISOBMFF", nil},
	}},
	"hdlr": {spec: isobmff + " 8.4.3", fields: map[string]fieldSyntax{
		"PreDefined":  {"pre_defined", "0 in ISOBMFF", nil},
		"HandlerType": {"handler_type", "type of the media or metadata, such as vide, soun or meta", nil},
		"Reserved[]":  {"reserved", "reserved, 0", nil},
		"bytesLeft":   {"name", "readable name of the track type, as zero-terminated UTF-8", nil},
	}},
	"stsd": {spec: isobmff + " 8.5.2", fields: map[string]fieldSyntax{
		"sampleCount": {"entry_count", "number of sample entries that follow", nil},
	}},
	"stts": {spec: isobmff + " 8.6.1.2", fields: map[string]fieldSyntax{
		"entryCount":  entryCount,
		"SampleCount": {"sample_count", "number of consecutive samples with this duration", nil},
		"SampleDelta": {"sample_delta", "duration of each of these samples, in the media time scale", nil},
	}},
	"ctts": {spec: isobmff + " 8.6.1.3", fields: map[string]fieldSyntax{
		"entryCount":   entryCount,
		"SampleCount":  {"sample_count", "number of consecutive samples with this offset", nil},
		"SampleOffset": {"sample_offset", "composition time minus decoding time, in the media time scale; signed in version 1", nil},
	}},
	"stss": {spec: isobmff + " 8.6.2", fields: map[string]fieldSyntax{
		"entryCount":   entryCount,
		"SampleNumber": {"sample_number", "number of a sync sample, from 1", nil},
	}},
	"sdtp": {spec: isobmff + " 8.6.4", fields: map[string]fieldSyntax{
		"entries": {"sample", "is_leading, sample_depends_on, sample_is_depended_on and sample_has_redundancy of a sample, " +
			"2 bits each", showSampleDependency},
	}},
	"elst": {spec: isobmff + " 8.6.6", fields: map[string]fieldSyntax{
		"entryCount":                entryCount,
		"Entries[].SegmentDuration": {"edit_duration", "duration of the edit, in the movie time scale", nil},
		"Entries[].MediaTime": {"media_time", "start of the edit in the media, in the media time scale, or -1 for an " +
			"empty edit", nil},
		"Entries[].MediaRateInteger":  {"media_rate_integer", "playback rate of the edit: 1 for normal, 0 to dwell on one time", nil},
		"Entries[].MediaRateFraction": {"media_rate_fraction", "0", nil},
	}},
	"dref": {spec: isobmff + " 8.7.2", fields: map[string]fieldSyntax{
		"entryCount": {"entry_count", "number of data entries that follow", nil},
	}},
	"url ": {spec: isobmff + " 8.7.2", flags: []flagBit{{0x1, "media data in the same file"}}},
	"stsz": {spec: isobmff + " 8.7.3", fields: map[string]fieldSyntax{
		"SampleUniformSize": {"sample_size", "size of every sample if they all have the same size, and otherwise 0", nil},
		"SampleNumber":      {"sample_count", "number of samples in the track", nil},
		"SampleSize":        {"entry_size", "size of a sample in bytes", nil},
	}},
	"stsc": {spec: isobmff + " 8.7.4", fields: map[string]fieldSyntax{
		"entryCount": entryCount,
		"Entries[].FirstChunk": {"first_chunk", "number of the first chunk of a run of chunks with the same samples " +
			"per chunk, from 1", nil},
		"Entries[].SamplesPerChunk": {"samples_per_chunk", "number of samples in each of these chunks", nil},
		"sdi":                       {"sample_description_index", "index of the sample entry of these samples, from 1", nil},
	}},
	"stco": {spec: isobmff + " 8.7.5", fields: chunkOffsets},
	"co64": {spec: isobmff + " 8.7.5", fields: chunkOffsets},
	"mehd": {spec: isobmff + " 8.8.2", fields: map[string]fieldSyntax{
		"FragmentDuration": {"fragment_duration", "duration of the whole movie with all its fragments, in the movie time scale", nil},
	}},
	"trex": {spec: isobmff + " 8.8.3", fields: map[string]fieldSyntax{
		"TrackID":                       trackID,
		"DefaultSampleDescriptionIndex": {"default_sample_description_index", "sample entry of the samples in fragments, from 1", nil},
		"DefaultSampleDuration":         {"default_sample_duration", "duration of the samples in fragments, in the media time scale", nil},
		"DefaultSampleSize":             {"default_sample_size", "size of the samples in fragments, in bytes", nil},
		"DefaultSampleFlags":            sampleFlag("default_sample_flags", "sample flags of the samples in fragments"),
	}},
	"mfhd": {spec: isobmff + " 8.8.5", fields: map[string]fieldSyntax{
		"sequenceNumber": {"sequence_number", "number of the fragment, which increases in decoding order", nil},
	}},
	"tfhd": {spec: isobmff + " 8.8.7",
		flags: []flagBit{{0x000001, "base-data-offset-present"}, {0x000002, "sample-description-index-present"},
			{0x000008, "default-sample-duration-present"}, {0x000010, "default-sample-size-present"},
			{0x000020, "default-sample-flags-present"}, {0x010000, "duration-is-empty"},
			{0x020000, "default-base-is-moof"}},
		fields: map[string]fieldSyntax{
			"TrackID":                trackID,
			"BaseDataOffset":         {"base_data_offset", "file offset that the data offsets of the fragment start from", nil},
			"SampleDescriptionIndex": {"sample_description_index", "sample entry of the samples, from 1", nil},
			"DefaultSampleDuration":  {"default_sample_duration", "duration of the samples, in the media time scale", nil},
			"DefaultSampleSize":      {"default_sample_size", "size of the samples, in bytes", nil},
			"DefaultSampleFlags":     sampleFlag("default_sample_flags", "sample flags of the samples"),
		}},
	"trun": {spec: isobmff + " 8.8.8",
		flags: []flagBit{{0x000001, "data-offset-present"}, {0x000004, "first-sample-flags-present"},
			{0x000100, "sample-duration-present"}, {0x000200, "sample-size-present"}, {0x000400, "sample-flags-present"},
			{0x000800, "sample-composition-time-offsets-present"}},
		fields: map[string]fieldSyntax{
			"sampleCount":      {"sample_count", "number of samples in the run", nil},
			"DataOffset":       {"data_offset", "offset of the data of the first sample, from the base data offset", nil},
			"firstSampleFlags": sampleFlag("first_sample_flags", "sample flags of the first sample, instead of the default"),
			"dur":              {"sample_duration", "duration of the sample, in the media time scale", nil},
			"size":             {"sample_size", "size of the sample, in bytes", nil},
			"flags":            sampleFlag("sample_flags", "sample flags of the sample"),
			"cto": {"sample_composition_time_offset", "composition time minus decoding time of the sample; signed " +
				"in version 1", nil},
		}},
	"tfdt": {spec: isobmff + " 8.8.12", fields: map[string]fieldSyntax{
		"baseMediaDecodeTime": {"baseMediaDecodeTime", "decoding time of the first sample of the fragment, in the media time scale", nil},
	}},
	"sidx": {spec: isobmff + " 8.14.3", fields: map[string]fieldSyntax{
		"ReferenceID":              {"reference_ID", "track ID of the reference stream", nil},
		"Timescale":                {"timescale", "number of time units per second for the times in the box", nil},
		"EarliestPresentationTime": {"earliest_presentation_time", "earliest presentation time of the first subsegment", nil},
		"FirstOffset":              {"first_offset", "distance from the end of the box to the first subsegment, in bytes", nil},
		"":                         {"reserved", "reserved, 0", nil},
		"refCount":                 {"reference_count", "number of references that follow", nil},
		"SubSegmentDuration":       {"subsegment_duration", "duration of the subsegment, in the time scale of the box", nil},
	}},
	// QuickTime File Format atoms.
	"chan": {spec: quickTime + ", audio channel layout atom", fields: map[string]fieldSyntax{
		"LayoutTag": {"mChannelLayoutTag", "Core Audio channel layout: the upper 16 bits name the layout, the lower 16 " +
			"bits are the number of channels", nil},
		"Bitmap":        {"mChannelBitmap", "a bit per channel present, when the layout tag says to use the bitmap", nil},
		"n":             {"mNumberChannelDescriptions", "number of channel descriptions that follow", nil},
		"Label":         {"mChannelLabel", "Core Audio label of the channel, such as 1 for left", nil},
		"Flags":         {"mChannelFlags", "how to read the coordinates", nil},
		"Coordinates[]": {"mCoordinates", "position of the channel, a 32-bit float", nil},
	}},
	"keys": {spec: quickTime + ", metadata item keys atom", fields: map[string]fieldSyntax{
		"count": entryCount,
		"size":  {"key_size", "size of the key entry, its size and namespace included", nil},
		"ns":    {"key_namespace", "naming scheme of the key, such as mdta", nil},
		"Value": {"key_value", "the key, such as com.apple.quicktime.make", nil},
	}},
	"gmin": {spec: quickTime + ", base media info atom", fields: map[string]fieldSyntax{
		"GraphicsMode": {"graphics mode", "QuickDraw transfer mode", nil},
		"Opcolor[]":    {"opcolor", "red, green and blue of the transfer mode", nil},
		"Balance":      {"balance", "sound balance", nil},
		"Reserved":     {"reserved", "reserved, 0", nil},
	}},
	"tcmi": {spec: quickTime + ", timecode media information atom", fields: map[string]fieldSyntax{
		"TextFont":          {"text font", "font number of the timecode text, or 0 for the system font", nil},
		"TextFace":          {"text face", "style bits: bold, italic, underline, outline, shadow, condense, extend", nil},
		"TextSize":          {"text size", "point size of the timecode text", nil},
		"Reserved":          {"(unlisted)", "a 16-bit field that QuickTime and ffmpeg write, but the specification lacks", nil},
		"TextColor[]":       {"text color", "red, green and blue of the timecode text", nil},
		"BackgroundColor[]": {"background color", "red, green and blue of the background", nil},
		"n":                 {"font name length", "length of the Pascal string that follows", nil},
		"FontName":          {"font name", "name of the font of the timecode text", nil},
	}},
	"tmcd": {spec: quickTime + ", timecode sample description", fields: map[string]fieldSyntax{
		"reserved":           {"reserved", "reserved, 0", nil},
		"DataReferenceIndex": {"data reference index", "data entry in dref of the samples, from 1", nil},
		"Reserved":           {"reserved", "reserved, 0", nil},
		"Flags":              {"flags", "drop frame 0x1, 24 hour max 0x2, negative times OK 0x4, counter 0x8", nil},
		"TimeScale":          {"time scale", "time units per second of the frame duration", nil},
		"FrameDuration":      {"frame duration", "duration of a frame, in the time scale", nil},
		"NumberOfFrames":     {"number of frames", "frames per second, or per counter tick", nil},
		"Reserved2":          {"reserved", "one byte, or three as in the specification", nil},
	}},
}

const quickTime = "QuickTime File Format"

var brandFields = map[string]fieldSyntax{
	"major_brand":       {"major_brand", "brand of the specification that the file conforms to best", nil},
	"minor_version":     {"minor_version", "version of the major brand", nil},
	"compatible_brands": {"compatible_brands", "a brand that the file conforms to", nil},
}

var chunkOffsets = map[string]fieldSyntax{
	"entryCount":  entryCount,
	"ChunkOffset": {"chunk_offset", "file offset of a chunk", nil},
}

// headerSyntax are the fields of every box header and full box.
var headerSyntax = map[string]fieldSyntax{
	"size":      {"size", "size of the box in bytes, its header included", nil},
	"boxType":   {"type", "four-character type of the box", nil},
	"largeSize": {"largesize", "size of the box as 64 bits, when size is 1", nil},
	"version":   {"version", "version of the format of the box", nil},
	"flags":     {"flags", "flags of the box", nil},
}

// addSyntax gives the fields of a box of boxType, which starts at pos, their names in the specification, says
// what they are, splits the version and flags of a full box, and returns the specification of the box.
func addSyntax(boxType string, pos uint64, fields []Field) ([]Field, string) {
	syn := syntaxes[boxType]
	out := make([]Field, 0, len(fields)+1)
	for i, f := range fields {
		switch {
		case f.Pos < pos+8 && (f.Name == "size" || f.Name == "boxType"):
			setSyntax(&f, headerSyntax[f.Name], "")
		case i <= 3 && f.Name == "largeSize":
			setSyntax(&f, headerSyntax["largeSize"], "")
		case f.Name == "versionAndFlags" && f.Kind == "u32" && f.Size == 4:
			out = append(out, splitVersionAndFlags(f, syn.flags)...)
			continue
		default:
			base, index := splitIndex(f.Name)
			if fs, ok := syn.fields[base]; ok {
				setSyntax(&f, fs, index)
			}
		}
		out = append(out, f)
	}
	if boxType == "sidx" {
		nameSidxReferences(out)
	}
	return out, syn.spec
}

// setSyntax gives f the name of fs, with index if not empty, and its description and meaning.
func setSyntax(f *Field, fs fieldSyntax, index string) {
	f.Spec = fs.name + index
	f.About = fs.about
	if fs.show == nil {
		return
	}
	if v, err := strconv.ParseInt(f.Value, 10, 64); err == nil {
		f.Value += " (" + fs.show(uint64(v)) + ")"
	} else if v, err := strconv.ParseUint(f.Value, 10, 64); err == nil {
		f.Value += " (" + fs.show(v) + ")"
	}
}

// splitIndex splits the number of a repeated field from its name: "SampleSize #3" gives "SampleSize" and "[3]",
// and "Entries[2].FirstChunk" gives "Entries[].FirstChunk" and "[3]", counting from 1 as the entries do.
func splitIndex(name string) (string, string) {
	if i := strings.LastIndex(name, " #"); i >= 0 {
		return name[:i], "[" + name[i+2:] + "]"
	}
	open, end := strings.Index(name, "["), strings.Index(name, "]")
	if open < 0 || end < open {
		return name, ""
	}
	n, err := strconv.Atoi(name[open+1 : end])
	if err != nil {
		return name, ""
	}
	return name[:open+1] + name[end:], "[" + strconv.Itoa(n+1) + "]"
}

// splitVersionAndFlags splits the 32-bit version and flags of a full box into its 8-bit version and 24-bit flags.
func splitVersionAndFlags(f Field, flags []flagBit) []Field {
	v, err := strconv.ParseUint(f.Value, 10, 32)
	if err != nil {
		return []Field{f}
	}
	version := Field{Pos: f.Pos, Size: 1, Kind: "u8", Name: "version", Value: strconv.FormatUint(v>>24, 10)}
	setSyntax(&version, headerSyntax["version"], "")
	fl := uint32(v) & 0xffffff
	value := fmt.Sprintf("%06x", fl)
	var set []string
	for _, b := range flags {
		if fl&b.mask != 0 {
			set = append(set, b.name)
		}
	}
	if len(set) > 0 {
		value += " (" + strings.Join(set, ", ") + ")"
	}
	flagsField := Field{Pos: f.Pos + 1, Size: 3, Kind: "u24", Name: "flags", Value: value}
	setSyntax(&flagsField, headerSyntax["flags"], "")
	return []Field{version, flagsField}
}

// nameSidxReferences names the two words of each reference of a sidx box, which pack several fields, and which
// the layout names work.
func nameSidxReferences(fields []Field) {
	ref, second := 0, false
	for i := range fields {
		f := &fields[i]
		base, _ := splitIndex(f.Name)
		switch base {
		case "work":
			v, _ := strconv.ParseUint(f.Value, 10, 32)
			if !second {
				ref++
				f.Spec = fmt.Sprintf("reference[%d]", ref)
				f.About = "reference_type (1 bit: 1 for a sidx, 0 for media) and referenced_size (31 bits, in bytes)"
				f.Value = fmt.Sprintf("%d (reference_type=%d referenced_size=%d)", v, v>>31, v&0x7fffffff)
			} else {
				f.Spec = fmt.Sprintf("sap[%d]", ref)
				f.About = "starts_with_SAP (1 bit), SAP_type (3 bits) and SAP_delta_time (28 bits)"
				f.Value = fmt.Sprintf("%d (starts_with_SAP=%d SAP_type=%d SAP_delta_time=%d)", v, v>>31, v>>28&7,
					v&0xfffffff)
			}
			second = !second
		case "SubSegmentDuration":
			f.Spec = fmt.Sprintf("subsegment_duration[%d]", ref)
		}
	}
}

// showFixed16_16 writes a 16.16 fixed-point number.
func showFixed16_16(v uint64) string {
	return strconv.FormatFloat(float64(int32(uint32(v)))/65536, 'g', -1, 64)
}

// showFixed8_8 writes an 8.8 fixed-point number.
func showFixed8_8(v uint64) string {
	return strconv.FormatFloat(float64(int16(uint16(v)))/256, 'g', -1, 64)
}

// macEpoch is the start of the times of ISOBMFF and QuickTime.
var macEpoch = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)

func showMacTime(v uint64) string {
	if v == 0 {
		return "not set"
	}
	return macEpoch.Add(time.Duration(v) * time.Second).Format(time.RFC3339)
}

func showLanguage(v uint64) string {
	b := []byte{byte(v>>10&0x1f) + 0x60, byte(v>>5&0x1f) + 0x60, byte(v&0x1f) + 0x60}
	return string(b)
}

func showSampleFlags(v uint64) string {
	return fmt.Sprintf("%#08x: is_leading=%d sample_depends_on=%d sample_is_depended_on=%d sample_has_redundancy=%d "+
		"sample_padding_value=%d sample_is_non_sync_sample=%d sample_degradation_priority=%d", v,
		v>>26&3, v>>24&3, v>>22&3, v>>20&3, v>>17&7, v>>16&1, v&0xffff)
}

func showSampleDependency(v uint64) string {
	return fmt.Sprintf("is_leading=%d sample_depends_on=%d sample_is_depended_on=%d sample_has_redundancy=%d",
		v>>6&3, v>>4&3, v>>2&3, v&3)
}
