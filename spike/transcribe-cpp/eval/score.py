#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Score transcribe-cli --batch-jsonl output against the reference file.

Japanese has no word boundaries, so CER is the primary figure -- the same
convention kotoba-whisper's own model card uses. The normalizer is stated
here rather than assumed: NFKC, drop whitespace, drop punctuation. Nothing
else is touched (no kana folding, no number normalization), because a
normalizer that rewrites content flatters whichever model happens to share
its conventions.
"""
import argparse
import json
import pathlib
import sys
import unicodedata

PUNCT = set("、。，．・「」『』（）()〔〕[]｛｝{}〈〉《》【】!?！？:;：；'\"“”‘’…―ー-—–~〜/／\\|｜*＊+＋=＝<>＜＞@＠#＃$＄%％^＾&＆_＿`｀,.")


def norm(s: str) -> str:
    s = unicodedata.normalize("NFKC", s)
    return "".join(ch for ch in s if not ch.isspace() and ch not in PUNCT)


def levenshtein(a: str, b: str) -> int:
    if len(a) < len(b):
        a, b = b, a
    prev = list(range(len(b) + 1))
    for i, ca in enumerate(a, 1):
        cur = [i]
        for j, cb in enumerate(b, 1):
            cur.append(min(prev[j] + 1, cur[j - 1] + 1, prev[j - 1] + (ca != cb)))
        prev = cur
    return prev[-1]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("refs")
    ap.add_argument("hyps", help="batch-jsonl output from transcribe-cli")
    ap.add_argument("--label", default="")
    ap.add_argument("--show", type=int, default=0, help="print N worst utterances")
    args = ap.parse_args()

    refs = {}
    for line in pathlib.Path(args.refs).read_text(encoding="utf-8").splitlines():
        r = json.loads(line)
        refs[r["file"]] = r["ref"]

    rows, missing = [], 0
    for line in pathlib.Path(args.hyps).read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        h = json.loads(line)
        if h.get("type") == "batch_header" or "file" not in h:
            continue
        ref = refs.get(h["file"])
        if ref is None:
            missing += 1
            continue
        r, y = norm(ref), norm(h.get("text", ""))
        rows.append((levenshtein(r, y), len(r), ref, h.get("text", "")))

    if not rows:
        print("no scored utterances", file=sys.stderr)
        return 1
    errs = sum(d for d, _, _, _ in rows)
    chars = sum(n for _, n, _, _ in rows)
    # Corpus CER: total edits over total reference characters. Not the mean of
    # per-utterance CERs, which lets one short utterance dominate.
    print(f"{args.label or args.hyps}\tCER {100*errs/chars:.2f}%\tn={len(rows)}\tchars={chars}"
          + (f"\tunmatched={missing}" if missing else ""))
    if args.show:
        for d, n, ref, hyp in sorted(rows, key=lambda r: -r[0] / max(r[1], 1))[: args.show]:
            print(f"  [{100*d/max(n,1):5.1f}%] ref: {ref}\n           hyp: {hyp}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
