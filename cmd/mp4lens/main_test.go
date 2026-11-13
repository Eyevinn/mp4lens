package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtract(t *testing.T) {
	frag, err := os.ReadFile("../../testdata/bbb5s_aac_sidx.mp4")
	if err != nil {
		t.Fatal(err)
	}
	truncated, err := os.ReadFile("../../testdata/init_truncated.mp4")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		desc       string
		args       []string
		wantOut    []byte
		wantStderr string
		err        string
	}{
		{
			desc:       "first moof",
			args:       []string{"-extract", "moof[0]", "../../testdata/bbb5s_aac_sidx.mp4"},
			wantOut:    frag[883 : 883+456],
			wantStderr: "moof[0] pos=883 size=456\n",
		},
		{
			desc:       "init segment",
			args:       []string{"-extract", "ftyp, moov", "../../testdata/bbb5s_aac_sidx.mp4"},
			wantOut:    append(bytes.Clone(frag[:32]), frag[90:90+697]...),
			wantStderr: "ftyp pos=0 size=32\nmoov pos=90 size=697\n",
		},
		{
			desc:       "box in a track",
			args:       []string{"-extract", "stsd[1]", "../../testdata/prog_8s.mp4"},
			wantStderr: "moov/trak[1]/mdia[1]/minf[1]/stbl[1]/stsd[1] pos=2853 size=175\n",
		},
		{
			desc:       "truncated box",
			args:       []string{"-extract", "moov", "../../testdata/init_truncated.mp4"},
			wantOut:    truncated[69:],
			wantStderr: "moov pos=69 size=646\n",
			err:        "wrote what the file has, but moov has 31 of its 646 bytes in the file",
		},
		{
			desc: "several of a type",
			args: []string{"-extract", "moof", "../../testdata/bbb5s_aac_sidx.mp4"},
			err:  "moof: there are 3 of them",
		},
		{
			desc: "-o without -extract",
			args: []string{"-o", "out.mp4", "../../testdata/bbb5s_aac_sidx.mp4"},
			err:  "-o is for -extract",
		},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(c.args, &stdout, &stderr)
			if c.err == "" && err != nil || c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)) {
				t.Fatalf("got error %v, want %q", err, c.err)
			}
			if c.wantOut != nil && !bytes.Equal(stdout.Bytes(), c.wantOut) {
				t.Errorf("wrote %d bytes, want %d", stdout.Len(), len(c.wantOut))
			}
			if stderr.String() != c.wantStderr {
				t.Errorf("stderr is %q, want %q", stderr.String(), c.wantStderr)
			}
		})
	}
}

func TestExtractToFile(t *testing.T) {
	data, err := os.ReadFile("../../testdata/bbb5s_aac_sidx.mp4")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "last.m4s")
	var stdout, stderr bytes.Buffer
	err = run([]string{"-extract", "moof[-1],mdat[-1]", "-o", out, "../../testdata/bbb5s_aac_sidx.mp4"}, &stdout,
		&stderr)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data[64855:]) || stdout.Len() != 0 {
		t.Errorf("wrote %d bytes to the file and %d to stdout, want %d and 0", len(got), stdout.Len(),
			len(data)-64855)
	}
	if want := "moof[2] pos=64855 size=276\nmdat[2] pos=65131 size=16050\n"; stderr.String() != want {
		t.Errorf("stderr is %q, want %q", stderr.String(), want)
	}

	// A box that is not there leaves no file.
	out = filepath.Join(t.TempDir(), "none.m4s")
	if err := run([]string{"-extract", "moof[9]", "-o", out, "../../testdata/bbb5s_aac_sidx.mp4"}, &stdout,
		&stderr); err == nil {
		t.Error("no error for moof[9]")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("%s exists after an error", out)
	}
}

func TestFindPrintsOrderNumbers(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-find", "type:stsd", "../../testdata/prog_8s.mp4"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	want := "moov/trak[0]/mdia[0]/minf[0]/stbl[0]/stsd[0] pos=424 size=91\n" +
		"moov/trak[1]/mdia[1]/minf[1]/stbl[1]/stsd[1] pos=2853 size=175\n"
	if stdout.String() != want {
		t.Errorf("got\n%s\nwant\n%s", stdout.String(), want)
	}
}
