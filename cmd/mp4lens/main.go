// Command mp4lens shows the boxes of an MP4 file in the terminal, with their positions, fields and bytes,
// and searches them. With -find, it prints the boxes that match a search instead, and with -summary an overview
// of the file.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	version := fs.Bool("version", false, "print the version and exit")
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nSearch:\n%s\n", boxview.QueryHelp)
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
		line := fmt.Sprintf("%s pos=%d size=%d", n.Path(), n.Pos, n.Size)
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
