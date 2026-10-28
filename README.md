# mp4lens

Look inside MP4 files: the box tree with the position and size of every box, the fields and bytes of each box,
and the boxes that cannot be decoded, with the boxes around them still shown. Search for box types, field
values, file positions and bytes.

mp4lens comes as a web page and as a terminal viewer. Both decode with [mp4ff][mp4ff], and both show the same
things and take the same searches.

- **Web**: <https://eyevinn.github.io/mp4lens/>. Open or drop a file. It is decoded in the browser by mp4ff
  compiled to WebAssembly; nothing is uploaded. Large files are read in slices, so they are not loaded into
  memory as a whole.
- **Terminal**: `mp4lens file.mp4`

## Install

```sh
go install github.com/Eyevinn/mp4lens/cmd/mp4lens@latest
```

## The terminal viewer

```sh
mp4lens file.mp4                   # browse the boxes
mp4lens -levels all:1 file.mp4     # with all fields, such as every sample of trun and stsz
mp4lens -search is:error file.mp4  # start at the boxes that cannot be decoded
mp4lens -find type:emsg file.m4s   # print the matching boxes and exit
```

The tree is on the left and the selected box on the right: its path, position, size, header, version and
flags, decode error, fields and bytes, with the header and the child box headers marked in the bytes.
Press `?` for the keys. The main ones:

| Key | Action |
|-----|--------|
| `↑` `↓` `←` `→` (or `k` `j` `h` `l`) | move, open and close boxes |
| `/` | search |
| `n` `N` | next and previous matching box |
| `f` | show only the matching boxes |
| `e` `E` | next and previous box that cannot be decoded |
| `a` | switch between the main fields and all fields |
| `tab` | switch between the tree and the box pane |
| `q` | quit |

## Search

A box matches a query when it matches all its terms:

| Term | Matches |
|------|---------|
| `text` | text in the box type, its name or its fields, ignoring case |
| `type:trun,tfdt` | boxes of these types |
| `path:trak/mdia` | boxes whose path, such as `moov/trak/mdia/mdhd`, contains this |
| `key=value` | boxes with this field value, such as `trackID=2` or `handlerType=soun` |
| `@1234`, `@0x4d2` | the innermost box that holds the byte at this position |
| `size>1000` | boxes larger than 1000 bytes; also `<`, `<=`, `>=` and `=` |
| `hex:000001b6` | the innermost boxes that hold these bytes, which are marked in the bytes view |
| `is:error` | boxes that cannot be decoded |
| `is:unknown` | box types that mp4ff does not know |
| `is:truncated` | boxes that continue past the end of the file |

Put `-` before a term to match the boxes that do not match it, and quotes around text with spaces:
`"sample size" -type:stz2`.

## How it works

The [`boxtree`][boxtree] package of mp4ff decodes the box tree. It keeps the position of every box, and when a
box cannot be decoded, it decodes its children one by one, so the error is reported on the smallest box that
fails while the boxes around it are still decoded. The fields are the `Info` output of the mp4ff boxes, the
same as `mp4ff-info` prints.

This repository holds the viewers:

- `internal/boxview`: the tree for viewing (parents, keys, long box names, fields), JSON for the web page,
  and the search
- `internal/tui` and `cmd/mp4lens`: the terminal viewer, built with [Bubble Tea][bubbletea]
- `web`: the web page; `web/wasm` is the decoder for `GOOS=js GOARCH=wasm`, which runs in a Web Worker

## Building the web page

```sh
go run ./web/build                            # makes web/site
python3 -m http.server -d web/site 8000       # serve it, since Web Workers do not run from file: URLs
```

Then open <http://localhost:8000/>. The GitHub Pages workflow builds and publishes the same site on every
push to `main`.

## Status

mp4lens needs the `boxtree` package of mp4ff, which is not yet in a released mp4ff version. Until it is,
`go.mod` replaces mp4ff with a local checkout in `../mp4ff-boxtree` that has the package.

The example files in `testdata` come from the mp4ff test data.

## License

MIT, see [LICENSE](LICENSE).

[mp4ff]: https://github.com/Eyevinn/mp4ff
[boxtree]: https://github.com/Eyevinn/mp4ff/tree/master/boxtree
[bubbletea]: https://github.com/charmbracelet/bubbletea
