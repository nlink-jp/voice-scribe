#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Speaker-structure summary for a diarized run, from either engine.

Reads voice-scribe's envelope (segments[].speaker) or transcribe-cli's
--batch-jsonl (speakers[].speaker_id). Prints counts and time shares only --
no transcript text, because the source is a private recording.

The interesting number is not "how many speakers" on its own but how much of
the audio the top few hold: over-splitting shows up as a long tail of
speakers who hold almost no time.
"""
import argparse
import json
import pathlib
from collections import defaultdict


def from_voice_scribe(d):
    out = defaultdict(float)
    for s in d["segments"]:
        out[s.get("speaker") or "?"] += max(0.0, s["end"] - s["start"])
    return out, d["metadata"]["duration_seconds"]


def from_transcribe_cli(d, dur):
    out = defaultdict(float)
    for s in d.get("speakers", []):
        out[f"S{s['speaker_id']}"] += max(0, s["t1_ms"] - s["t0_ms"]) / 1000
    return out, dur


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("json")
    ap.add_argument("--label", default="")
    ap.add_argument("--duration", type=float, default=0.0,
                    help="audio seconds (needed for transcribe-cli input)")
    args = ap.parse_args()

    raw = pathlib.Path(args.json).read_text(encoding="utf-8").strip()
    try:
        doc = json.loads(raw)  # voice-scribe: one pretty-printed object
    except json.JSONDecodeError:
        doc = json.loads(raw.splitlines()[-1])  # transcribe-cli: jsonl
    if "segments" in doc:
        share, dur = from_voice_scribe(doc)
    else:
        share, dur = from_transcribe_cli(doc, args.duration)

    total = sum(share.values()) or 1.0
    ranked = sorted(share.values(), reverse=True)
    top4 = 100 * sum(ranked[:4]) / total
    singletons = sum(1 for v in share.values() if v < 2.0)  # under 2 s of speech

    print(f"{args.label or args.json}\tspeakers={len(share)}"
          f"\ttop4_share={top4:.1f}%\tunder2s={singletons}"
          f"\tspeech={total:.0f}s/{dur:.0f}s")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
