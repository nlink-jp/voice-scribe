#!/bin/zsh
# Two-speaker Japanese fixture for the transcribe.cpp spike (ADR-0007).
#
# ADR-0002 の教訓に従い、`say` が既定音声へ無言でフォールバックしていないことを
# 先に確かめる: 同一テキストを 2 音声で読ませ、波形が異なることを確認してから
# ターンを組み立てる。フィクスチャを疑わずにコードを疑うと、1 話者の音声を
# 「話者分離が壊れている」と読み違える。
set -eu

OUT=${1:-fixture}
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)   # concat のリストは絶対パスで書く必要がある
cd "$OUT"

VOICE_A=Kyoko
VOICE_B=Rocko

# --- 1. フィクスチャ自体の検証: 2 音声は本当に別物か -----------------------
say -v "$VOICE_A" -o probe_a.aiff "これはテストです"
say -v "$VOICE_B" -o probe_b.aiff "これはテストです"
if [ "$(shasum -a 256 probe_a.aiff | cut -d' ' -f1)" = "$(shasum -a 256 probe_b.aiff | cut -d' ' -f1)" ]; then
  echo "FATAL: $VOICE_A と $VOICE_B が同一波形 — say がフォールバックしている" >&2
  exit 1
fi
echo "voices differ OK  ($VOICE_A != $VOICE_B)"

# --- 2. 交互ターンの生成 ---------------------------------------------------
i=0
: > concat.txt
turn() {
  local voice=$1 text=$2
  local f=$(printf 'turn%02d.aiff' $i)
  say -v "$voice" -o "$f" "$text"
  echo "file '$OUT/$f'" >> concat.txt
  i=$((i + 1))
}

turn "$VOICE_A" "おはようございます。本日の定例会議を始めます。"
turn "$VOICE_B" "はい、よろしくお願いします。"
turn "$VOICE_A" "先週のインシデント対応について共有します。"
turn "$VOICE_B" "了解しました。ログの解析結果もお願いします。"
turn "$VOICE_A" "では、順番に説明していきます。"

# --- 3. 16 kHz mono WAV へ（transcribe.cpp の入力要件） --------------------
ffmpeg -hide_banner -loglevel error -y -f concat -safe 0 -i concat.txt \
  -ar 16000 -ac 1 -c:a pcm_s16le meeting_ja_2spk.wav

# --- 4. 正解のターン境界（判定はこれと突き合わせる） -----------------------
acc=0
for n in 00 01 02 03 04; do
  d=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "turn$n.aiff")
  printf 'turn%s  %7.0f - %7.0f ms\n' "$n" "$(echo "$acc*1000" | bc)" "$(echo "($acc+$d)*1000" | bc)"
  acc=$(echo "$acc + $d" | bc)
done
echo "wrote $OUT/meeting_ja_2spk.wav"
