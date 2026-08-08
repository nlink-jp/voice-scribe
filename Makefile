BINARY  := voice-scribe
DIST    := dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/nlink-jp/voice-scribe/cmd.Version=$(VERSION)

# The real transcription runtime (whisper.cpp + ggml + Metal) is linked in only
# under the `cgo_whisper` build tag. `make build-engine` builds the whisper.cpp
# static libraries first (via `deps`, which needs cmake + the Metal toolchain),
# then links them into a single Go binary. Without the tag, `make build`
# produces a binary whose engine reports ErrNoRuntime — useful for scaffold work
# and toolchain-less machines.

W_DIR   := third_party/whisper.cpp
W_BUILD := $(W_DIR)/build
W_LIB   := $(W_BUILD)/src/libwhisper.a

# whisper.cpp's Go bindings carry their own go.mod, so `./...` already skips
# them and PKGS is currently identical to it. Kept as insurance: image-forge
# needed exactly this filter because sd.cpp's vendored swig bindings were *not*
# a nested module, and an upstream that drops its go.mod would break `go test`
# here the same way.
PKGS := $(shell go list ./... 2>/dev/null | grep -v '/third_party/')

# macOS Developer ID signing / notarization (see scripts/).
CODESIGN_IDENTITY ?= Developer ID Application
NOTARY_PROFILE    ?= nlink-jp-notary

.PHONY: build build-engine build-all package deps test fmt vet clean clean-deps

## build: scaffold binary (no transcription runtime)
build:
	@mkdir -p $(DIST)
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) .

## build-engine: full binary with the statically-linked whisper.cpp runtime
build-engine: deps
	@mkdir -p $(DIST)
	CGO_ENABLED=1 go build -tags cgo_whisper -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) .

## deps: build whisper.cpp into static libraries (Metal backend, embedded shaders)
deps: $(W_LIB)

# GGML_METAL_EMBED_LIBRARY=ON compiles the Metal shader source into the library,
# which is what keeps the release a single self-contained binary — without it the
# runtime looks for a ggml-metal.metal file next to the executable at load time.
$(W_LIB):
	cmake -S $(W_DIR) -B $(W_BUILD) -DCMAKE_BUILD_TYPE=Release \
		-DBUILD_SHARED_LIBS=OFF -DGGML_METAL=ON -DGGML_METAL_EMBED_LIBRARY=ON \
		-DWHISPER_BUILD_EXAMPLES=OFF -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_SERVER=OFF
	cmake --build $(W_BUILD) --config Release -j

## build-all: release binary. This tool is CGO + Metal, so Apple Silicon
## (darwin/arm64) ONLY — cross-compilation is impossible (Metal has no
## Linux/Windows/amd64 target), a deliberate scope decision (see the RFP).
build-all: build-engine
	@scripts/codesign-darwin.sh $(DIST)/$(BINARY) "$(CODESIGN_IDENTITY)" "$(BINARY)"

## package: signed + notarized release zip (canonical binary + README + LICENSE
## inside, per the org Release Archive Standard).
package: build-all
	@cd $(DIST) && cp ../README.md ../LICENSE . \
		&& zip -j $(BINARY)-$(VERSION)-darwin-arm64.zip $(BINARY) README.md LICENSE \
		&& rm -f README.md LICENSE
	@scripts/notarize-darwin.sh $(DIST)/$(BINARY)-$(VERSION)-darwin-arm64.zip "$(NOTARY_PROFILE)"

test:
	go test $(PKGS)

## test-engine: run the suite against the real runtime (needs `make deps`)
test-engine: deps
	go test -tags cgo_whisper $(PKGS)

fmt:
	go fmt $(PKGS)

vet:
	go vet $(PKGS)

clean:
	rm -rf $(DIST)

## clean-deps: remove the whisper.cpp build tree
clean-deps:
	rm -rf $(W_BUILD)

BREW_KIND := formula
BREW_DESC := Local speech-to-text engine and MCP server
include scripts/release-brew.mk
