package summary

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Eyevinn/mp4ff/aac"
	"github.com/Eyevinn/mp4ff/avc"
	"github.com/Eyevinn/mp4ff/hevc"
	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/Eyevinn/mp4lens/internal/boxview"
	"github.com/Eyevinn/mp4lens/internal/parts"
)

// trackKinds are the kinds of media of handler types.
var trackKinds = map[string]string{
	"soun": "audio", "vide": "video", "text": "text", "sbtl": "subtitles", "subt": "subtitles",
	"clcp": "closed captions", "tmcd": "timecode", "meta": "metadata", "hint": "hint", "auxv": "auxiliary video",
	"pict": "picture",
}

// sampleStats are the number, duration and size of the samples of a track.
type sampleStats struct {
	samples   uint64
	sync      uint64 // the number of sync samples
	duration  uint64 // in the media time scale
	bytes     uint64
	sampleDur uint32 // the duration of every sample, or 0 if it varies
	fragments int    // the number of track fragments, for a fragmented track
}

// addDur adds a sample duration to st, and keeps track of whether all samples have the same duration.
func (st *sampleStats) addDur(dur uint32, count uint64) {
	switch {
	case st.samples == 0:
		st.sampleDur = dur
	case st.sampleDur != dur:
		st.sampleDur = 0
	}
	st.duration += uint64(dur) * count
}

// trackSection returns what a track is, from its boxes in trak, and the samples of its fragments in frags.
// chapterOf lists the tracks that refer to each chapter track.
func trackSection(trak, moov *boxview.Node, chapterOf map[uint32][]uint32, frags map[uint32]*sampleStats) Section {
	tkhdNode := child(trak, "tkhd")
	tkhd := boxOf[*mp4.TkhdBox](tkhdNode)
	mdia := child(trak, "mdia")
	mdhdNode := child(mdia, "mdhd")
	mdhd := boxOf[*mp4.MdhdBox](mdhdNode)
	hdlrNode := child(mdia, "hdlr")
	hdlr := boxOf[*mp4.HdlrBox](hdlrNode)
	stbl := child(child(mdia, "minf"), "stbl")
	s := Section{Title: "Track", Key: trak.Key}
	var id uint32
	if tkhd != nil {
		id = tkhd.TrackID
		s.Title = fmt.Sprintf("Track %d", id)
	}
	if hdlr != nil {
		kind := trackKinds[hdlr.HandlerType]
		if kind == "" {
			kind = hdlr.HandlerType
		}
		s.Title += ": " + kind
		handler := hdlr.HandlerType
		if name := handlerName(hdlr); name != "" {
			handler += fmt.Sprintf(" %q", name)
		}
		s.add("handler", handler, hdlrNode)
	}
	if tkhd != nil {
		s.add("flags", trackFlags(tkhd.Flags), tkhdNode)
	}
	if mdhd != nil {
		lang := mdhd.GetLanguage()
		if mdhd.Language < 0x400 {
			lang = fmt.Sprintf("mac:%d", mdhd.Language) // a QuickTime Macintosh language code
		}
		s.add("language", lang, mdhdNode)
	}
	if refs := chapterOf[id]; len(refs) > 0 {
		what := "chapter titles"
		if !isTextTrack(trak) {
			what = "chapter images"
		}
		s.add("chapters", what+" of "+trackList(refs), trak)
	}
	for _, c := range kids(child(trak, "tref")) {
		if ref := boxOf[*mp4.TrefTypeBox](c); ref != nil {
			s.add("reference", ref.Name+" to "+trackList(ref.TrackIDs), c)
		}
	}
	addSampleEntries(&s, child(stbl, "stsd"), tkhd)
	stats := tableStats(stbl)
	if frag := frags[id]; frag != nil && frag.samples > 0 {
		stats = frag
	}
	var timescale uint32
	if mdhd != nil {
		timescale = mdhd.Timescale
		dur := mdhd.Duration
		if dur == 0 && stats != nil {
			dur = stats.duration
		}
		s.add("duration", ticksText(dur, timescale, mdhd.Version == 0), mdhdNode)
	}
	addSamples(&s, stats, timescale, stbl)
	addEditList(&s, child(child(trak, "edts"), "elst"), moov, timescale)
	addUserData(&s, child(trak, "udta"))
	return s
}

