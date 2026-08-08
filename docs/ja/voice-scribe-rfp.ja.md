# RFP: voice-scribe

> Generated: 2026-08-08
> Status: Draft
> 正本: この日本語版（`docs/en/voice-scribe-rfp.md` は翻訳）

## 1. Problem Statement

音声を直接扱えない、あるいは音声認識が苦手な LLM を使っているエージェントに対して、
**ローカル完結の文字起こし能力を MCP 経由で与える**ツール。gem-transcribe（Vertex AI
Gemini）と同じ仕事を、API コストゼロ・音声データを外部へ送信せずに実行する。

対象ユーザーは nlink-jp org の運用者自身と、util-series の MCP 群を利用するエージェント
（Claude Code / 他クライアント）。入力はローカルの音声・動画ファイル、出力は話者ラベルと
タイムスタンプ付きの構造化テキスト。議事録の構造化・要約はスコープ外で、後段の
meeting-notes スキルへ渡す前提とする。

## 2. Functional Specification

### Commands / API Surface

単一バイナリ `voice-scribe` に CLI と MCP サーバーを同居させる
（image-forge と同型、org の single-binary-subcommand 慣習に準拠）。

#### CLI

| サブコマンド | 用途 |
|---|---|
| `voice-scribe transcribe <file>` | 本体。文字起こし＋（任意で）話者分離 |
| `voice-scribe models list` | インストール済み／カタログの一覧（`--catalog` / `--all` / `--json`） |
| `voice-scribe models pull <name>` | モデル取得（weights + プロファイル登録） |
| `voice-scribe models import <path>` | ローカル ggml/ONNX モデルの登録（`--kind`） |
| `voice-scribe models rm <name>` | 削除 |
| `voice-scribe mcp` | MCP サーバー起動（stdio） |
| `voice-scribe doctor` | モデル／Metal／デコード可否／ONNX Runtime の診断 |
| `voice-scribe --version` | 版数（`git describe` 由来、Homebrew の `brew test` が叩く） |

`transcribe` のフラグ:

| フラグ | 意味 |
|---|---|
| `-m, --model <name>` | レジストリ名。省略時は config の `default_model` |
| `--lang <code>` | 入力言語。省略時は自動判定 |
| `--translate` | 英語訳を併記（whisper の translate タスク）。`text` に `en` キーが増える |
| `--diarize` | 話者分離を有効化 |
| `--speakers <N>` | 話者数を固定 |
| `--min-speakers` / `--max-speakers` | 話者数の探索範囲 |
| `--speaker-hint <names>` | 話者ラベルを A/B/C ではなく指定名に割り当て |
| `--prompt <text>` | 語彙バイアス（whisper の initial prompt）。固有名詞・専門用語対策 |
| `--offset <sec>` / `--duration <sec>` | 部分処理 |
| `-f, --format <fmt>` | `json`(既定) / `text` / `md` / `srt` / `vtt` |
| `-o, --output-file <path>` | 出力先。省略時は stdout |
| `--threads <N>` | 推論スレッド数 |
| `--vad` / `--no-vad` | 無音区間の VAD ゲート（ハルシネーション抑制） |

#### MCP ツール（4本）

| ツール | 種別 | 説明 |
|---|---|---|
| `get_usage` | 同期 | 使い方の全文（embed した usage.md）。初回に読ませる |
| `transcribe` | **非同期ジョブ** | `job_id` を即時返す。引数は CLI とほぼ対応 |
| `check_job` | 同期 | ジョブのポーリング。完了時に結果を返す |
| `list_models` | 同期 | インストール済みモデル（`models list --json` と同一ビュー） |

**`models pull` は MCP に露出しない。** 数 GB のダウンロードをエージェントの判断で
開始させないため。未インストール時は `list_models` と各エラーが CLI 手順を案内する。

### Input / Output

#### 入力

- ローカルの音声・動画ファイル（m4a / mp3 / wav / aiff / caf / mp4 / mov / flac 等）
- デコードは **AVFoundation を CGO 経由で叩く**（16kHz mono float32 PCM へ変換）
- MCP 経由では `workspace_root`（絶対パス、省略時 `~/.voice-scribe`）配下に封じ込め
  （voice-studio-mcp ADR-0010 と同じ `os.Root` によるカーネル封じ込め）

#### 出力（JSON）

gem-transcribe と**互換のエンベロープ**を採用する。後段（meeting-notes 等）が
どちらの文字起こし元でも同じパーサで読めることを保証するため。

