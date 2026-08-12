#!/bin/zsh
# run.sh SET MODEL LABEL [extra transcribe-cli flags...]
# Writes hyp/<SET>.<LABEL>.jsonl and prints the CER line.
set -eu

HERE=${0:A:h}
CLI=$HERE/../third_party/transcribe.cpp/build/bin/transcribe-cli

SET=$1; MODEL=$2; LABEL=$3; shift 3

mkdir -p "$HERE/hyp"
OUT=$HERE/hyp/$SET.$LABEL.jsonl

start=$(python3 -c 'import time; print(time.time())')
"$CLI" -q -m "$MODEL" --batch "$HERE/$SET/files.txt" --batch-jsonl "$@" > "$OUT" 2>"$HERE/hyp/$SET.$LABEL.err"
end=$(python3 -c 'import time; print(time.time())')

secs=$(python3 -c "print(f'{$end-$start:.1f}')")
"$HERE/score.py" "$HERE/$SET/refs.jsonl" "$OUT" --label "$(printf '%-28s' "$LABEL")" | \
  awk -v s="$secs" '{print $0 "\twall=" s "s"}'
