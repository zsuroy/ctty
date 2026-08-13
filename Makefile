.PHONY: build build-local test clean release snapshot

# Version can be overridden via environment variable or command line
VERSION ?= dev

# Go build flags
LDFLAGS := -s -w -X github.com/zsuroy/ctty/cmd.AppVersion=$(VERSION)

# Build with specific version
build:
	@mkdir -p dist
	go build -ldflags="$(LDFLAGS)" -o dist/ctty .

# Build with git version
build-local: VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
build-local: build

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -rf dist

# Release with GoReleaser (requires tag)
release:
	@if [ -z "$(shell git tag --points-at HEAD)" ]; then \
		echo "Error: No git tag found at current commit. Create a tag first with: git tag vX.Y.Z"; \
		exit 1; \
	fi
	goreleaser release --clean

# Build snapshot (without tag)
snapshot:
	goreleaser release --snapshot --clean

# Check GoReleaser config
release-check:
	goreleaser check

# Run GoReleaser in dry-run mode
release-dry-run:
	goreleaser release --snapshot --skip=publish --clean
