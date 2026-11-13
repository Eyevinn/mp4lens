# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-11-13

### Added

- `-extract` writes the bytes of boxes and parts, named by type and order number, such as `moof[0]`, `moof[-1]`
  or `stsd[1]`, to standard output or the `-o` file
- `pssh` parts: the decoded Widevine data, and the PlayReady Object with its records; the `pssh` layout has the
  Common Encryption field names and shows the system ID and key IDs as UUIDs

### Changed

- `-find` and the terminal viewer show the path of a box with the order numbers of repeated boxes, such as
  `moov/trak[1]/mdia[1]/minf[1]/stbl[1]/stsd[1]`
- mp4ff v0.63.0: the fields show the component fields of QuickTime handlers, `gmin` graphics modes, `lpcm`
  flags, `colr` code point names and the keys of `ilst` items
- QuickTime handlers: `componentType` and `componentName` are marked QT and linked to their bytes, and the layout
  and the summary decode the counted-string name

## [0.2.0] - 2026-11-12

### Added

- Terminal viewer: mark a layout field to see its bytes, as on the web page, with `j` and `k` in the box pane or
  by pointing with the mouse at a layout row, field or byte; search hits in the bytes are now yellow

## [0.1.0] - 2026-11-10

First release.

### Added

- `mp4lens` terminal viewer: box tree with positions and decode errors, fields and bytes of the selected box,
  and search; `-find` prints the matching boxes instead
- Web page that decodes files in the browser with WebAssembly, with the same views and search
- File summary with brands, duration, metadata, chapters, and the codec string, samples and bitrate of each
  track, in both viewers; `-summary` prints it
- Byte layout of each box: position, size, type, name and value of every field that mp4ff reads,
  linked to the bytes view on the web page
- Parts: the parameter sets and SEI messages in avcC, hvcC and vvcC, the OBUs in av1C, and the descriptors
  and AudioSpecificConfig in esds
- The AudioSpecificConfig of AAC Main and xHE-AAC (USAC) is decoded in full, and the summary names AAC LC that
  signals SBR or PS in a sync extension HE-AAC or HE-AACv2
- QuickTime boxes, fields and files are marked; search with `is:quicktime` and `is:part`
- The layout gives the fields of common boxes their names in the specification, says what they are, and splits
  version and flags, sample flags and other packed values
- Plain container boxes are marked as containers, without fields or layout
- Logo and favicon with the Go gopher; the web page has the favicon and a link preview image
- `make serve` and `go run ./web/build -serve localhost:8000` build the web page and serve it locally
- The web page shows its version in the footer, as `mp4lens -version` does, with the date of the release

<!-- At a release, add a section for the version and date below [Unreleased] with its
changes, add a link for the version that compares it with the previous one, for example
[0.2.0]: https://github.com/Eyevinn/mp4lens/compare/v0.1.0...v0.2.0
and point [Unreleased] at the new version. -->

[Unreleased]: https://github.com/Eyevinn/mp4lens/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/Eyevinn/mp4lens/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/Eyevinn/mp4lens/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/Eyevinn/mp4lens/releases/tag/v0.1.0