// fragmentTrackSection returns the samples of a track in a media segment without moov, whose time scale is not
// known.
func fragmentTrackSection(id uint32, st *sampleStats) Section {
	s := Section{Title: fmt.Sprintf("Track %d (fragments only)", id)}
	addSamples(&s, st, 0, nil)
	return s
}

// trackFlags returns the flags of a track header as text.
func trackFlags(flags uint32) string {
	var out []string
	for _, f := range []struct {
		mask uint32
		name string
	}{{0x1, "enabled"}, {0x2, "in movie"}, {0x4, "in preview"}, {0x8, "size is aspect ratio"}} {
		if flags&f.mask != 0 {
			out = append(out, f.name)
		}
	}
	if flags&0x1 == 0 {
		out = append([]string{"disabled"}, out...)
	}
	return strings.Join(out, ", ")
}

// trackList returns track IDs as text, such as "track 1" or "tracks 1, 2".
func trackList(ids []uint32) string {
	var out []string
	for _, id := range ids {
		out = append(out, fmt.Sprint(id))
	}
	if len(ids) == 1 {
		return "track " + out[0]
	}
	return "tracks " + strings.Join(out, ", ")
}

// addSampleEntries adds the type, codec and details of the sample entries in stsd to s.
func addSampleEntries(s *Section, stsd *boxview.Node, tkhd *mp4.TkhdBox) {
	entries := kids(stsd)
	for i, e := range entries {
		name := "sample entry"
		if len(entries) > 1 {
			name = fmt.Sprintf("sample entry %d", i+1)
		}
		s.add(name, e.Name(), e)
		s.add("codec", codecString(e.Box), e)
		switch b := e.Box.(type) {
		case *mp4.VisualSampleEntryBox:
			s.add("video", videoDetails(b, tkhd), e)
			s.add("colour", colour(b), e)
			if b.Sinf != nil {
				s.add("encryption", encryption(b.Sinf), e)
			}
		case *mp4.AudioSampleEntryBox:
			s.add("audio", audioDetails(b), e)
			if b.Sinf != nil {
				s.add("encryption", encryption(b.Sinf), e)
			}
		case *mp4.TimecodeSampleEntryBox:
			s.add("timecode", timecodeDetails(b), e)
		case *mp4.TextSampleEntryBox:
			if b.Ftab != nil && len(b.Ftab.Fonts) > 0 && b.Ftab.Fonts[0].Name != "" {
				s.add("font", b.Ftab.Fonts[0].Name, e)
			}
		}
	}
}

