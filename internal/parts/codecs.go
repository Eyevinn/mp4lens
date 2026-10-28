package parts

import (
	"fmt"

	"github.com/Eyevinn/mp4ff/av1"
	"github.com/Eyevinn/mp4ff/avc"
	"github.com/Eyevinn/mp4ff/hevc"
	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/Eyevinn/mp4ff/sei"
	"github.com/Eyevinn/mp4ff/vvc"
)

// The parameter sets of avcC, with the SPS and PPS parsed.
func (f *finder) avcC(b *mp4.AvcCBox) []*Part {
	var out []*Part
	spsMap := map[uint32]*avc.SPS{}
	for _, nalu := range b.SPSnalus {
		sps, err := avc.ParseSPSNALUnit(nalu, true)
		if sps != nil {
			spsMap[sps.ParameterID] = sps
		}
		out = append(out, f.part("SPS", "Sequence Parameter Set", nalu, sps, err))
	}
	for _, nalu := range b.PPSnalus {
		pps, err := avc.ParsePPSNALUnit(nalu, spsMap)
		out = append(out, f.part("PPS", "Picture Parameter Set", nalu, pps, err))
	}
	return out
}

// hevcNames are the names of the HEVC NAL unit types in hvcC boxes.
var hevcNames = map[hevc.NaluType][2]string{
	hevc.NALU_VPS:        {"VPS", "Video Parameter Set"},
	hevc.NALU_SPS:        {"SPS", "Sequence Parameter Set"},
	hevc.NALU_PPS:        {"PPS", "Picture Parameter Set"},
	hevc.NALU_SEI_PREFIX: {"SEI", "Prefix Supplemental Enhancement Information"},
	hevc.NALU_SEI_SUFFIX: {"SEI", "Suffix Supplemental Enhancement Information"},
}

// The NAL units of the arrays of hvcC, with the parameter sets and SEI messages parsed.
func (f *finder) hvcC(b *mp4.HvcCBox) []*Part {
	var out []*Part
	vpsMap := map[byte]*hevc.VPS{}
	spsMap := map[uint32]*hevc.SPS{}
	var lastSPS *hevc.SPS
	for _, arr := range b.NaluArrays {
		for _, nalu := range arr.Nalus {
			if len(nalu) < 2 {
				continue
			}
			typ := hevc.GetNaluType(nalu[0])
			names, ok := hevcNames[typ]
			if !ok {
				names = [2]string{typ.String(), fmt.Sprintf("NAL unit type %d", typ)}
			}
			var p *Part
			switch typ {
			case hevc.NALU_VPS:
				vps, err := hevc.ParseVPSNALUnit(nalu)
				if vps != nil {
					vpsMap[vps.VpsID] = vps
				}
				p = f.part(names[0], names[1], nalu, vps, err)
			case hevc.NALU_SPS:
				sps, err := hevc.ParseSPSNALUnitWithVPS(nalu, vpsMap)
				if sps != nil {
					spsMap[uint32(sps.SpsID)] = sps
					lastSPS = sps
				}
				p = f.part(names[0], names[1], nalu, sps, err)
			case hevc.NALU_PPS:
				pps, err := hevc.ParsePPSNALUnit(nalu, spsMap)
				p = f.part(names[0], names[1], nalu, pps, err)
			case hevc.NALU_SEI_PREFIX, hevc.NALU_SEI_SUFFIX:
				msgs, err := hevc.ParseSEINalu(nalu, lastSPS)
				p = f.part(names[0], names[1], nalu, nil, err)
				p.Children = seiParts(msgs, p)
			default:
				p = f.part(names[0], names[1], nalu, nil, nil)
			}
			p.Info = append(p.Info, field(0, "nal_unit_type", fmt.Sprintf("%d (%s)", typ, typ)))
			out = append(out, p)
		}
	}
	return out
}

// seiParts returns a part for each SEI message in the NAL unit of part p. The messages have no positions of their
// own, since emulation prevention bytes are removed before they are parsed, so they get the position of p.
func seiParts(msgs []sei.SEIMessage, p *Part) []*Part {
	var out []*Part
	for _, m := range msgs {
		c := &Part{Name: fmt.Sprintf("SEI %d", m.Type()), LongName: seiName(m.Type()), Pos: p.Pos, Size: p.Size}
		c.Info = append([]string{fmt.Sprintf("[SEI %d] payloadSize=%d", m.Type(), m.Size())},
			field(0, "payloadType", m.Type()), field(0, "payloadSize", m.Size()))
		c.Info = append(c.Info, structLines(m, 0)...)
		c.Info = append(c.Info, field(0, "message", m.String()))
		out = append(out, c)
	}
	return out
}

// seiName returns the name of an SEI payload type.
func seiName(t uint) string {
	switch t {
	case sei.SEIPicTimingType:
		return "Picture timing"
	case sei.SEIUserDataRegisteredITUtT35Type:
		return "User data registered by ITU-T T.35"
	case sei.SEIUserDataUnregisteredType:
		return "User data unregistered"
	case sei.SEIRecoveryPointType:
		return "Recovery point"
	case sei.SEITimeCodeType:
		return "Time code"
	case sei.SEIMasteringDisplayColourVolumeType:
		return "Mastering display colour volume"
	case sei.SEIContentLightLevelInformationType:
		return "Content light level information"
	case sei.SEIAlternativeTransferCharacteristicsType:
		return "Alternative transfer characteristics"
	}
	return fmt.Sprintf("SEI payload type %d", t)
}

// The NAL units of the arrays of vvcC, with their headers.
func (f *finder) vvcC(b *mp4.VvcCBox) []*Part {
	var out []*Part
	for _, arr := range b.NaluArrays {
		for _, nalu := range arr.Nalus {
			hdr, err := vvc.ParseNaluHeader(nalu)
			name := hdr.NaluType.String()
			p := f.part(name, fmt.Sprintf("VVC NAL unit type %d", hdr.NaluType), nalu, hdr, err)
			out = append(out, p)
		}
	}
	return out
}

// The OBUs of av1C, with the sequence header parsed.
func (f *finder) av1C(b *mp4.Av1CBox) []*Part {
	if len(b.ConfigOBUs) == 0 {
		return nil
	}
	obus, err := av1.SplitOBUs(b.ConfigOBUs)
	if err != nil {
		p := f.part("configOBUs", "Configuration OBUs", b.ConfigOBUs, nil, err)
		return []*Part{p}
	}
	var out []*Part
	start, _ := f.find(b.ConfigOBUs)
	off := start
	for _, o := range obus {
		size := uint64(o.Size())
		p := &Part{Name: o.Header.Type.String(), LongName: "AV1 OBU", Pos: off, Size: size}
		p.Info = []string{fmt.Sprintf("[%s] size=%d", p.Name, size)}
		p.Info = append(p.Info, structLines(o.Header, 0)...)
		if o.Header.Type == av1.OBUSequenceHeader {
			p.LongName = "AV1 Sequence Header OBU"
			sh, err := av1.ParseSequenceHeader(o.Payload)
			p.Err = err
			p.Info = append(p.Info, structLines(sh, 0)...)
		} else {
			p.Info = append(p.Info, field(0, "payload", hexText(o.Payload, maxHexBytes)))
		}
		out = append(out, p)
		off += size
	}
	return out
}
