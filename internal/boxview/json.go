package boxview

import (
	"encoding/json"
	"fmt"
)

// MarshalJSON writes the tree as the web page reads it.
func (t *Tree) MarshalJSON() ([]byte, error) {
	type jsonTree struct {
		Size      int64   `json:"size"`
		ReadErr   string  `json:"readError,omitempty"`
		QuickTime string  `json:"quickTime,omitempty"`
		Boxes     []*Node `json:"boxes"`
	}
	j := jsonTree{Size: t.Size, QuickTime: t.QuickTime, Boxes: t.Boxes}
	if j.Boxes == nil {
		j.Boxes = []*Node{}
	}
	if t.ReadErr != nil {
		j.ReadErr = t.ReadErr.Error()
	}
	return json.Marshal(j)
}

// MarshalJSON writes the box or part with its name, as given by Name, its long name, and its fields.
func (n *Node) MarshalJSON() ([]byte, error) {
	type jsonNode struct {
		Type      string  `json:"type"`
		LongName  string  `json:"name,omitempty"`
		Pos       uint64  `json:"pos"`
		Size      uint64  `json:"size"`
		HdrLen    int     `json:"hdrLen"`
		Version   *int    `json:"version,omitempty"`
		Flags     string  `json:"flags,omitempty"`
		Fields    []Field `json:"fields,omitempty"`
		Err       string  `json:"error,omitempty"`
		ChildErr  bool    `json:"childError,omitempty"`
		Unknown   bool    `json:"unknown,omitempty"`
		Part      bool    `json:"part,omitempty"`
		QuickTime string  `json:"quickTime,omitempty"`
		Children  []*Node `json:"children,omitempty"`
	}
	j := jsonNode{Type: n.Name(), LongName: n.LongName(), Pos: n.Pos, Size: n.Size, HdrLen: n.HdrLen,
		Fields: n.Fields(), ChildErr: n.ChildErr, Unknown: n.Unknown(), Part: n.Part != nil, QuickTime: n.QuickTime,
		Children: n.Children}
	if v, f, ok := n.VersionFlags(); ok {
		j.Version, j.Flags = &v, fmt.Sprintf("%06x", f)
	}
	if n.Err != nil {
		j.Err = n.Err.Error()
	}
	return json.Marshal(j)
}
