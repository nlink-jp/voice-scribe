# Reproduce the ADR-0007 measurements. Nothing here ships.
#
#   make deps      clone + build + install transcribe.cpp (~2 min)
#   make build     link the Go harness against it
#   make fixture   generate the two-speaker Japanese WAV
#   make diarize   Sortformer over the fixture, from Go
#   make kotoba    convert kotoba-whisper-v2.0 to GGUF, quantize, transcribe
#   make eval      日本語 CER: 6 モデル × 2 コーパス（数 GB の DL を伴う）
#   make caps      capability matrix（タイムスタンプ・ライセンス・上限）
#   make all       everything above, in order

REV      := 856d7c10a1a864b900e066b7c9801edf373f5148   # main, 2026-08-07
UPSTREAM := https://github.com/handy-computer/transcribe.cpp.git

T        := third_party/transcribe.cpp
INSTALL  := $(T)/build/install
FIXTURE  := fixture/meeting_ja_2spk.wav
MODELS   := models

SORTFORMER_URL := https://huggingface.co/handy-computer/diar_streaming_sortformer_4spk-v2.1-gguf/resolve/main/diar_streaming_sortformer_4spk-v2.1-Q8_0.gguf

.PHONY: all deps build fixture diarize kotoba eval eval-models caps clean

all: deps build fixture diarize kotoba eval caps

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

# --- 日本語 CER -------------------------------------------------------------
# 評価セットは kotoba-whisper 自身の model card が使っているものと同じ 2 本。
# 公表値と突き合わせられるので、ハーネス側の誤りに気づける。
HF_DS := https://huggingface.co/datasets/japanese-asr

EVAL_MODELS := \
	SenseVoiceSmall-Q8_0:https://huggingface.co/handy-computer/SenseVoiceSmall-gguf/resolve/main/SenseVoiceSmall-Q8_0.gguf \
	Qwen3-ASR-0.6B-Q8_0:https://huggingface.co/handy-computer/Qwen3-ASR-0.6B-gguf/resolve/main/Qwen3-ASR-0.6B-Q8_0.gguf \
	Qwen3-ASR-1.7B-Q8_0:https://huggingface.co/handy-computer/Qwen3-ASR-1.7B-gguf/resolve/main/Qwen3-ASR-1.7B-Q8_0.gguf \
	Fun-ASR-MLT-Nano-2512-Q8_0:https://huggingface.co/handy-computer/Fun-ASR-MLT-Nano-2512-gguf/resolve/main/Fun-ASR-MLT-Nano-2512-Q8_0.gguf \
	whisper-large-v3-turbo-Q8_0:https://huggingface.co/handy-computer/whisper-large-v3-turbo-gguf/resolve/main/whisper-large-v3-turbo-Q8_0.gguf

eval-models:
	@mkdir -p $(MODELS)
	@for spec in $(EVAL_MODELS); do \
		name=$${spec%%:*}; url=$${spec#*:}; \
		test -f $(MODELS)/$$name.gguf || { echo "fetching $$name"; \
			curl -sfL --retry 3 -o $(MODELS)/$$name.gguf "$$url"; }; \
	done
	test -f $(MODELS)/kotoba-whisper-v2.0-Q8_0.gguf || \
		$(T)/build/bin/transcribe-quantize \
			$(T)/models/kotoba-whisper-v2.0/kotoba-whisper-v2.0-BF16.gguf \
			$(MODELS)/kotoba-whisper-v2.0-Q8_0.gguf --quant Q8_0

eval/cv8/refs.jsonl:
	mkdir -p eval/data
	curl -sfL -o eval/data/cv8_test.parquet "$(HF_DS)/ja_asr.common_voice_8_0/resolve/main/data/test-00000-of-00001.parquet"
	./eval/prepare.py eval/data/cv8_test.parquet eval/cv8 --n 100

eval/jsut/refs.jsonl:
	mkdir -p eval/data
	curl -sfL -o eval/data/jsut_test0.parquet "$(HF_DS)/ja_asr.jsut_basic5000/resolve/main/data/test-00000-of-00004.parquet"
	./eval/prepare.py eval/data/jsut_test0.parquet eval/jsut --n 100

eval: eval-models eval/cv8/refs.jsonl eval/jsut/refs.jsonl
	@for set in cv8 jsut; do echo "=== $$set ==="; \
		for m in kotoba-whisper-v2.0-Q8_0 whisper-large-v3-turbo-Q8_0 \
		         SenseVoiceSmall-Q8_0 Fun-ASR-MLT-Nano-2512-Q8_0 \
		         Qwen3-ASR-0.6B-Q8_0 Qwen3-ASR-1.7B-Q8_0; do \
			./eval/run.sh $$set $(MODELS)/$$m.gguf $$m -l ja; \
		done; done

# タイムスタンプの粒度がここで出る。CER より先に見るべき列。
caps:
	@for m in $(MODELS)/*.gguf; do ./tcspike -caps "$$m" 2>/dev/null | \
		python3 -c 'import json,sys; d=json.load(sys.stdin); \
print("%-30s ts=%-8s license=%-14s max_audio=%-10s ja=%s" % (d["model"].split("/")[-1], d["max_timestamps"], d["license"][:14], "unbounded" if d["max_audio_sec"]==0 else "%.0fs"%d["max_audio_sec"], d["advertises_ja"]))'; done

clean:
	rm -rf tcspike fixture $(MODELS) eval/data eval/cv8 eval/jsut eval/hyp