```json
{
  "metadata": {
    "source": "meeting.m4a",
    "model": "kotoba-whisper-v2.2-q5_0",
    "duration_seconds": 3421.5,
    "languages": ["ja"],
    "speaker_hints": [],
    "dropped_segments": 0
  },
  "segments": [
    {
      "start": 0.0,
      "end": 4.12,
      "speaker": "A",
      "text": { "ja": "それでは始めます。" }
    }
  ]
}
```

- `text` は**言語コード → テキストのマップ**。gem-transcribe の `--lang=en,ja`
  （原文＋翻訳）と同じ形。voice-scribe では `--translate` 指定時に `en` キーが増える。
  **2026-08-08 追記**: whisper の translate は「出力の追加」ではなく**別デコード**
  なので、原文と英訳の両方を得るには音声を 2 回通す必要がある。所要時間は約 2 倍になり、
  2 パスのセグメント境界が一致しないため時間の重なりでマージする（近似）。
- `speaker` は `--diarize` 無効時は全セグメント `"A"` 固定。有効時は `A`/`B`/…、
  `--speaker-hint` 指定時は与えられた名前。
- `dropped_segments` は形状互換のため常に存在し、通常 0
  （whisper はモデル出力の JSON 破損が起きないため）。
- voice-scribe 固有の情報（engine、量子化、RTF、diarization パラメータ）は
  `metadata` に**追加フィールド**として載せる。gem-transcribe 側は無視できる。

#### 返却方式（MCP）— 二段

成果物がテキストであるため、画像・音声系 MCP の file-mediated 原則をそのまま適用しない。

- 既定 **8 KB 以下**: テキストをインラインで返却（往復 1 回で済む）
- 閾値超過: `output/` 配下のファイルパス＋先頭抜粋＋総セグメント数を返却
- 閾値は `[mcp] inline_threshold`（config）とツール引数で上書き可

### Configuration

`~/.config/voice-scribe/config.toml`（`XDG_CONFIG_HOME` 準拠、macOS でも `~/.config`
を探す。`$VOICE_SCRIBE_CONFIG` で明示指定可）。

```toml
default_model = "kotoba-whisper-v2.2"
models_dir = "~/.local/share/voice-scribe/models"   # 大容量ディスクへ逃がせる

[transcribe]
format = "json"
vad = true
threads = 0          # 0 = 自動

[diarize]
enabled = false
min_speakers = 1
max_speakers = 8

[mcp]
inline_threshold = 8192
```

env var（`$VOICE_SCRIBE_MODELS_DIR` 等）> config の優先順位。
config スキーマ・ローダの流儀は org 統一、パスは本ツール個別。

### External Dependencies

| 依存 | 種別 | 備考 |
|---|---|---|
| whisper.cpp (ggml) | **静的リンク**（CGO） | MIT。Metal バックエンド |
| sherpa-onnx + ONNX Runtime | **静的リンク**（CGO、Phase 2a） | 話者分離。Apache-2.0 |
| AVFoundation / CoreMedia / AudioToolbox | macOS システムフレームワーク | 音声デコード |
| Metal / MetalKit / Foundation / Accelerate | macOS システムフレームワーク | 推論 |
| Hugging Face | ネットワーク（`models pull` 時のみ） | ungated、トークン不要 |

**ランタイムの外部プロセス依存はゼロ**（ffmpeg 不要）。

## 3. Design Decisions

### なぜ Go + CGO 静的リンク（whisper.cpp）か

image-forge で stable-diffusion.cpp を CGO 静的リンクし、Metal shader 埋め込み・
ggml 静的リンク・Developer ID 署名・notarize まで通した実績がある。whisper.cpp は
同じ ggml 上に載っているため、**プロジェクト最大のビルドリスクが既に解決済み**である。
LDFLAGS のフレームワーク列、`build-engine` の cmake 手順、`third_party` を
`go test ./...` から除外する運用まで、そのまま流用できる。

対抗案の評価:

| 案 | 却下／保留理由 |
|---|---|
| Apple SpeechAnalyzer / SpeechTranscriber (macOS 26) | オンデバイス・モデル管理不要・省電力と利点は本物だが、MCP サーバーを Swift で書くことになり org 内に前例がない。対応ロケール・タイムスタンプ粒度・語彙ヒントの制御が OS 任せ。**却下ではなく保留** — instant-translate ↔ quick-translate と同じ「軽量姉妹」構図で 2 本目として後から足す余地を残す |
| MLX Whisper / faster-whisper (Python) | gem-transcribe が Python なので姉妹としては自然だが、利用者に Python ランタイムとモデル配置が露出する。単一バイナリの手軽さを失う代償が大きい |
| parakeet 系 | 日本語が弱く、主要ユースケースに合わない |

