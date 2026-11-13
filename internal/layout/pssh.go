package layout

import (
	"strings"

	"github.com/Eyevinn/mp4ff/mp4"
)

// psshFields writes the system ID and the key IDs of a pssh box as UUIDs, the system ID with the name of its
// protection system. data holds the bytes of the box, which starts at position pos in the file.
func psshFields(b *mp4.PsshBox, fields []Field, data []byte, pos uint64) {
	for i := range fields {
		f := &fields[i]
		start := int(f.Pos - pos)
		if f.Size != 16 || start < 0 || start+16 > len(data) {
			continue
		}
		id := mp4.UUID(data[start : start+16])
		switch {
		case f.Name == "SystemID":
			f.Value = id.String() + " (" + mp4.ProtectionSystemName(b.SystemID) + ")"
		case f.Name == "KIDs" || strings.HasPrefix(f.Name, "KIDs #"):
			f.Value = id.String()
		}
	}
}
