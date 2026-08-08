# ADR-0002: 話者分離を sherpa-onnx で行う

- Status: Accepted
- Date: 2026-08-08

## Context

gem-transcribe はクラウド側（Gemini）で話者推論を行っている。ローカル対がこれを欠くと
「劣化した代替」になるため、RFP の段階で **v1 に話者分離を含める**とユーザーが判断した。

whisper.cpp 単体では不可能である。同梱の tinydiarize は英語 2 話者限定で、日本語の
会議録には使えない。したがって第二のランタイムを抱える必要がある。

RFP はこれを Phase 2a のリスクとして明示し、「ONNX Runtime の静的リンクは whisper.cpp
ほど枯れていないため、着手時にバイナリサイズと署名への影響を実測する」と宿題を残していた。

## Decision

**sherpa-onnx（k2-fsa）を CGO で静的リンクする。** モデルは pyannote-segmentation-3.0
（話者境界の検出）と 3D-Speaker campplus（話者埋め込み）の 2 本立て。

ビルドタグは whisper の `cgo_whisper` とは**別に `cgo_sherpa` を立てる**。理由は下記の
「上流の pin が壊れた」件で、ONNX Runtime のアーカイブが取得できない環境でも
文字起こしバイナリは作れるようにしておく必要があると分かったため。

### submodule はリリースタグに固定する

**master 追従では動かなかった。** master（`xcframework-14-g634265c9`）が pin していた
onnxruntime 1.27.1 のハッシュが、実際に公開されているアセットと一致しない。zip 自体は
健全（`unzip -t` 通過）。リリースタグ **v1.13.4**（onnxruntime 1.27.0）ではハッシュが
一致した。

**機序**: `xcframework` は上流の**ローリングタグ**で、同一タグにアセットが随時
再アップロードされる（GitHub のリリースページでアセットの日付が混在する — 一部だけ
新しい）。コミットメッセージも `Set onnxruntime version to 1.27.1 in SPM` で、
onnxruntime のバージョンを動かしている最中の状態だった。**可変リリースに依存していた**
のが原因で、上流の不具合というより我々の pin の選び方の問題である。

したがって submodule は**不変のバージョンタグに固定**する（現在 v1.13.4 のタグコミット
そのもの）。ローリングタグや master の任意コミットは、第三者が中身を差し替えられる点で
再現性がない。**submodule を更新するときは、必ず `vX.Y.Z` 形式のタグを選ぶこと。**

### ONNX Runtime は Makefile が先に取得する

sherpa-onnx の cmake は configure 中に onnxruntime の静的アーカイブを取りに行くが、
**cmake 内蔵のダウンローダはこの環境で失敗する**（同じ URL に curl は 200 で到達する）。
sherpa-onnx の cmake はローカルファイルを探すフォールバックを持っているので、Makefile が
curl で取得して build ツリーに置き、そこを拾わせる。

URL とハッシュは Makefile に書き写さず、**submodule の cmake ファイルから抽出する**。
submodule 更新に自動追随させるためで、かつ**取得後にハッシュを検証する**（上流の pin が
一度腐っている以上、検証を省く理由がない）。

> **Makefile の罠**: `$(shell ...)` の中に括弧を含む正規表現を書くと make のパーサが
> 壊れる（`unterminated call to function 'shell'`）。抽出は括弧を使わない形で書くこと。

## Consequences

### 実測（2026-08-08、M2 Max / macOS 26）

| 項目 | 結果 |
|---|---|
| バイナリ | 10.2 MB → **29.5 MB**（+19.3 MB、+190%） |
| 増分の中身 | ほぼ ONNX Runtime の静的ライブラリ |
| 動的依存の追加 | Security.framework / AVFAudio — **いずれも OS 標準**、第三者 dylib はゼロ |
| 話者分離の所要時間 | 17 秒の音声に対し約 2 秒（CPU、Metal 非使用） |

**署名・notarize への影響はない見込み**（第三者 dylib が増えていないため）。ただし
リリース時に実物で確認すること。

### モデルとライセンス（RFP の宿題）

| モデル | 配布元 | ライセンス |
|---|---|---|
| pyannote-segmentation-3.0 (ONNX) | csukuangfj/sherpa-onnx-pyannote-segmentation-3-0 | **MIT © 2022 CNRS**（ONNX パッケージが原本の LICENSE を同梱） |
| 3D-Speaker campplus | csukuangfj/speaker-embedding-models | **Apache-2.0**（modelscope/3D-Speaker 由来） |
| sherpa-onnx 本体 | k2-fsa/sherpa-onnx | Apache-2.0 |

**上流の pyannote/segmentation-3.0 は Hugging Face 上で gated だが、sherpa-onnx の
エクスポート版は ungated**。image-forge で学んだ「gated repo は ungated ミラーを探す」
と同じ構図。

### sherpa の config はゼロ値で渡してはいけない

whisper の `whisper_full_default_params()` に相当する既定値ゲッターが C API に無い。
Go の `var cfg C.Sherpa...Config` はゼロ初期化なので、**threshold = 0 / min_duration = 0
で渡すことになり、全ターンが 1 話者に潰れる**。実際に最初の実装がこれで、
「話者分離は動いているが全員 A」という症状になった。既定値（threshold 0.5 /
min_duration_on 0.3 / min_duration_off 0.5）は明示的に書き出す。

### `--min-speakers` / `--max-speakers` は実装しない（RFP からの逸脱）

RFP はこの 2 つのフラグを挙げていたが、**sherpa-onnx のクラスタラにその概念が無い**。
`FastClusteringConfig` は `num_clusters`（正確な数）か `threshold`（距離）のどちらかで、
その中間は取れない（`fast-clustering.cc` の分岐を実地に確認）。

存在しない機能のフラグを出すより、実在するつまみを出すほうがよい。したがって
**`--speaker-threshold` に置き換える**。`[diarize] min_speakers` / `max_speakers` も
`threshold` に置き換えた。

### 話者ラベルはクラスタ番号ではなく登場順

sherpa が返す speaker はクラスタ番号で、意味を持たない（3 番は「3 人目」ではない）。
読み手は「A は最初に喋った人」を期待するので、**時間順に振り直す**。
`--speaker-hint` の名前もこの順に当てる。

### 検証で踏んだ落とし穴（フィクスチャ側）

最初のテスト音声は `say -v Kyoko` と `say -v Otoya` で作ったが、**Otoya は未インストールで
`say` が既定音声に無言でフォールバック**しており、4 クリップすべてが同一話者だった。
話者分離が「1 話者」と報告したのは**正しかった**。

実在する 2 音声（Kyoko / Rocko）で作り直したところ、自動で 2 話者を検出し、
5 ターンすべての話者交代を正しく再現した。**コードを疑う前にフィクスチャを検証すること。**

### 受け入れる代償

- バイナリが 3 倍近くになる。話者分離を使わない利用者にも課される
- ビルドに ONNX Runtime のダウンロード（18 MB）が要る
- 話者分離は CPU 実行（ONNX Runtime の CoreML EP は未使用）