### なぜ AVFoundation デコード（ffmpeg ではなく）か

whisper.cpp は 16kHz mono の生 PCM しか受け付けないため変換層が必須。ffmpeg 外部依存は
実装ゼロで全形式に対応できるが、**「単一バイナリで完結する」という image-forge 以来の
配布上の強みが崩れる**。darwin 専用リリースである以上 AVFoundation は常に存在し、
移植性の損失もない。代償は Objective-C ブリッジの実装コストで、これは Phase 1 の
リスクとして明示的に受け入れる。AVFoundation が扱えないコンテナ（mkv / webm 等）は
**明示エラーで拒否**し、ffmpeg での事前変換を案内する。

### なぜ話者分離を v1 に含めるか

gem-transcribe がクラウド側で話者推論を行っているため、ローカル対がこれを欠くと
「劣化した代替」になってしまう。whisper.cpp 単体では不可能（tinydiarize は英語 2 話者
限定）なので、sherpa-onnx の pyannote-segmentation-3.0（ONNX 6.6MB）＋ speaker
embedding を追加する。engine が 2 本立てになるリスクは、**Phase 1 を文字起こしコアに
限定し、話者分離を Phase 2a として独立レビュー可能に切る**ことで管理する。

### なぜモデルカタログ方式か

whisper 系モデルは量子化・言語適性・速度のトレードオフが大きく、利用者に選択を
強いると使われない。image-forge のプロファイル方式（落とし穴を既定値で隠蔽する）を
踏襲し、`kotoba-whisper v2.x`（日本語最適・large-v3 比 6.3 倍速・WER 同等）と
`large-v3-turbo`（多言語）を両方カタログに載せ、言語指定から自動選択する。

### 既存 nlink-jp ツールとの関係

| ツール | 関係 |
|---|---|
| gem-transcribe | **クラウド版の対**。出力エンベロープを互換に保つ |
| voice-studio-mcp | **逆方向**（TTS ↔ STT）。workspace 設計・ジョブ設計を共有 |
| meeting-notes | **後段**。voice-scribe の JSON をそのまま食わせる |
| image-forge | **骨格の移植元**（CGO×ggml×Metal、catalog/store/download、MCP 同居） |
| data-toolbox-mcp / video-studio-mcp | MCP 骨格（jsonrpc / transport / mcpserver / toolerr / job / workspace）の移植元 |

### 明示的なスコープ外

- リアルタイム／ストリーミング認識、マイク入力（ファイル入力のみ）
- モデルの学習・ファインチューン
- クラウド STT へのフォールバック
- 議事録の構造化・要約・アクションアイテム抽出（meeting-notes の仕事）
- darwin/arm64 以外のプラットフォーム
- 音声の編集・加工（voice-studio / video-studio の領分）

## 4. Development Plan

### Phase 1: Core（独立レビュー可 — CLI 単体で完結）

1. **ビルド疎通スパイクを最初に置く** — whisper.cpp を submodule 化し cmake で静的
   ライブラリ化、CGO 静的リンクして `whisper_print_system_info()` を Go から呼び、
   Metal 初期化を実機確認する。プロジェクト最大のリスクを先頭で潰す
2. Scaffold（Go module、Makefile、`docs/{en,ja}`、LICENSE(MIT)、`config.example.toml`、
   AGENTS.md、`.gitignore` は `dist/` のみ）
3. AVFoundation デコーダ（CGO / Objective-C）: 任意形式 → 16kHz mono float32
4. engine 層: `Open` / `Transcribe` / `Close` の Session 化（常駐対応）、進捗コール
   バック、**ログの stdout 隔離**（下記「既知の地雷」）
5. `internal/{store,catalog,download}` を image-forge から移植（レジストリ、
   プロファイル、レジューム DL）
6. CLI: `transcribe` / `models` / `doctor` / `--version`
7. フォーマッタ 5 種（json / text / md / srt / vtt）、gem-transcribe 互換エンベロープ
8. config.toml
9. テスト（純関数中心。engine は build tag で実体／stub 切替、`go test ./...` は
   `third_party` を除外）

**Phase 1 の完了条件**: 実音声ファイルから CLI で日本語 JSON 出力を得る E2E 成功。

### Phase 2: Features

**Phase 2a — 話者分離（独立レビュー可）**

- sherpa-onnx + ONNX Runtime の CGO 静的リンク
- pyannote-segmentation-3.0 + speaker embedding モデルをカタログに追加（`kind` 拡張）
- diarization タイムラインと whisper セグメントのマージ（境界不一致の解決規則を
  純関数として切り出しテスト）
