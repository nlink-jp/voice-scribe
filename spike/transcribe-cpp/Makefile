# Reproduce the ADR-0007 measurements. Nothing here ships.
#
#   make deps      clone + build + install transcribe.cpp (~2 min)
#   make build     link the Go harness against it
#   make fixture   generate the two-speaker Japanese WAV
#   make diarize   Sortformer over the fixture, from Go
#   make kotoba    convert kotoba-whisper-v2.0 to GGUF, quantize, transcribe
#   make all       everything above, in order

REV      := 856d7c10a1a864b900e066b7c9801edf373f5148   # main, 2026-08-07
UPSTREAM := https://github.com/handy-computer/transcribe.cpp.git

T        := third_party/transcribe.cpp
INSTALL  := $(T)/build/install
FIXTURE  := fixture/meeting_ja_2spk.wav
MODELS   := models

SORTFORMER_URL := https://huggingface.co/handy-computer/diar_streaming_sortformer_4spk-v2.1-gguf/resolve/main/diar_streaming_sortformer_4spk-v2.1-Q8_0.gguf

.PHONY: all deps build fixture diarize kotoba clean

all: deps build fixture diarize kotoba

# Pinned to a commit, not to `main`: the diarization work this spike is about
# is unreleased, so there is no tag to pin to. See ADR-0007.
$(T):
	mkdir -p third_party
	git clone $(UPSTREAM) $(T)
	cd $(T) && git checkout $(REV)

deps: $(T)
	cd $(T) && cmake -B build -DCMAKE_BUILD_TYPE=Release \
		-DTRANSCRIBE_BUILD_TESTS=OFF -DTRANSCRIBE_BUILD_TOOLS=ON \
		-DGGML_METAL=ON -DGGML_METAL_EMBED_LIBRARY=ON
	cd $(T) && cmake --build build --config Release -j
	cd $(T) && cmake --install build --prefix "$$PWD/build/install"
	@echo "--- link manifest (the LDFLAGS in main.go come from here) ---"
	@cat $(INSTALL)/lib/transcribe-link.json

build:
	CGO_ENABLED=1 go build -o tcspike .

fixture $(FIXTURE):
	./make-fixture.sh fixture

$(MODELS)/sortformer-Q8_0.gguf:
	mkdir -p $(MODELS)
	curl -fL --retry 3 -o $@ "$(SORTFORMER_URL)"

diarize: $(MODELS)/sortformer-Q8_0.gguf $(FIXTURE)
	./tcspike $(MODELS)/sortformer-Q8_0.gguf $(FIXTURE)

# convert-whisper.py rejects unknown variant slugs, so the allowlist entry is
# applied as a patch rather than carried as a fork. Upstreamable as-is: the
# dict already holds a community fine-tune (breeze-asr-25).
kotoba: $(FIXTURE)
	cd $(T) && git apply --check ../../kotoba-variant.patch 2>/dev/null && \
		git apply ../../kotoba-variant.patch || echo "patch already applied"
	cd $(T) && uv run --project scripts/envs/whisper \
		scripts/convert-whisper.py kotoba-tech/kotoba-whisper-v2.0
	cd $(T) && ./build/bin/transcribe-quantize \
		models/kotoba-whisper-v2.0/kotoba-whisper-v2.0-BF16.gguf \
		models/kotoba-whisper-v2.0/kotoba-whisper-v2.0-Q5_0.gguf --quant Q5_0
	./tcspike $(T)/models/kotoba-whisper-v2.0/kotoba-whisper-v2.0-Q5_0.gguf $(FIXTURE)

clean:
	rm -rf tcspike fixture $(MODELS)
