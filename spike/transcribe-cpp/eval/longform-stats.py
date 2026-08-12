#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Structural health of a long-form transcript, with no reference text.

A 39-minute recording has no ground truth, so accuracy cannot be scored. What
CAN be scored is whether the transcript is structurally sane -- and the two
failure modes ADR-0006 documented on this kind of source are exactly that
kind: a decoder that falls into a repetition loop, and speech that is never
covered by any segment.

Reports counts and rates only. No transcript text is printed, because the
source is a private recording.
"""
import argparse
import json
import pathlib
import sys
from collections import Counter


def load(path):
    d = json.loads(pathlib.Path(path).read_text(encoding="utf-8"))
    segs = [
        {"start": s["start"], "end": s["end"], "text": s["text"].get("ja", "")}
        for s in d["segments"]
    ]
    return d["metadata"], segs


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("json", nargs="+")
    args = ap.parse_args()

    hdr = ("model", "segs", "chars", "cover%", "gap_max", "dup_seg%", "loop_max", "dropped", "rtf")
    print("%-24s %6s %7s %7s %8s %9s %9s %8s %6s" % hdr)

    for path in args.json:
        meta, segs = load(path)
        dur = meta["duration_seconds"]
        if not segs:
            print(f"{path}: no segments", file=sys.stderr)
            continue

        chars = sum(len(s["text"]) for s in segs)
        covered = sum(max(0.0, s["end"] - s["start"]) for s in segs)

        # Largest stretch of audio no segment claims. On this source the
        # music-only passages make some gap normal; a very large one means
        # the decoder lost the thread.
        gap, prev = 0.0, 0.0
        for s in sorted(segs, key=lambda s: s["start"]):
            gap = max(gap, s["start"] - prev)
            prev = max(prev, s["end"])
        gap = max(gap, dur - prev)

        # Repetition: identical segment text anywhere (dup rate), and the
        # longest run of consecutive identical segments (an actual loop).
        texts = [s["text"].strip() for s in segs if s["text"].strip()]
        dup = sum(c - 1 for c in Counter(texts).values() if c > 1)
        loop, run = 1, 1
        for a, b in zip(texts, texts[1:]):
            run = run + 1 if a == b else 1
            loop = max(loop, run)

        print("%-24s %6d %7d %6.1f%% %7.1fs %8.1f%% %9d %8d %6.3f" % (
            meta["model"], len(segs), chars, 100 * covered / dur, gap,
            100 * dup / max(len(texts), 1), loop,
            meta.get("dropped_segments", 0), meta["real_time_factor"],
        ))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