- `--speakers` / `--min-speakers` / `--max-speakers` / `--speaker-hint`
- ONNX 再配布ライセンスの確認と帰属表記

**Phase 2b — MCP サーバー（独立レビュー可）**

- 骨格移植（jsonrpc / transport / mcpserver / toolerr / job(単一 FIFO worker) / workspace）
- 4 ツール、非同期ジョブ、`workspace_root` + `os.Root` 封じ込め
- 二段返却（inline / file）と閾値の実装
- `initialize` の `instructions` と `get_usage`（embed した usage.md との整合テスト）
- ダミー stdio クライアントによる実エンジン E2E

**Phase 2c — 仕上げ**

- `--prompt`（語彙バイアス）、`--translate`、`--offset` / `--duration`、VAD
- 常駐エンジンのモデル切替（reload key）

### Phase 3: Release

1. README.md / README.ja.md、`docs/{en,ja}` 3 層、ADR（設計判断を遡って記録）
2. `make build-all` → Developer ID 署名 → zip → notarize（Accepted + staple）
3. zip 展開実物検証（`--version` 応答、`spctl` が `Notarized Developer ID`）
4. GitHub Release（public リポジトリ、LICENSE 必須）
5. Homebrew tap（`BREW_MACOS_FLOOR` の設定、`brew audit --cask --online`）
6. util-series submodule pointer 更新
7. `nlink-jp/.github/profile/README.md`（アルファベット順）、util-series README、
   web-site カタログ EN/JA の 3 面同期
8. `check-org.sh` all green
9. `nlink-jp/knowledge` へ知見還元

### 独立レビュー可能な単位

- **Phase 1** — MCP を介さず CLI 単体で E2E 検証できる
- **Phase 2a**（話者分離）と **Phase 2b**（MCP）は互いに独立。並行も順不同も可

## 5. Required API Scopes / Permissions

**None.**

- 認証情報・API キー・OAuth スコープ・IAM ロールはいずれも不要
- マイク権限（`NSMicrophoneUsageDescription`）は不要 — ファイル入力のみ
- ネットワークアクセスは `models pull` 実行時の Hugging Face への HTTPS のみ。
  対象モデル（kotoba-whisper 系、ggerganov/whisper.cpp、sherpa-onnx リリース）は
  いずれも ungated でトークン不要
- 推論そのものはネットワークを使わないが、ドキュメントに「完全オフライン」とは
  書かない（モデル取得と OS の挙動を保証できないため）

## 6. Series Placement

**Series: util-series**

理由:

- パイプフレンドリーな変換 CLI（音声 → 構造化テキスト）であり util-series の定義に合致
- 姉妹となる gem-transcribe / voice-studio-mcp / video-studio-mcp / image-forge が
  すべて util-series に属しており、命名・骨格・リリース手順を共有する
- 外部サービスの対話クライアント（cli-series）でも、Slack 自動化（chatops-series）でも、
  セキュリティツール（cybersecurity-series）でもない
- 実験段階ではなく、明確な成果物と利用者を持つため lab-series ではない

## 7. External Platform Constraints

### デコード

- **AVFoundation が扱えないコンテナ（mkv / webm 等）は非対応。** 明示エラーで拒否し、
  ffmpeg での事前変換を案内する

### whisper / モデル

- 無音区間や BGM のみの区間で**同一フレーズを繰り返すハルシネーション**が起きる。
  Silero VAD ゲート（whisper.cpp 側にサポートあり）を既定 ON にして緩和する
- whisper は 30 秒チャンク単位。長尺は sequential long-form で処理される
  （whisper.cpp が対応済み）が、チャンク境界での文の分断は避けられない
- `large-v3-turbo` q5 で約 550MB。~~Metal の cold-load があるため常駐（MCP / serve）の
  価値が高い~~ → **2026-08-08 訂正**: Metal ライブラリの初期化 8.8 秒は初回のシェーダ
  コンパイルのみで OS がキャッシュし、2 回目以降は 0.011 秒だった。常駐の根拠は
  Metal ではなくモデルのロード時間にある。詳細は ADR-0001
- Hugging Face の Xet ストレージへの 302 リダイレクトがある（image-forge の既知事項。
  range GET はマニフェストを返すが全体 GET は実バイトを返す）

### ビルド・配布

- **CGO × Metal のためクロスコンパイル不可 → darwin/arm64 単一リリース**
  （image-forge と同じ制約）
- ONNX Runtime の静的リンクは whisper.cpp ほど枯れていないため、Phase 2a 着手時に
  バイナリサイズと署名への影響を実測する
