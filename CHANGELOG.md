# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
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
- QuickTime boxes, fields and files are marked; search with `is:quicktime` and `is:part`
- The layout gives the fields of common boxes their names in the specification, says what they are, and splits
  version and flags, sample flags and other packed values
- Plain container boxes are marked as containers, without fields or layout
- Logo and favicon with the Go gopher; the web page has the favicon and a link preview image
