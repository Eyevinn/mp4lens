package boxview

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestCorpus decodes the files listed, one path per line, in the file that MP4LENS_CORPUS names, finds the
// layout of every box, and reports panics. It is skipped when MP4LENS_CORPUS is not set.
func TestCorpus(t *testing.T) {
	list := os.Getenv("MP4LENS_CORPUS")
	if list == "" {
		t.Skip("MP4LENS_CORPUS is not set")
	}
	lf, err := os.Open(list)
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	var files, boxes, parts, qt, qtNodes, errs int
	var fieldBytes, namedBytes int
	var slowest time.Duration
	slowFile := ""
	sc := bufio.NewScanner(lf)
	for sc.Scan() {
		path := strings.TrimSpace(sc.Text())
		if path == "" {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		fi, _ := f.Stat()
		start := time.Now()
		func() {
			cur := "decode"
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: panic at %s: %v", path, cur, r)
				}
			}()
			tr := Decode(f, fi.Size(), "")
			files++
			if tr.QuickTime != "" {
				qt++
			}
			errs += len(tr.Errors())
			for _, n := range tr.Nodes {
				if hasQuickTime(n) {
					qtNodes++
					if os.Getenv("MP4LENS_CORPUS_QT") != "" {
						t.Logf("QuickTime: %s %s: %s", path, n.Path(), n.QuickTime)
					}
				}
				if !n.IsBox() {
					parts++
					continue
				}
				boxes++
				cur = n.Path() + fmt.Sprintf("@%d", n.Pos)
				res := tr.Layout(n, f)
				if res == nil {
					continue
				}
				for _, fl := range res.Fields {
					fieldBytes += fl.Size
					if fl.Name != "" {
						namedBytes += fl.Size
					}
				}
			}
		}()
		if d := time.Since(start); d > slowest {
			slowest, slowFile = d, path
		}
		f.Close()
	}
	t.Logf("%d files (%d QuickTime), %d boxes, %d parts, %d QuickTime boxes or parts, %d box errors; "+
		"%d of %d layout bytes named (%.1f%%); slowest %v %s", files, qt, boxes, parts, qtNodes, errs,
		namedBytes, fieldBytes, 100*float64(namedBytes)/float64(max(1, fieldBytes)),
		slowest, slowFile)
}