- pyannote-segmentation-3.0 の ONNX 再配布ライセンス（sherpa-onnx による export）は
  Phase 2a 着手時に確認し、必要なら帰属表記を README に追加する

### 既知の地雷（先回りで潰す）

image-forge v0.9.0 で踏んだ **「ネイティブライブラリの出力が stdout に漏れて JSON-RPC を
破壊する」** 罠が同型で起きる。2 層で防ぐ:

1. **発生源** — `whisper_log_set` / `ggml_log_set` でログコールバックを差し替え、
   `print_progress` / `print_realtime` を無効化する
2. **MCP 側** — 本物の stdout を dup して transport 専用にし、fd 1 を stderr へ向ける
   （defense-in-depth）

そのうえで、**「コンソールで見えない ≠ stdout 未出力」**なので `1>file 2>file` の
分離実測で検証する。

---

## Discussion Log

**発端（2026-08-08）** — 「音声文字起こしが苦手なモデルを使っているエージェント向けに
文字起こし MCP サーバーを提供したい。外部 API はコストがかかるのでローカル実行モデルで
やりたい」という相談から開始。

**手段の比較** — whisper.cpp（CGO 静的リンク）／Apple SpeechAnalyzer（macOS 26）／
MLX Whisper・faster-whisper（Python）／parakeet 系を比較。whisper.cpp を本命としたのは
「image-forge で stable-diffusion.cpp を CGO 静的リンクした経路がそのまま使え、
プロジェクト最大のビルドリスクが既に解決済み」であることが決め手。Apple
SpeechAnalyzer は利点を認めたうえで、instant-translate ↔ quick-translate と同じ
「軽量姉妹」構図で後から 2 本目として足す余地を残す形で保留とした。

**モデル調査** — kotoba-whisper v2.x が公式に GGML 形式で配布されており（`kotoba-tech/
kotoba-whisper-v2.0-ggml` ほか）、large-v3 比 6.3 倍速で WER 同等であることを確認。
日本語主体の用途で速度・精度の大きなマージンがあるため、単一モデル案を退けて
カタログ＋プロファイル方式を採用した。

**確定した選択（ユーザー判断）**

| 論点 | 決定 | 補足 |
|---|---|---|
| ツール名 | `voice-scribe` | 当初 `voice-scribe-mcp` を選択したが、CLI 同居が決まった時点で `-mcp` を落とした。image-forge と同型の区別（CLI と MCP の両方を持つツールは `-mcp` を付けない）が org 内で一貫する |
| 言語スコープ | 多言語対応・既定は日本語最適 | voice-studio-mcp の「日本語専用」とは異なる方針。gem-transcribe の多言語出力と整合させるため |
| 話者分離 | **v1 から搭載** | engine が 2 本立てになるリスクを提示したうえでユーザーが採用を判断。Phase 2a として独立レビュー可能に切ることでリスクを管理する |
| バイナリ形態 | CLI + `mcp` サブコマンド同居 | E2E 検証とトラブルシュートが MCP クライアントを介さず行える |
| 音声デコード | **AVFoundation を CGO で叩く** | ffmpeg 外部依存を退け、単一バイナリ完結を優先。darwin 専用リリースなので移植性の損失なし。Objective-C ブリッジのコストは Phase 1 のリスクとして受容 |
| MCP 返却方式 | 二段（閾値以下はインライン） | 成果物がテキストである点を理由に、画像・音声系 MCP の file-mediated 原則を一律には適用しない |

**出力スキーマの確定** — gem-transcribe の `models.py` を実地に確認したところ、
`Segment.text` が**言語コード → テキストのマップ**（`--lang=en,ja` で原文＋翻訳を
同一セグメントに保持する形）であることが判明。whisper の translate タスク（→ 英語）が
この形にそのまま収まるため、当初「方針として揃える」程度だった互換性を、
**エンベロープ完全互換**まで引き上げた。これにより meeting-notes 等の後段は
クラウド版・ローカル版のどちらの出力も同一パーサで読める。

**検討したが採らなかった案**

- 単一モデル（large-v3-turbo のみ）でカタログ機構を省く案 — 日本語の 6 倍速マージンを
  捨てることになるため却下
- `models pull` を MCP ツールとして露出する案 — 数 GB のダウンロードをエージェントの
  判断で開始させないため却下。CLI 専用とした
- 常に file-mediated で返す案（voice-studio / video-studio / image-forge と完全同一の
  契約）— 一貫性は最高だが、短い音声でもエージェントに必ずファイル Read の往復を
  強いるため却下
