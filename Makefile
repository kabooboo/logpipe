.PHONY: build build-all test clean version help

VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD)
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

# Default target
all: build

# Build for current platform
build:
	go build -ldflags "$(LDFLAGS)" -o logpipe main.go

# Build for all platforms
build-all: clean
	mkdir -p dist
	
	# Linux AMD64
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/logpipe-linux-amd64 main.go
	
	# Linux ARM64
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/logpipe-linux-arm64 main.go
	
	# macOS AMD64
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/logpipe-darwin-amd64 main.go
	
	# macOS ARM64 (Apple Silicon)
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/logpipe-darwin-arm64 main.go
	
	# Windows AMD64
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/logpipe-windows-amd64.exe main.go

# Generate checksums
checksums:
	cd dist && sha256sum * > checksums.txt

# Run tests
test:
	go test -v ./...

# Show version information
version:
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT)"
	@echo "Date: $(DATE)"

# Clean build artifacts
clean:
	rm -rf dist/
	rm -f logpipe

# Show help
help:
	@echo "Available targets:"
	@echo "  build      - Build for current platform"
	@echo "  build-all  - Build for all platforms"
	@echo "  test       - Run tests"
	@echo "  checksums  - Generate checksums for dist files"
	@echo "  version    - Show version information"
	@echo "  clean      - Clean build artifacts"
	@echo "  help       - Show this help"