// codecString returns the RFC 6381 codecs parameter of a sample entry, such as avc1.64001F or mp4a.40.2, or the
// type of the sample entry if there is no more to it here.
func codecString(entry mp4.Box) string {
	if entry == nil {
		return ""
	}
	typ := entry.Type()
	switch e := entry.(type) {
	case *mp4.VisualSampleEntryBox:
		if e.Sinf != nil && e.Sinf.Frma != nil {
			typ = e.Sinf.Frma.DataFormat
		}
		switch {
		case e.DoViConfig != nil && (typ == "dvh1" || typ == "dvhe" || typ == "dva1" || typ == "dvav"):
			return fmt.Sprintf("%s.%02d.%02d", typ, e.DoViConfig.DVProfile, e.DoViConfig.DVLevel)
		case e.AvcC != nil && len(e.AvcC.SPSnalus) > 0:
			if sps, err := avc.ParseSPSNALUnit(e.AvcC.SPSnalus[0], false); err == nil {
				return avc.CodecString(typ, sps)
			}
		case e.HvcC != nil:
			if spss := e.HvcC.GetNalusForType(hevc.NALU_SPS); len(spss) > 0 {
				if sps, err := hevc.ParseSPSNALUnit(spss[0]); err == nil {
					return hevc.CodecString(typ, sps)
				}
			}
		case e.Av1C != nil:
			return e.Av1C.CodecString(typ)
		case e.VppC != nil:
			return fmt.Sprintf("%s.%02d.%02d.%02d", typ, e.VppC.Profile, e.VppC.Level, e.VppC.BitDepth)
		}
	case *mp4.AudioSampleEntryBox:
		if e.Sinf != nil && e.Sinf.Frma != nil {
			typ = e.Sinf.Frma.DataFormat
		}
		if e.Esds != nil && e.Esds.DecConfigDescriptor != nil {
			dc := e.Esds.DecConfigDescriptor
			if dc.ObjectType == 0x40 && dc.DecSpecificInfo != nil {
				hdr, err := aac.DecodeAudioSpecificConfigHeader(bytes.NewReader(dc.DecSpecificInfo.DecConfig))
				if err == nil {
					return fmt.Sprintf("%s.40.%d", typ, hdr.ObjectType)
				}
			}
			return fmt.Sprintf("%s.%02X", typ, dc.ObjectType)
		}
	}
	return typ
}

// videoDetails returns the size of the video, its pixel aspect ratio and the name of the compressor.
func videoDetails(e *mp4.VisualSampleEntryBox, tkhd *mp4.TkhdBox) string {
	out := []string{fmt.Sprintf("%dx%d", e.Width, e.Height)}
	if e.Pasp != nil && e.Pasp.HSpacing != e.Pasp.VSpacing {
		out = append(out, fmt.Sprintf("pixel aspect %d:%d", e.Pasp.HSpacing, e.Pasp.VSpacing))
	}
	if tkhd != nil {
		w, h := uint32(tkhd.Width)>>16, uint32(tkhd.Height)>>16
		if (w != 0 || h != 0) && (w != uint32(e.Width) || h != uint32(e.Height)) {
			out = append(out, fmt.Sprintf("shown as %dx%d", w, h))
		}
	}
	if name := printableName(e.CompressorName); name != "" {
		out = append(out, fmt.Sprintf("compressor %q", name))
	}
	return strings.Join(out, ", ")
}

// Names of common code points of ITU-T H.273 for the colour of video.
var (
	colourPrimaries = map[uint16]string{1: "BT.709", 5: "BT.601 625", 6: "BT.601 525", 9: "BT.2020", 12: "P3-D65"}
	transferNames   = map[uint16]string{1: "BT.709", 13: "sRGB", 14: "BT.2020", 16: "PQ", 18: "HLG"}
	matrixNames     = map[uint16]string{0: "identity", 1: "BT.709", 5: "BT.601 625", 6: "BT.601 525", 9: "BT.2020"}
)

// colour returns the colour primaries, transfer and matrix of the colr box of a visual sample entry, and which
// HDR metadata boxes it has, or "".
func colour(e *mp4.VisualSampleEntryBox) string {
	var out []string
	for _, c := range e.Children {
		colr, ok := c.(*mp4.ColrBox)
		if !ok {
			continue
		}
		switch colr.ColorType {
		case "nclx", "nclc":
			name := func(names map[uint16]string, v uint16) string {
				if n := names[v]; n != "" {
					return fmt.Sprintf("%d (%s)", v, n)
				}
				return fmt.Sprint(v)
			}
			text := fmt.Sprintf("primaries %s, transfer %s, matrix %s", name(colourPrimaries, colr.ColorPrimaries),
				name(transferNames, colr.TransferCharacteristics), name(matrixNames, colr.MatrixCoefficients))
			if colr.ColorType == "nclx" && colr.FullRangeFlag {
				text += ", full range"
			}
			out = append(out, text)
		case "rICC", "prof":
			out = append(out, fmt.Sprintf("ICC profile of %d bytes", len(colr.ICCProfile)))
		}
	}
	var hdr []string
	if e.Mdcv != nil || e.SmDm != nil {
		hdr = append(hdr, "mastering display")
	}
	if e.Clli != nil || e.CoLL != nil {
		hdr = append(hdr, "content light level")
	}
	if len(hdr) > 0 {
		out = append(out, "HDR metadata: "+strings.Join(hdr, ", "))
	}
	return strings.Join(out, "; ")
}

