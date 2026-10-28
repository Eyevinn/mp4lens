# mp4lens

Look inside MP4 and QuickTime files: the box tree with the position and size of every box, the fields and bytes
of each box, where in the bytes each field is, the structures inside boxes such as parameter sets, SEI messages
and descriptors, and the boxes that cannot be decoded, with the boxes around them still shown. What is specific
to QuickTime is marked. Search for box types, field values, file positions and bytes.

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
flags, decode error, fields, byte layout and bytes, with the header and the child box headers marked in the bytes.
Press `?` for the keys. The main ones:

| Key | Action |
|-----|--------|
| `↑` `↓` `←` `→` (or `k` `j` `h` `l`) | move, open and close boxes |
| `/` | search |
| `n` `N` | next and previous matching box |
| `f` | show only the matching boxes |
| `e` `E` | next and previous box that cannot be decoded |
| `a` | switch between the main fields and all fields |
| `m` | show more bytes and layout fields |
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
| `is:quicktime` | boxes, parts and fields that are specific to QuickTime |
| `is:part` | structures inside boxes, such as `SPS` or `ES_Descriptor` |

Put `-` before a term to match the boxes that do not match it, and quotes around text with spaces:
`"sample size" -type:stz2`.

## Fields, layout and parts

Each box shows two views of its content:

- **Fields**: the `Info` output of the mp4ff box, the same as `mp4ff-info` prints, with values interpreted,
  such as times and fixed-point numbers
- **Layout**: every read that the mp4ff decoder of the box makes, with its file position, offset in the box,
  size, kind (such as `u32` or `bytes`), name and raw value. On the web page, pointing at a layout row or a
  field marks its bytes, and pointing at a byte shows its field

The names in the layout come from the source of the mp4ff decoders: a read gets the name of the field that it
sets, such as `TrackID` for `t.TrackID = sr.ReadUint32()`. `go generate ./internal/layout` makes the table of
names from the mp4ff version in `go.mod`, and CI checks that it is up to date. Repeated reads are numbered, as
`Matrix[2]` or `SampleSize #3`.

**Parts** are the structures that boxes hold as bytes, decoded with mp4ff and shown as children of the box:

| Box | Parts |
|-----|-------|
| `avcC` | SPS and PPS, with all their fields |
| `hvcC` | VPS, SPS, PPS and SEI NAL units, with the SEI messages |
| `vvcC` | NAL units with their headers |
| `av1C` | OBUs, with the sequence header |
| `esds` | `ES_Descriptor`, `DecoderConfigDescriptor`, `DecoderSpecificInfo` with the AudioSpecificConfig, and `SLConfigDescriptor` |
| `©nam` and other QuickTime user data atoms | the text items, with their language |

## QuickTime

The QuickTime File Format and ISOBMFF share most boxes, but QuickTime adds atoms and gives meaning to bytes
that ISOBMFF reserves. mp4lens marks them with `QT` and says what they are:

- files with the `qt` brand, or without `ftyp`
- QuickTime atoms, such as `wide`, `wave`, `chan`, `keys`, `fiel`, the terminator atom, and the text atoms in `udta`
- the QuickTime meta atom without version and flags, and the terminator at the end of `udta`
- fields that overlay reserved or pre_defined bytes of ISOBMFF, such as the vendor and quality of sample
  entries, the component type of `hdlr` and the times of `mvhd`, when they are not 0
- the fields of version 1 and 2 sound descriptions, which ISOBMFF does not have

## How it works

The [`boxtree`][boxtree] package of mp4ff decodes the box tree. It keeps the position of every box, and when a
box cannot be decoded, it decodes its children one by one, so the error is reported on the smallest box that
fails while the boxes around it are still decoded. The fields are the `Info` output of the mp4ff boxes, the
same as `mp4ff-info` prints.

This repository holds the viewers:

- `internal/boxview`: the tree for viewing (parents, keys, long box names, fields, parts, QuickTime notes),
  JSON for the web page, and the search
- `internal/layout`: the byte layout of a box, from a `bits.SliceReader` that records the reads of the mp4ff
  decoder, with the names from the decoder source in `names_gen.go`
- `internal/parts`: the structures inside boxes
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

The example files in `testdata` come from the mp4ff test data, except the `qt_*.mov` files, which are made with
ffmpeg from its test sources.

`MP4LENS_CORPUS=files.txt go test -count=1 ./internal/boxview -run TestCorpus -v` decodes the files listed in
`files.txt`, finds the layout of every box, and reports panics and how many layout bytes have names.

## License

MIT, see [LICENSE](LICENSE).

[mp4ff]: https://github.com/Eyevinn/mp4ff
[boxtree]: https://github.com/Eyevinn/mp4ff/tree/master/boxtree
[bubbletea]: https://github.com/charmbracelet/bubbletea
