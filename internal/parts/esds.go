package parts

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/Eyevinn/mp4ff/aac"
)

// Descriptor tags of ISO/IEC 14496-1.
const (
	tagESDescriptor    = 0x03
	tagDecoderConfig   = 0x04
	tagDecSpecificInfo = 0x05
	tagSLConfig        = 0x06
)

var descriptorNames = map[byte][2]string{
	tagESDescriptor:    {"ES_Descriptor", "Elementary Stream Descriptor"},
	tagDecoderConfig:   {"DecoderConfigDescriptor", "Decoder Configuration Descriptor"},
	tagDecSpecificInfo: {"DecoderSpecificInfo", "Decoder Specific Information"},
	tagSLConfig:        {"SLConfigDescriptor", "Sync Layer Configuration Descriptor"},
}

// objectTypes are names of objectTypeIndication values.
var objectTypes = map[byte]string{
	0x20: "Visual ISO/IEC 14496-2", 0x21: "AVC ISO/IEC 14496-10", 0x40: "Audio ISO/IEC 14496-3 (AAC)",
	0x66: "MPEG-2 AAC Main", 0x67: "MPEG-2 AAC LC", 0x68: "MPEG-2 AAC SSR", 0x69: "Audio ISO/IEC 13818-3",
	0x6b: "Audio ISO/IEC 11172-3 (MP3)", 0xa5: "AC-3", 0xa6: "E-AC-3", 0xa9: "DTS", 0xad: "Opus",
}

// streamTypes are names of streamType values.
var streamTypes = map[byte]string{1: "ObjectDescriptor", 2: "ClockReference", 3: "SceneDescription", 4: "Visual",
	5: "Audio", 6: "MPEG-7", 7: "IPMP", 8: "ObjectContentInfo", 9: "MPEG-J", 0x0a: "Interaction", 0x0b: "IPMPTool"}

// aacObjectTypes are names of the audio object types of an AudioSpecificConfig.
var aacObjectTypes = map[byte]string{1: "AAC Main", 2: "AAC LC", 3: "AAC SSR", 4: "AAC LTP", 5: "SBR (HE-AAC)",
	29: "PS (HE-AACv2)", 42: "xHE-AAC (USAC)"}

// esdsParts returns the descriptors in the esds box in data, which starts at position pos in the file.
func esdsParts(data []byte, pos uint64) []*Part {
	const payloadStart = 12 // box header, version and flags
	if len(data) <= payloadStart {
		return nil
	}
	return descriptors(data, payloadStart, len(data), pos, 0)
}

// descriptors returns the descriptors in data[start:end]. objectType is the objectTypeIndication of the
// enclosing DecoderConfigDescriptor, or 0.
func descriptors(data []byte, start, end int, pos uint64, objectType byte) []*Part {
	var out []*Part
	for off := start; off < end; {
		tag := data[off]
		size, n := 0, 1
		for ; n <= 4 && off+n < end; n++ {
			b := data[off+n]
			size = size<<7 | int(b&0x7f)
			if b&0x80 == 0 {
				break
			}
		}
		hdr := 1 + n
		names, ok := descriptorNames[tag]
		if !ok {
			names = [2]string{fmt.Sprintf("Descriptor %d", tag), fmt.Sprintf("Descriptor with tag %d", tag)}
		}
		p := &Part{Name: names[0], LongName: names[1], Pos: pos + uint64(off), Size: uint64(min(hdr+size, end-off))}
		p.Info = []string{fmt.Sprintf("[%s] size=%d", names[0], p.Size), field(0, "tag", tag), field(0, "size", size)}
		out = append(out, p)
		bodyStart, bodyEnd := off+hdr, off+hdr+size
		if bodyEnd > end {
			p.Err = fmt.Errorf("size %d goes beyond the end of the enclosing structure", size)
			bodyEnd = end
		}
		if bodyStart <= bodyEnd {
			body := data[bodyStart:bodyEnd]
			switch tag {
			case tagESDescriptor:
				subStart := esFields(p, body)
				if subStart >= 0 {
					p.Children = descriptors(data, bodyStart+subStart, bodyEnd, pos, 0)
				}
			case tagDecoderConfig:
				if len(body) >= 13 {
					objType := body[0]
					p.Info = append(p.Info,
						field(0, "objectTypeIndication", fmt.Sprintf("0x%02x %s", objType, objectTypes[objType])),
						field(0, "streamType", fmt.Sprintf("%d %s", body[1]>>2, streamTypes[body[1]>>2])),
						field(0, "upStream", body[1]>>1&1),
						field(0, "bufferSizeDB", uint32(body[2])<<16|uint32(body[3])<<8|uint32(body[4])),
						field(0, "maxBitrate", binary.BigEndian.Uint32(body[5:])),
						field(0, "avgBitrate", binary.BigEndian.Uint32(body[9:])))
					p.Children = descriptors(data, bodyStart+13, bodyEnd, pos, objType)
				} else {
					p.Err = fmt.Errorf("%d bytes are too few for a DecoderConfigDescriptor", len(body))
				}
			case tagDecSpecificInfo:
				p.Info = append(p.Info, field(0, "data", hexText(body, maxHexBytes)))
				if objectType == 0x40 || objectType >= 0x66 && objectType <= 0x68 {
					p.Children = []*Part{audioSpecificConfig(body, pos+uint64(bodyStart))}
				}
			case tagSLConfig:
				if len(body) > 0 {
					p.Info = append(p.Info, field(0, "predefined", body[0]))
				}
			default:
				p.Info = append(p.Info, field(0, "data", hexText(body, maxHexBytes)))
			}
		}
		off = bodyEnd
	}
	return out
}