// audioDetails returns the codec, sampling rate and channels of the audio.
func audioDetails(e *mp4.AudioSampleEntryBox) string {
	rate, channels, bits := float64(e.SampleRate), int(e.ChannelCount), int(e.SampleSize)
	if v2 := e.QuickTimeV2; v2 != nil {
		rate, channels, bits = v2.AudioSampleRate, int(v2.NumAudioChannels), int(v2.ConstBitsPerChannel)
	}
	if e.Esds != nil && e.Esds.DecConfigDescriptor != nil && e.Esds.DecConfigDescriptor.DecSpecificInfo != nil {
		config := e.Esds.DecConfigDescriptor.DecSpecificInfo.DecConfig
		if asc, err := aac.DecodeAudioSpecificConfig(bytes.NewReader(config)); err == nil {
			name := aacConfigName(asc)
			if asc.SBRPresentFlag && asc.ExtensionFrequency != 0 {
				return fmt.Sprintf("%s, %d Hz (core %d Hz), %s", name, asc.ExtensionFrequency,
					asc.SamplingFrequency, plural(channels, "channel"))
			}
			return fmt.Sprintf("%s, %d Hz, %s", name, asc.SamplingFrequency, plural(channels, "channel"))
		}
		if hdr, err := aac.DecodeAudioSpecificConfigHeader(bytes.NewReader(config)); err == nil {
			return fmt.Sprintf("%s, %d Hz, %s", aacName(hdr.ObjectType), hdr.SamplingFrequency,
				plural(channels, "channel"))
		}
	}
	out := fmt.Sprintf("%g Hz, %s", rate, plural(channels, "channel"))
	if bits > 0 {
		out += fmt.Sprintf(", %d bits", bits)
	}
	return out
}

// aacConfigName returns the name of the AAC configuration in asc. An AAC LC config that signals SBR, and maybe PS,
// in a sync extension is named as the object type of explicit signalling, HE-AAC or HE-AACv2.
func aacConfigName(asc *aac.AudioSpecificConfig) string {
	if asc.ObjectType == aac.AAClc && asc.SBRPresentFlag {
		if asc.PSPresentFlag {
			return aacName(aac.HEAACv2)
		}
		return aacName(aac.HEAACv1)
	}
	return aacName(asc.ObjectType)
}

// aacName returns the name of an AAC audio object type.
func aacName(objectType byte) string {
	if name := parts.AACObjectTypeName(objectType); name != "" {
		return name
	}
	return fmt.Sprintf("audio object type %d", objectType)
}

// timecodeDetails returns the frame rate and flags of a timecode.
func timecodeDetails(e *mp4.TimecodeSampleEntryBox) string {
	out := fmt.Sprintf("%d frames per second", e.NumberOfFrames)
	if e.FrameDuration != 0 {
		out += fmt.Sprintf(" (frame duration %d at time scale %d)", e.FrameDuration, e.TimeScale)
	}
	if e.Flags&mp4.TimecodeFlagDropFrame != 0 {
		out += ", drop frame"
	}
	return out
}

