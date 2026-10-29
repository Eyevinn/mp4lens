.PHONY: all
all: test check coverage build

.PHONY: build
build: mp4lens

.PHONY: prepare
prepare:
	go mod tidy

# Building ./cmd/mp4lens as a package makes Go embed the version from git, which mp4lens -version reports.
.PHONY: mp4lens
mp4lens:
	go build -o out/$@ ./cmd/$@

# The web page in web/site, as static files.
.PHONY: web
web:
	go run ./web/build

# Builds the web page and serves it on http://localhost:$(PORT)/, since browsers do not run its Web Worker from
# file: URLs.
PORT ?= 8000
.PHONY: serve
serve:
	go run ./web/build -serve localhost:$(PORT)

# The field names of the layout, from the source of the mp4ff version in go.mod.
.PHONY: generate
generate:
	go generate ./internal/layout

.PHONY: test
test: prepare
	go test ./...

.PHONY: coverage
coverage:
	# Ignore (allow) packages without any tests
	go test -coverpkg=./... -coverprofile coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	go tool cover -func coverage.out -o coverage.txt
	tail -1 coverage.txt

# The Python environment for pre-commit is .venv, since go test ./... skips directories that start with a dot.
.venv:
	python3 -m venv .venv
	.venv/bin/pip install --upgrade pip
	.venv/bin/pip install pre-commit==4.7.0

# Installs the pre-commit and the commit-msg hook.
.PHONY: pre-commit-install
pre-commit-install: .venv
	.venv/bin/pre-commit install

.PHONY: pre-commit
pre-commit: .venv
	.venv/bin/pre-commit run --all-files

# codespell runs as a pre-commit hook, with its settings in .codespellrc.
.PHONY: codespell
codespell: .venv
	.venv/bin/pre-commit run codespell --all-files

.PHONY: check
check: prepare pre-commit

# The social preview image for link previews of the repository (upload it in the GitHub settings) and of the
# page: the logo on its background color, in the 1280x640 that GitHub asks for. Needs rsvg-convert and ImageMagick.
images/social-preview.png: images/mp4lens-logo.svg
	rsvg-convert -w 1200 $< | magick - -background '#0f172a' -gravity center -extent 1280x640 -strip $@

.PHONY: clean
clean:
	rm -rf out/ dist/ web/site/ coverage.out coverage.html coverage.txt

.PHONY: install
install:
	go install ./cmd/mp4lens
