// Command build makes the mp4lens web site in web/site, ready to serve as static files, such as on GitHub Pages:
//
//   - index.html, the page, with the list of examples and the first example and its box tree embedded
//   - mp4lens.wasm, the decoder in web/wasm compiled to WebAssembly
//   - wasm_exec.js, the JavaScript support code of the Go WebAssembly port
//   - worker.js, the Web Worker that runs the decoder
//   - examples/, the example files
//
// Run it from the root of the repository:
//
//	go run ./web/build
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Eyevinn/mp4lens/internal/boxview"
)

const (
	srcDir      = "web"
	siteDir     = "web/site"
	testdataDir = "testdata"
)

// example is a file in the list of examples. The first example has its data and box tree embedded, so
// that the page shows it before the decoder has loaded.
type example struct {
	ID   string        `json:"id"`
	Name string        `json:"name"`
	File string        `json:"file"`
	Data string        `json:"data,omitempty"`
	Tree *boxview.Tree `json:"tree,omitempty"`
}

// exampleFile is an example made from a file in testdata, changed by corrupt if not nil.
type exampleFile struct {
	id, name, file, source string
	corrupt                func(data []byte) error
}

var exampleFiles = []exampleFile{
	{"corrupt", "Init segment with a corrupt stsz", "init_corrupt_stsz.mp4", "init.mp4", corruptStsz},
	{"progressive", "Progressive MP4 with video and audio", "prog_8s.mp4", "prog_8s.mp4", nil},
	{"init", "AVC init segment", "init.mp4", "init.mp4", nil},
	{"segment", "Media segment", "1.m4s", "1.m4s", nil},
	{"cenc-init", "Encrypted init segment (cenc)", "init_cenc.cmfv", "init_cenc.cmfv", nil},
	{"encrypted", "Encrypted media segment", "moof_enc.m4s", "moof_enc.m4s", nil},
	{"av1", "AV1 init segment", "av1_init.mp4", "av1_init.mp4", nil},
	{"hevc", "HEVC init segment with an SEI message in hvcC", "hvc1_init.mp4", "hvc1_init.mp4", nil},
	{"mov", "QuickTime movie with AAC audio", "qt_h264_aac.mov", "qt_h264_aac.mov", nil},
	{"mov-pcm", "QuickTime movie with 5.1 PCM audio", "qt_h264_lpcm.mov", "qt_h264_lpcm.mov", nil},
	{"truncated", "Truncated init segment", "init_truncated.mp4", "init_truncated.mp4", nil},
}

// corruptStsz sets the sample count of the stsz box to 1, without adding a sample size.
func corruptStsz(data []byte) error {
	pos := bytes.Index(data, []byte("stsz")) - 4
	if pos < 0 || pos+20 > len(data) {
		return errors.New("no stsz box")
	}
	binary.BigEndian.PutUint32(data[pos+16:], 1)
	return nil
}

func main() {
	if err := build(); err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
}

func build() error {
	if err := os.RemoveAll(siteDir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(siteDir, "examples"), 0o755); err != nil {
		return err
	}
	wasm := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(siteDir, "mp4lens.wasm"), "./web/wasm")
	wasm.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	wasm.Stdout, wasm.Stderr = os.Stdout, os.Stderr
	if err := wasm.Run(); err != nil {
		return fmt.Errorf("build wasm: %w", err)
	}
	if err := copyWasmExec(); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(srcDir, "worker.js"), filepath.Join(siteDir, "worker.js")); err != nil {
		return err
	}
	examples, err := makeExamples()
	if err != nil {
		return err
	}
	exJSON, err := json.Marshal(examples)
	if err != nil {
		return err
	}
	page, err := os.ReadFile(filepath.Join(srcDir, "index.src.html"))
	if err != nil {
		return err
	}
	out := string(page)
	for placeholder, value := range map[string]string{
		"/*__EXAMPLES__*/": "const EXAMPLES = " + strings.ReplaceAll(string(exJSON), "</", `<\/`) + ";",
		"__QUERY_HELP__":   html.EscapeString(boxview.QueryHelp),
		"__MAX_HITS__":     strconv.Itoa(boxview.MaxHits),
	} {
		if !strings.Contains(out, placeholder) {
			return fmt.Errorf("index.src.html has no %s", placeholder)
		}
		out = strings.Replace(out, placeholder, value, 1)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte(out), 0o644); err != nil {
		return err
	}
	// GitHub Pages serves the files as they are, without Jekyll.
	if err := os.WriteFile(filepath.Join(siteDir, ".nojekyll"), nil, 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", siteDir)
	return nil
}

// makeExamples writes the example files to the site and returns the list of examples.
func makeExamples() ([]example, error) {
	var examples []example
	for i, ef := range exampleFiles {
		data, err := os.ReadFile(filepath.Join(testdataDir, ef.source))
		if err != nil {
			return nil, err
		}
		if ef.corrupt != nil {
			if err := ef.corrupt(data); err != nil {
				return nil, fmt.Errorf("%s: %w", ef.id, err)
			}
		}
		if err := os.WriteFile(filepath.Join(siteDir, "examples", ef.file), data, 0o644); err != nil {
			return nil, err
		}
		ex := example{ID: ef.id, Name: ef.name, File: ef.file}
		if i == 0 {
			ex.Data = base64.StdEncoding.EncodeToString(data)
			ex.Tree = boxview.Decode(bytes.NewReader(data), int64(len(data)), "")
		}
		examples = append(examples, ex)
	}
	return examples, nil
}

// copyWasmExec copies wasm_exec.js from the Go installation, where Go 1.24 and later keep it in lib/wasm.
func copyWasmExec() error {
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return fmt.Errorf("go env GOROOT: %w", err)
	}
	root := strings.TrimSpace(string(out))
	for _, dir := range []string{"lib/wasm", "misc/wasm"} {
		src := filepath.Join(root, dir, "wasm_exec.js")
		if _, err := os.Stat(src); err == nil {
			return copyFile(src, filepath.Join(siteDir, "wasm_exec.js"))
		}
	}
	return errors.New("wasm_exec.js not found in GOROOT")
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