// encryption returns the scheme, key ID and pattern of an encrypted sample entry.
func encryption(sinf *mp4.SinfBox) string {
	var out []string
	if sinf.Schm != nil {
		out = append(out, "scheme "+sinf.Schm.SchemeType)
	}
	if sinf.Frma != nil {
		out = append(out, "original format "+sinf.Frma.DataFormat)
	}
	if sinf.Schi != nil && sinf.Schi.Tenc != nil {
		tenc := sinf.Schi.Tenc
		out = append(out, "default KID "+tenc.DefaultKID.String())
		if tenc.DefaultCryptByteBlock != 0 || tenc.DefaultSkipByteBlock != 0 {
			out = append(out, fmt.Sprintf("pattern %d:%d", tenc.DefaultCryptByteBlock, tenc.DefaultSkipByteBlock))
		}
		if tenc.DefaultPerSampleIVSize == 0 && len(tenc.DefaultConstantIV) > 0 {
			out = append(out, fmt.Sprintf("constant IV of %d bytes", len(tenc.DefaultConstantIV)))
		} else if tenc.DefaultPerSampleIVSize > 0 {
			out = append(out, fmt.Sprintf("IV of %d bytes per sample", tenc.DefaultPerSampleIVSize))
		}
	}
	return strings.Join(out, ", ")
}

// tableStats returns the samples of the sample table stbl, or nil if it has no stsz box.
func tableStats(stbl *boxview.Node) *sampleStats {
	stsz := boxOf[*mp4.StszBox](child(stbl, "stsz"))
	if stsz == nil {
		return nil
	}
	st := &sampleStats{}
	if stts := boxOf[*mp4.SttsBox](child(stbl, "stts")); stts != nil {
		for i, count := range stts.SampleCount {
			if i < len(stts.SampleTimeDelta) && count > 0 {
				st.addDur(stts.SampleTimeDelta[i], uint64(count))
				st.samples += uint64(count)
			}
		}
	}
	st.samples = uint64(stsz.GetNrSamples())
	if st.samples > 0 {
		if total, err := stsz.GetTotalSampleSize(1, uint32(st.samples)); err == nil {
			st.bytes = total
		}
	}
	st.sync = st.samples
	if stss := boxOf[*mp4.StssBox](child(stbl, "stss")); stss != nil {
		st.sync = uint64(stss.EntryCount())
	}
	return st
}

// fragmentStats returns the samples of the track fragments in the moof boxes of t, by track ID, with the
// defaults of the trex boxes in moov, which may be nil.
func fragmentStats(t *boxview.Tree, moov *boxview.Node) map[uint32]*sampleStats {
	trex := map[uint32]*mp4.TrexBox{}
	for _, n := range kids(child(moov, "mvex")) {
		if b := boxOf[*mp4.TrexBox](n); b != nil {
			trex[b.TrackID] = b
		}
	}
	stats := map[uint32]*sampleStats{}
	for _, moof := range t.Boxes {
		if moof.Type != "moof" {
			continue
		}
		for _, traf := range children(moof, "traf") {
			tfhd := boxOf[*mp4.TfhdBox](child(traf, "tfhd"))
			if tfhd == nil {
				continue
			}
			st := stats[tfhd.TrackID]
			if st == nil {
				st = &sampleStats{}
				stats[tfhd.TrackID] = st
			}
			st.fragments++
			defaults := trex[tfhd.TrackID]
			if defaults == nil {
				defaults = &mp4.TrexBox{}
			}
			for _, n := range children(traf, "trun") {
				if trun := boxOf[*mp4.TrunBox](n); trun != nil {
					addTrun(st, trun, tfhd, defaults)
				}
			}
		}
	}
	return stats
}

// addTrun adds the samples of trun to st, with the defaults of tfhd and trex.
func addTrun(st *sampleStats, trun *mp4.TrunBox, tfhd *mp4.TfhdBox, trex *mp4.TrexBox) {
	dur, size, flags := trex.DefaultSampleDuration, trex.DefaultSampleSize, trex.DefaultSampleFlags
	if tfhd.HasDefaultSampleDuration() {
		dur = tfhd.DefaultSampleDuration
	}
	if tfhd.HasDefaultSampleSize() {
		size = tfhd.DefaultSampleSize
	}
	if tfhd.HasDefaultSampleFlags() {
		flags = tfhd.DefaultSampleFlags
	}
	firstFlags, hasFirst := trun.FirstSampleFlags()
	for i, smp := range trun.Samples {
		d, sz, f := dur, size, flags
		if trun.HasSampleDuration() {
			d = smp.Dur
		}
		if trun.HasSampleSize() {
			sz = smp.Size
		}
		switch {
		case i == 0 && hasFirst:
			f = firstFlags
		case trun.HasSampleFlags():
			f = smp.Flags
		}
		st.addDur(d, 1)
		st.samples++
		st.bytes += uint64(sz)
		if f&0x10000 == 0 { // sample_is_non_sync_sample is not set
			st.sync++
		}
	}
}

