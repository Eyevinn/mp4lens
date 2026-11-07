# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `mp4lens` terminal viewer: box tree with positions and decode errors, fields and bytes of the selected box,
  and search; `-find` prints the matching boxes instead
- Web page that decodes files in the browser with WebAssembly, with the same views and search
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

<!-- At a release, rename [Unreleased] to the version and date, add a new
[Unreleased] section, and add a link for the version, for example
[0.1.0]: https://github.com/Eyevinn/mp4lens/releases/tag/v0.1.0
and point [Unreleased] at https://github.com/Eyevinn/mp4lens/compare/v0.1.0...HEAD -->

[Unreleased]: https://github.com/Eyevinn/mp4lens/commits/main
