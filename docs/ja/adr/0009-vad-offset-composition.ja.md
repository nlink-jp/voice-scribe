# ADR-0009: VAD と区間指定は元音声のタイムラインで合成する

- Status: Accepted
- Date: 2026-08-27

## Context

外部からのバグレポートで、VAD まわりの不具合が 2 件特定された。実材料は
1 時間の会議録画（末尾約 2 分 40 秒が無音）で、報告者は無音区間のハルシ
ネーションを `--vad` で抑制しようとして両方に行き当たっている。

**所見1: MCP サーバは VAD を一切有効化できない。** `transcribe` ツールの
InputSchema に `vad` がなく（`additionalProperties: false` なので渡す手段も
ない）、さらに `mcp_wiring.go` が `engine.Params` を組むとき `VADModelPath`
を設定せず、`Config.Transcribe.VAD` を参照する箇所も存在しない。CLI は
`--vad` フラグと config の OR を取るのに対し、MCP 経由では config の
`vad = true` が黙って無視される。`Threads` や `Diarize.Threshold` は MCP
でも config から読んでいるため、「config は MCP でも効く」という期待は
自然に生まれ、裏切られたことを告げるものが何もない。

**所見2: `--vad` と `--offset`/`--duration` を併用すると、黙って別の区間を
文字起こしする。** 上流 `whisper_full` は VAD を先に走らせ、`samples` を
発話のみの圧縮バッファに差し替えてから `whisper_full_with_state` が
`offset_ms` を適用する。つまり **offset は圧縮後のタイムラインを指す**。
一方、出力タイムスタンプは `vad_mapping_table` で元のタイムラインへ正しく
写し戻されるため、返る JSON は完全に整合して見える — 要求した区間と違う
ことだけが、どこにも現れない。報告の実測では、要求 offset 1800 秒に対し
実際の開始は 2101.1 秒（ずれは VAD が非音声と判定した累積長で、offset に
対して単調増加）。末尾側の指定では要求区間の実発話を全て落として
ハルシネーションだけを返し、圧縮後終端を越えると `empty_transcript` が
「音声が無音か、モデルが扱えない言語」という事実と異なる説明で出る。
`--diarize` 併用時は、話者分離側が Go 側で正しい区間を切る
（`diarizeSlice`）ため、話者ラベルとテキストの対応も崩れる。

## Decision

### 所見2: エンジン層で cut+shift する

`VADModelPath != ""` かつ offset/duration 指定時、`Session.Transcribe` が

1. サンプルを Go 側で要求区間に切り出し、
2. whisper には `offset_ms`/`duration_ms` を 0 で渡し（VAD は切り出した
   窓の中だけで動く）、
3. 返ってきたタイムスタンプに offset を加算して元のタイムラインへ戻す。

`diarizeSlice` が既に採用しているパターンの適用である。実装位置を cmd 層
ではなくエンジン層にしたのは、CLI・MCP・translate の 2 パス目のすべてが
`Session.Transcribe` を通るため、一箇所で全経路が直り、diarize との区間
不一致も自動的に解消するからである。切り出しの結果、窓が音声の終端より
後になった場合は、その事実をそのまま述べるエラーを返す。

副次的に、長い録画の一部だけを見る場合の VAD コストも窓の分だけに下がる。

### 所見1: MCP に `vad` を公開し、config も CLI と同じ式で読む

`transcribe` ツールに `vad` (boolean) を追加し、wiring は CLI と同じ
`resolveVAD(rt, req.VAD || Config.Transcribe.VAD)` を通す。VAD モデル
未導入時のエラーは既存の `classify` が `model_not_found` に分類する
（メッセージが `models pull` を含むため。追加の分岐は不要）。

### 同時に出荷し、所見2 を先に修正する

MCP からは VAD を有効化できないため、所見2 は所見1 に隠れていた。所見1
だけ先に直すと、`vad` と `offset_seconds` を同時に渡すクライアントが
所見2 を踏む経路を新設することになる。よって修正順序は所見2 が先、
リリースは両方まとめて 1 バージョンとする。

## Alternatives considered

**併用をエラーで拒否する。** 安全側だが、`--vad` はまさに長時間録画で
使いたい機能であり、区間指定との併用を禁じるのは機能の価値を下げる。

**上流 whisper.cpp を修正する。** offset を VAD より前に適用する変更は
上流の既存利用者にとっては挙動変更であり、submodule を不変タグに固定する
方針（ADR-0002）とも衝突する。Go 側の cut+shift は上流に手を入れずに
同じ結果を得る。

**cmd 層で切る。** `diarizeSlice` と対称になるが、CLI と MCP の両方に
同じ処理を書くことになり、片方だけ直る所見1 と同型の非対称を将来また
作りかねない。共通の通り道であるエンジン層を選んだ。

## Consequences

- offset/duration の意味が VAD の有無によらず「元音声の秒」で一致する。
- VAD 有効時の offset 指定は、従来と異なるセグメント（=本当に要求した
  区間）を返すようになる。従来の挙動に依存する使い方は想定できないため
  互換性の配慮はしない。
- ADR-0008 の VAD 測定はすべて全長実行（offset なし）なので、この不具合の
  影響を受けておらず、「VAD は既定にできない」という結論と README の
  文字数減少の記述はそのまま有効である。同 ADR の未解決事項「VAD が
  落とした内訳（幻覚 対 実音声）」に本レポートは一つのデータ点を与える
  が、それは区間ずれの結果であって VAD の判定精度ではない点に注意。
  全長実行での内訳測定は引き続き未解決のまま残る。