// esFields adds the fields of an ES_Descriptor with payload body to p, and returns where the descriptors inside
// start in body, or -1.
func esFields(p *Part, body []byte) int {
	if len(body) < 3 {
		p.Err = fmt.Errorf("%d bytes are too few for an ES_Descriptor", len(body))
		return -1
	}
	flags := body[2]
	p.Info = append(p.Info, field(0, "ES_ID", binary.BigEndian.Uint16(body)),
		field(0, "streamDependenceFlag", flags>>7), field(0, "URL_Flag", flags>>6&1),
		field(0, "OCRstreamFlag", flags>>5&1), field(0, "streamPriority", flags&0x1f))
	off := 3
	if flags&0x80 != 0 && off+2 <= len(body) {
		p.Info = append(p.Info, field(0, "dependsOn_ES_ID", binary.BigEndian.Uint16(body[off:])))
		off += 2
	}
	if flags&0x40 != 0 && off < len(body) {
		n := int(body[off])
		end := min(off+1+n, len(body))
		p.Info = append(p.Info, field(0, "URLstring", fmt.Sprintf("%q", body[off+1:end])))
		off = end
	}
	if flags&0x20 != 0 && off+2 <= len(body) {
		p.Info = append(p.Info, field(0, "OCR_ES_Id", binary.BigEndian.Uint16(body[off:])))
		off += 2
	}
	return off
}

// fullASCTypes are the audio object types whose AudioSpecificConfig mp4ff decodes in full. For other types,
// such as USAC, it decodes the leading fields.
var fullASCTypes = map[byte]bool{aac.AAClc: true, aac.HEAACv1: true, aac.HEAACv2: true}

// audioSpecificConfig decodes the AudioSpecificConfig in b, at position pos in the file.
func audioSpecificConfig(b []byte, pos uint64) *Part {
	p := &Part{Name: "AudioSpecificConfig", LongName: "MPEG-4 Audio Specific Config", Pos: pos, Size: uint64(len(b))}
	p.Info = []string{fmt.Sprintf("[AudioSpecificConfig] size=%d", len(b))}
	asc, err := aac.DecodeAudioSpecificConfig(bytes.NewReader(b))
	if err != nil {
		hdr, hdrErr := aac.DecodeAudioSpecificConfigHeader(bytes.NewReader(b))
		if hdrErr != nil {
			p.Err = err
			return p
		}
		p.Info = append(p.Info,
			field(0, "audioObjectType", fmt.Sprintf("%d %s", hdr.ObjectType, aacObjectTypes[hdr.ObjectType])),
			field(0, "samplingFrequency", hdr.SamplingFrequency),
			field(0, "channelConfiguration", hdr.ChannelConfiguration))
		if fullASCTypes[hdr.ObjectType] {
			p.Err = err
		} else {
			p.Info = append(p.Info, field(0, "note", "mp4ff decodes only these leading fields of this object type"))
		}
		return p
	}
	p.Info = append(p.Info,
		field(0, "audioObjectType", fmt.Sprintf("%d %s", asc.ObjectType, aacObjectTypes[asc.ObjectType])),
		field(0, "samplingFrequency", asc.SamplingFrequency),
		field(0, "channelConfiguration", asc.ChannelConfiguration))
	if asc.SBRPresentFlag {
		p.Info = append(p.Info, field(0, "SBRPresentFlag", true), field(0, "extensionSamplingFrequency", asc.ExtensionFrequency))
	}
	if asc.PSPresentFlag {
		p.Info = append(p.Info, field(0, "PSPresentFlag", true))
	}
	return p
}
