//go:build js && wasm

// Command wasm is the decoder of the mp4lens web page, built for GOOS=js and GOARCH=wasm. It runs in a Web
// Worker and gives JavaScript these functions:
//
//   - mp4lensDecode(source, levels) decodes the box tree of source and returns it as JSON. The source is a
//     Uint8Array, or a Blob, such as a File, that is read in slices with FileReaderSync.
//   - mp4lensSearch(query) searches the last decoded tree and returns the keys of the matching boxes as JSON.
//   - mp4lensLayout(key) returns the byte layout of the box with key in the last decoded tree as JSON.
//   - mp4lensSummary() returns the summary of the last decoded file as JSON.
//   - mp4lensVersion() returns the version of the build, such as "v0.1.0, date: 2026-11-10", as mp4lens -version
//     reports it.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"syscall/js"

	"github.com/Eyevinn/mp4lens/internal"
	"github.com/Eyevinn/mp4lens/internal/boxview"
	"github.com/Eyevinn/mp4lens/internal/layout"
	"github.com/Eyevinn/mp4lens/internal/summary"
)

// blobReader reads a JavaScript Blob, such as a File, with FileReaderSync.
type blobReader struct {
	blob js.Value
	size int64
}

func (b blobReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= b.size {
		return 0, io.EOF
	}
	end := min(off+int64(len(p)), b.size)
	buf := js.Global().Get("FileReaderSync").New().Call("readAsArrayBuffer", b.blob.Call("slice", off, end))
	n := js.CopyBytesToGo(p, js.Global().Get("Uint8Array").New(buf))
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// last is the last decoded file, which mp4lensSearch searches.
var last struct {
	tree *boxview.Tree
	r    io.ReaderAt
}

func decode(_ js.Value, args []js.Value) (ret any) {
	defer func() {
		if r := recover(); r != nil {
			ret = errorJSON("readError", fmt.Sprint("decoder panic: ", r))
		}
	}()
	src, levels := args[0], args[1].String()
	var r io.ReaderAt
	var size int64
	if src.InstanceOf(js.Global().Get("Uint8Array")) {
		data := make([]byte, src.Get("length").Int())
		js.CopyBytesToGo(data, src)
		r, size = bytes.NewReader(data), int64(len(data))
	} else {
		size = int64(src.Get("size").Float())
		r = blobReader{blob: src, size: size}
	}
	t := boxview.Decode(r, size, levels)
	last.tree, last.r = t, r
	out, err := json.Marshal(t)
	if err != nil {
		return errorJSON("readError", err.Error())
	}
	return string(out)
}

type searchResult struct {
	Keys   []string                  `json:"keys"`
	Hits   map[string][]boxview.Span `json:"hits,omitempty"`
	Capped bool                      `json:"capped,omitempty"`
}

func search(_ js.Value, args []js.Value) (ret any) {
	defer func() {
		if r := recover(); r != nil {
			ret = errorJSON("error", fmt.Sprint("search panic: ", r))
		}
	}()
	if last.tree == nil {
		return errorJSON("error", "no file has been decoded")
	}
	q, err := boxview.ParseQuery(args[0].String())
	if err != nil {
		return errorJSON("error", err.Error())
	}
	res, err := last.tree.Search(q, last.r)
	if err != nil {
		return errorJSON("error", err.Error())
	}
	sr := searchResult{Keys: make([]string, 0, len(res.Nodes)), Capped: res.Capped}
	for _, n := range res.Nodes {
		sr.Keys = append(sr.Keys, n.Key)
	}
	if res.Hits != nil {
		sr.Hits = make(map[string][]boxview.Span, len(res.Hits))
		for n, spans := range res.Hits {
			sr.Hits[n.Key] = spans
		}
	}
	out, err := json.Marshal(sr)
	if err != nil {
		return errorJSON("error", err.Error())
	}
	return string(out)
}

type layoutResult struct {
	Fields []layout.Field `json:"fields"`
	Capped bool           `json:"capped,omitempty"`
	Error  string         `json:"error,omitempty"`
	Spec   string         `json:"spec,omitempty"`
}

func boxLayout(_ js.Value, args []js.Value) (ret any) {
	defer func() {
		if r := recover(); r != nil {
			ret = errorJSON("error", fmt.Sprint("layout panic: ", r))
		}
	}()
	if last.tree == nil {
		return errorJSON("error", "no file has been decoded")
	}
	n := last.tree.ByKey(args[0].String())
	if n == nil {
		return errorJSON("error", "no box with key "+args[0].String())
	}
	res := last.tree.Layout(n, last.r)
	lr := layoutResult{Fields: []layout.Field{}}
	if res != nil {
		lr.Capped, lr.Spec = res.Capped, res.Spec
		if res.Fields != nil {
			lr.Fields = res.Fields
		}
		if res.Err != nil && !n.ChildErr {
			lr.Error = res.Err.Error()
		}
	}
	out, err := json.Marshal(lr)
	if err != nil {
		return errorJSON("error", err.Error())
	}
	return string(out)
}

func fileSummary(_ js.Value, _ []js.Value) (ret any) {
	defer func() {
		if r := recover(); r != nil {
			ret = errorJSON("error", fmt.Sprint("summary panic: ", r))
		}
	}()
	if last.tree == nil {
		return errorJSON("error", "no file has been decoded")
	}
	out, err := json.Marshal(summary.Of(last.tree, last.r))
	if err != nil {
		return errorJSON("error", err.Error())
	}
	return string(out)
}

func errorJSON(key, msg string) string {
	out, _ := json.Marshal(map[string]string{key: msg})
	return string(out)
}

func main() {
	js.Global().Set("mp4lensDecode", js.FuncOf(decode))
	js.Global().Set("mp4lensSearch", js.FuncOf(search))
	js.Global().Set("mp4lensLayout", js.FuncOf(boxLayout))
	js.Global().Set("mp4lensSummary", js.FuncOf(fileSummary))
	js.Global().Set("mp4lensVersion", js.FuncOf(func(js.Value, []js.Value) any { return internal.Version() }))
	select {}
}
