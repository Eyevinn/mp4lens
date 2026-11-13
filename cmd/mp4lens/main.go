// Command mp4lens shows the boxes of an MP4 file in the terminal, with their positions, fields and bytes,
// and searches them. With -find, it prints the boxes that match a search instead, with -summary an overview
// of the file, and with -extract it writes the bytes of boxes.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Eyevinn/mp4lens/internal"
	"github.com/Eyevinn/mp4lens/internal/boxview"
	"github.com/Eyevinn/mp4lens/internal/summary"
	"github.com/Eyevinn/mp4lens/internal/tui"
)

const usage = `mp4lens shows the boxes of an MP4 (ISOBMFF) file in the terminal: the box tree with the position and size
of each box, and the fields and bytes of the selected box. Boxes that cannot be decoded are marked, and the
boxes around them are still shown. Press ? in the viewer for the keys.

Usage: mp4lens [options] file

Options:
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "mp4lens:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mp4lens", flag.ContinueOnError)
	fs.SetOutput(stderr)
	levels := fs.String("levels", "", "detail levels of the fields, as for mp4ff-info, such as all:1 for all samples")
	query := fs.String("search", "", "search to run at the start")
	find := fs.String("find", "", "print the boxes that match this search, and exit")
	summarize := fs.Bool("summary", false, "print an overview of the file: brands, movie, metadata, chapters and "+
		"tracks, and exit")
	extract := fs.String("extract", "", "write the bytes of these boxes, such as moof[0] or ftyp,moov, one after "+
		"the other, to standard output or the -o file, print their paths, and exit")
	out := fs.String("o", "", "the file that -extract writes to")
	version := fs.Bool("version", false, "print the version and exit")
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nSearch:\n%s\n", boxview.QueryHelp)
		fmt.Fprintf(stderr, "\nBox names:\n%s\n", boxview.RefHelp)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *version {
		fmt.Fprintln(stdout, "mp4lens", internal.Version())
		return nil
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("give one file")
	}
	if *out != "" && *extract == "" {
		return errors.New("-o is for -extract")
	}
	path := fs.Arg(0)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if *find != "" {
		return findBoxes(stdout, f, fi.Size(), *levels, *find)
	}
	if *extract != "" {
		return extractBoxes(stdout, stderr, *out, f, fi.Size(), *extract)
	}
	if *summarize {
		_, err := fmt.Fprint(stdout, summary.Of(boxview.Decode(f, fi.Size(), *levels), f).Text())
		return err
	}
	m := tui.New(f, fi.Size(), tui.Options{Name: filepath.Base(path), Levels: *levels, Query: *query})
	_, err = tea.NewProgram(m).Run()
	return err
}

// findBoxes prints the path, position and size of the boxes that match query, and the positions of the
// bytes that hex terms found.
func findBoxes(w io.Writer, r io.ReaderAt, size int64, levels, query string) error {
	q, err := boxview.ParseQuery(query)
	if err != nil {
		return err
	}
	t := boxview.Decode(r, size, levels)
	res, err := t.Search(q, r)
	if err != nil {
		return err
	}
	for _, n := range res.Nodes {
		line := fmt.Sprintf("%s pos=%d size=%d", t.RefPath(n), n.Pos, n.Size)
		for _, h := range res.Hits[n] {
			line += fmt.Sprintf(" hit=%d", h.Pos)
		}
		if n.Err != nil {
			line += " error: " + n.Err.Error()
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	if res.Capped {
		fmt.Fprintf(w, "the byte search stopped after %d hits\n", boxview.MaxHits)
	}
	if len(res.Nodes) == 0 {
		return errors.New("no boxes match")
	}
	return nil
}

// extractBoxes writes the bytes of the boxes named in refs, separated by commas, to the file out, or to stdout if
// out is empty, and their paths, positions and sizes to stderr. It writes the bytes in the file of a box that goes
// beyond the end of the file, and then fails.
func extractBoxes(stdout, stderr io.Writer, out string, r io.ReaderAt, size int64, refs string) (err error) {
	t := boxview.Decode(r, size, "")
	var nodes []*boxview.Node
	for _, ref := range strings.Split(refs, ",") {
		n, err := t.Find(strings.TrimSpace(ref))
		if err != nil {
			return err
		}
		nodes = append(nodes, n)
	}
	w := stdout
	if out != "" {
		f, err := os.Create(out)
		if err != nil {
			return err
		}
		defer func() {
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}()
		w = f
	} else if isTerminal(stdout) {
		return errors.New("will not write the bytes of boxes to a terminal; use -o or a pipe")
	}
	var truncated []string
	for _, n := range nodes {
		fmt.Fprintf(stderr, "%s pos=%d size=%d\n", t.RefPath(n), n.Pos, n.Size)
		end := min(n.End(), uint64(size))
		if _, err := io.Copy(w, io.NewSectionReader(r, int64(n.Pos), int64(end-n.Pos))); err != nil {
			return err
		}
		if end < n.End() {
			truncated = append(truncated, fmt.Sprintf("%s has %d of its %d bytes in the file", t.Ref(n), end-n.Pos,
				n.Size))
		}
	}
	if len(truncated) > 0 {
		return fmt.Errorf("wrote what the file has, but %s", strings.Join(truncated, ", and "))
	}
	return nil
}

// isTerminal tells if w is a terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
