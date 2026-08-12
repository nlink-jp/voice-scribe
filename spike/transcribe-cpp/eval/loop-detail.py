#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Quantify repetition loops in a long-form transcript.

`loop_max` alone does not say how much damage a loop did. A 48-segment loop
matters differently depending on whether it burned 20 seconds or 6 minutes of
audio, and on how much of the transcript's text is loop output rather than
content. Both are computable without a reference -- and without printing any
of the text, which this source does not permit.
"""
import argparse
import json
import pathlib


def runs(segs, minlen=3):
    """Maximal runs of consecutive segments with identical text."""
    out, i = [], 0
    while i < len(segs):
        j = i
        while j + 1 < len(segs) and segs[j + 1]["t"] == segs[i]["t"]:
            j += 1
        if j - i + 1 >= minlen and segs[i]["t"]:
            out.append((i, j))
        i = j + 1
    return out


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("json", nargs="+")
    args = ap.parse_args()

    print("%-24s %8s %9s %10s %12s %12s" % (
        "model", "loops>=3", "loop_segs", "loop_secs", "loop_chars", "of_total"))
    for path in args.json:
        d = json.loads(pathlib.Path(path).read_text(encoding="utf-8"))
        segs = [{"t": s["text"].get("ja", "").strip(), "a": s["start"], "b": s["end"]}
                for s in d["segments"]]
        total_chars = sum(len(s["t"]) for s in segs)

        rs = runs(segs)
        # The repeats are the waste; the first utterance of a run may be real.
        loop_segs = sum(j - i for i, j in rs)
        loop_secs = sum(segs[j]["b"] - segs[i + 1]["a"] for i, j in rs)
        loop_chars = sum(len(segs[i]["t"]) * (j - i) for i, j in rs)

        print("%-24s %8d %9d %9.1fs %12d %11.1f%%" % (
            d["metadata"]["model"], len(rs), loop_segs, loop_secs, loop_chars,
            100 * loop_chars / max(total_chars, 1)))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
