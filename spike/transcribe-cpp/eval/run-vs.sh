#!/bin/zsh
# run-vs.sh SET MODEL_NAME [extra voice-scribe flags...]
#
# Same measurement as run.sh, but through voice-scribe's own binary
# (whisper.cpp runtime) instead of transcribe-cli. Deciding voice-scribe's
# default model on numbers measured in a different runtime would not be valid.
#
# voice-scribe transcribes one file per invocation, so this pays a model load
# per utterance; the wall time here is NOT comparable to run.sh's.
set -eu

HERE=${0:A:h}
VS=${VS:-$HERE/../../../dist/voice-scribe}

SET=$1; MODEL=$2; shift 2

mkdir -p "$HERE/hyp"
OUT=$HERE/hyp/$SET.vs-$MODEL.jsonl
: > "$OUT"

start=$(python3 -c 'import time; print(time.time())')
while IFS= read -r f; do
  [ -n "$f" ] || continue
  "$VS" transcribe "$f" --lang ja -m "$MODEL" -q "$@" 2>/dev/null | \
    python3 -c "
import json,sys
d = json.load(sys.stdin)
# Flatten the gem-transcribe envelope: segments[].text is a lang->text map.
text = ''.join(s['text'].get('ja', '') for s in d['segments'])
print(json.dumps({'file': sys.argv[1], 'text': text,
                  'rtf': d['metadata']['real_time_factor'],
                  'n_segments': len(d['segments'])}, ensure_ascii=False))
" "$f" >> "$OUT"
done < "$HERE/$SET/files.txt"
end=$(python3 -c 'import time; print(time.time())')

secs=$(python3 -c "print(f'{$end-$start:.1f}')")
rtf=$(python3 -c "
import json
rs=[json.loads(l)['rtf'] for l in open('$OUT')]
print(f'{sum(rs)/len(rs):.3f}')
")
"$HERE/score.py" "$HERE/$SET/refs.jsonl" "$OUT" --label "$(printf '%-22s' "$MODEL")" | \
  awk -v s="$secs" -v r="$rtf" '{print $0 "\tmean_rtf=" r "\twall=" s "s"}'
