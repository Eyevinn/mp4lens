package boxview

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFind(t *testing.T) {
	frag, _ := decodeFile(t, "bbb5s_aac_sidx.mp4", nil)
	prog, _ := decodeFile(t, "prog_8s.mp4", nil)
	cases := []struct {
		tree    *Tree
		ref     string
		wantPos uint64
		wantRef string // the path with order numbers
		err     string
	}{
		{tree: frag, ref: "moof[0]", wantPos: 883, wantRef: "moof[0]"},
		{tree: frag, ref: "moof[2]", wantPos: 64855, wantRef: "moof[2]"},
		{tree: frag, ref: "moof[-1]", wantPos: 64855, wantRef: "moof[2]"},
		{tree: frag, ref: "mdat[-3]", wantPos: 1339, wantRef: "mdat[0]"},
		{tree: frag, ref: "moov", wantPos: 90, wantRef: "moov"},
		{tree: frag, ref: " sidx ", err: `there is no  sidx  in the file`},
		{tree: prog, ref: "stsd[1]", wantPos: 2853, wantRef: "moov/trak[1]/mdia[1]/minf[1]/stbl[1]/stsd[1]"},
		{tree: prog, ref: "moov/trak/mdia/minf/stbl/stsd[1]", wantPos: 2853,
			wantRef: "moov/trak[1]/mdia[1]/minf[1]/stbl[1]/stsd[1]"},
		{tree: prog, ref: "moov/trak[1]/mdia[1]/minf[1]/stbl[1]/stsd[1]", wantPos: 2853,
			wantRef: "moov/trak[1]/mdia[1]/minf[1]/stbl[1]/stsd[1]"},
		{tree: prog, ref: "SPS", wantPos: 2971, wantRef: "moov/trak[1]/mdia[1]/minf[1]/stbl[1]/stsd[1]/avc1/avcC/SPS"},
		{tree: frag, ref: "moof", err: "there are 3 of them, so give the order number of one, from moof[0] to moof[2]"},
		{tree: frag, ref: "moof[3]", err: "there are 3 of them, from moof[0] to moof[2]"},
		{tree: frag, ref: "moof[-4]", err: "there are 3 of them, from moof[0] to moof[2]"},
		{tree: frag, ref: "moof[first]", err: "the order number is not an integer"},
		{tree: frag, ref: "xyz[0]", err: "there is no xyz in the file"},
		{tree: frag, ref: "", err: `"" does not name a box`},
		{tree: frag, ref: "moov/", err: `"moov/" does not name a box`},
		{tree: prog, ref: "moov/trak[0]/mdia/minf/stbl/stsd[1]",
			err: "the path of stsd[1] is moov/trak[1]/mdia[1]/minf[1]/stbl[1]/stsd[1]"},
		{tree: prog, ref: "moov/trak[0]", wantPos: 157, wantRef: "moov/trak[0]"},
		{tree: prog, ref: "free/trak[0]", err: "the path of trak[0] is moov/trak[0]"},
		{tree: prog, ref: "x/moov", err: "the path of moov is moov"},
	}
	for _, c := range cases {
		t.Run(c.ref, func(t *testing.T) {
			n, err := c.tree.Find(c.ref)
			if c.err != "" {
				if err == nil || !strings.HasSuffix(err.Error(), c.err) {
					t.Errorf("got error %v, want one that ends with %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if n.Pos != c.wantPos || c.tree.RefPath(n) != c.wantRef {
				t.Errorf("got %s at %d, want %s at %d", c.tree.RefPath(n), n.Pos, c.wantRef, c.wantPos)
			}
		})
	}
}

// TestFindAll checks that Find finds every box and part of the test files by its name and by its path.
func TestFindAll(t *testing.T) {
	files, err := filepath.Glob("../../testdata/*")
	if err != nil || len(files) < 5 {
		t.Fatalf("%d test files, %v", len(files), err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			tr, _ := decodeFile(t, filepath.Base(file), nil)
			for _, n := range tr.Nodes {
				for _, ref := range []string{tr.Ref(n), tr.RefPath(n), pathWithRef(tr, n)} {
					if got, err := tr.Find(ref); err != nil || got != n {
						t.Errorf("%s: got %v, %v", ref, got, err)
					}
				}
			}
		})
	}
}

// pathWithRef returns the path of n without order numbers, but for that of n, as moov/trak/mdia/minf/stbl/stsd[1].
func pathWithRef(t *Tree, n *Node) string {
	if n.Parent == nil {
		return t.Ref(n)
	}
	return n.Parent.Path() + "/" + t.Ref(n)
}
