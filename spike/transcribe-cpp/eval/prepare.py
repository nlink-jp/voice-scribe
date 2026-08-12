#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = ["pandas", "pyarrow", "soundfile", "numpy"]
# ///
"""Turn a japanese-asr parquet into 16 kHz mono WAVs + a reference file.

transcribe.cpp takes 16 kHz mono WAV only, so the resampling happens here
once and every model under test reads byte-identical audio.
"""
import argparse
import io
import json
import pathlib
import sys

import numpy as np
import pandas as pd
import soundfile as sf


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("parquet")
    ap.add_argument("outdir")
    ap.add_argument("--n", type=int, default=100)
    ap.add_argument("--seed", type=int, default=20260812)
    args = ap.parse_args()

    out = pathlib.Path(args.outdir)
    (out / "wav").mkdir(parents=True, exist_ok=True)

    df = pd.read_parquet(args.parquet)
    print("columns:", list(df.columns), file=sys.stderr)
    # Deterministic subset: the same utterances for every model, and the same
    # set again on a re-run.
    df = df.sample(n=min(args.n, len(df)), random_state=args.seed).reset_index(drop=True)

    text_col = next(c for c in ("transcription", "sentence", "text") if c in df.columns)
    refs, paths, total = [], [], 0.0
    for i, row in df.iterrows():
        audio = row["audio"]
        data = audio["bytes"] if isinstance(audio, dict) else audio
        pcm, rate = sf.read(io.BytesIO(data), dtype="float32", always_2d=True)
        pcm = pcm.mean(axis=1)  # mono
        if rate != 16000:  # linear resample is enough for a WER harness
            n = int(round(len(pcm) * 16000 / rate))
            pcm = np.interp(
                np.linspace(0, len(pcm) - 1, n, dtype=np.float64),
                np.arange(len(pcm), dtype=np.float64),
                pcm.astype(np.float64),
            ).astype(np.float32)
        wav = out / "wav" / f"{i:04d}.wav"
        sf.write(wav, pcm, 16000, subtype="PCM_16")
        total += len(pcm) / 16000
        paths.append(str(wav.resolve()))
        refs.append({"file": str(wav.resolve()), "ref": str(row[text_col])})

    (out / "files.txt").write_text("\n".join(paths) + "\n", encoding="utf-8")
    with (out / "refs.jsonl").open("w", encoding="utf-8") as f:
        for r in refs:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    print(f"{len(refs)} utterances, {total/60:.1f} min -> {out}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