// addSamples adds the number of samples, their rate and sync samples, and the bitrate of st to s. timescale is
// the media time scale, or 0 if it is not known.
func addSamples(s *Section, st *sampleStats, timescale uint32, stbl *boxview.Node) {
	if st == nil {
		return
	}
	var secs float64
	if timescale > 0 {
		secs = seconds(st.duration, timescale).Seconds()
	}
	out := []string{fmt.Sprint(st.samples)}
	if secs > 0 && st.samples > 1 {
		out = append(out, fmt.Sprintf("%.6g per second", float64(st.samples)/secs))
	}
	if st.sampleDur != 0 {
		out = append(out, "each "+plural(int(st.sampleDur), "tick"))
	}
	switch {
	case st.samples == 0:
	case st.sync == st.samples:
		out = append(out, "all sync")
	default:
		out = append(out, fmt.Sprintf("%d sync", st.sync))
	}
	if st.fragments > 0 {
		out = append(out, "in "+plural(st.fragments, "track fragment"))
	}
	s.add("samples", strings.Join(out, ", "), child(stbl, "stsz"))
	if timescale == 0 {
		s.add("sample duration", fmt.Sprintf("%d ticks of an unknown time scale", st.duration), nil)
	}
	if secs > 0 && st.bytes > 0 {
		bps := float64(st.bytes) * 8 / secs
		rate := fmt.Sprintf("%.1f kb/s", bps/1000)
		switch {
		case bps < 1000:
			rate = fmt.Sprintf("%.0f b/s", bps)
		case bps >= 10e6:
			rate = fmt.Sprintf("%.1f Mb/s", bps/1e6)
		}
		s.add("bitrate", fmt.Sprintf("%s, %d bytes", rate, st.bytes), nil)
	} else if st.bytes > 0 {
		s.add("bytes", fmt.Sprint(st.bytes), nil)
	}
}

// addEditList adds the entries of the elst box of n to s, with segment durations in the time scale of the mvhd
// box of moov and media times in timescale.
func addEditList(s *Section, n, moov *boxview.Node, timescale uint32) {
	elst := boxOf[*mp4.ElstBox](n)
	if elst == nil {
		return
	}
	var movieScale uint32
	if mvhd := boxOf[*mp4.MvhdBox](child(moov, "mvhd")); mvhd != nil {
		movieScale = mvhd.Timescale
	}
	var out []string
	for i, e := range elst.Entries {
		if i == 3 {
			out = append(out, fmt.Sprintf("and %d more", len(elst.Entries)-3))
			break
		}
		dur := fmt.Sprint(e.SegmentDuration)
		if movieScale > 0 {
			dur = seconds(e.SegmentDuration, movieScale).Round(time.Millisecond).String()
		}
		if e.MediaTime < 0 {
			out = append(out, "empty for "+dur)
			continue
		}
		start := fmt.Sprint(e.MediaTime)
		if timescale > 0 {
			start += " (" + seconds(uint64(e.MediaTime), timescale).Round(time.Microsecond).String() + ")"
		}
		out = append(out, fmt.Sprintf("from media time %s for %s", start, dur))
	}
	s.add("edit list", strings.Join(out, "; "), n)
}

// sortedIDs returns the track IDs of stats in order.
func sortedIDs(stats map[uint32]*sampleStats) []uint32 {
	var ids []uint32
	for id := range stats {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// kids returns the children of n, or nil if n is nil.
func kids(n *boxview.Node) []*boxview.Node {
	if n == nil {
		return nil
	}
	return n.Children
}
