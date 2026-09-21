# ADR-0001: whisper.cpp を CGO で静的リンクする

- Status: Accepted
- Date: 2026-08-08

## Context

voice-scribe はローカルで音声を文字起こしする。推論ランタイムをどう抱えるかで、
配布形態・ビルドの複雑さ・実行時の依存が決まる。RFP の Phase 1 では、これを
**プロジェクト最大のリスク**と位置づけ、他のいかなる実装よりも先に疎通を確認する
と決めていた。

選択肢は 3 つあった。

1. whisper.cpp（ggml/Metal）を CGO で**静的リンク**し、単一バイナリにする
2. Apple の SpeechAnalyzer / SpeechTranscriber（macOS 26）を Swift から使う
3. Python 実装（MLX Whisper / faster-whisper）を子プロセスとして起動する

## Decision

**(1) を採る。** whisper.cpp を submodule として抱え、cmake で静的ライブラリ化し、
CGO 経由で単一バイナリにリンクする。ランタイムは `cgo_whisper` ビルドタグの下でのみ
リンクされ、タグ無しのビルド（`make build`）では engine が `ErrNoRuntime` を返す。

決め手は、image-forge が stable-diffusion.cpp で同じ経路（CGO × ggml × Metal ×
Developer ID 署名 × notarize）を完走済みで、**同じ ggml の上に載っている whisper.cpp
には、その成果がほぼそのまま転用できる**ことだった。プロジェクト最大のビルドリスクが、
実質的に既知の解決済み問題になる。

(2) は利点が本物（オンデバイス・モデル管理不要・省電力）だが、MCP サーバーを Swift で
書くことになり org 内に前例がない。**却下ではなく保留**とし、将来の「軽量姉妹」として
余地を残す。(3) は利用者に Python ランタイムとモデル配置を露出させるため却下。

### ビルドタグで実体と stub を切り替える

`make build` は runtime 抜きのバイナリを作り、`make build-engine` が実体をリンクする。
これは image-forge と同じ構えで、次の 2 つを同時に満たす:

- cmake も Metal ツールチェインも無い環境で scaffold 作業ができる
- ランタイムの上に載る純 Go の層を、1.6 GB のモデルをディスクに置かずにテストできる

## Consequences

### 実測（2026-08-08、M2 Max / macOS 26 / Go 1.26.5 / cmake 4.4.2）

疎通スパイクで確認した事実:

| 項目 | 結果 |
|---|---|
| バイナリ | **6.0 MB、単一 Mach-O arm64** |
| 動的依存 | Metal / MetalKit / Foundation / Accelerate / CoreFoundation / libc++ / libSystem / libobjc — **すべて OS 標準**。第三者 dylib はゼロ |
| Metal 有効化 | `whisper_print_system_info()` が `MTL : EMBED_LIBRARY = 1` を報告。GPU は `Apple M2 Max` / `MTLGPUFamilyApple8` を認識 |
| Metal ライブラリ初期化 | **初回 8.797 秒 → 2 回目以降 0.011 秒** |
| プロセス全体（warm） | 0.05 秒 |
| stdout | **汚染なし**（下記） |

### 静的アーカイブのリンク順

依存する側を先に並べる必要がある。順序を誤ると configure ではなくリンク時に
undefined symbol として出る:

```
libwhisper.a → libparakeet.a → libggml.a → libggml-cpu.a
             → libggml-metal.a → libggml-blas.a → libggml-base.a
```

**`libparakeet.a` は image-forge の sd.cpp には無かったアーカイブ**で、現在の
whisper.cpp が同梱している。submodule を更新した際にアーカイブ構成が変わる可能性が
あるので、リンクエラーが出たら `find build -name '*.a'` で実体を確認すること。

### Metal の 8.8 秒はプロセス毎のコストではない

初回の 8.797 秒は Metal シェーダのコンパイルで、OS がキャッシュする。2 回目以降は
0.011 秒。したがって **「Metal cold-load があるから常駐が要る」という RFP の記述は
不正確**で、常駐の価値はモデルのロード時間（Phase 1 で実測する）にある。ここは
ドキュメントに書く前に測り直すこと。

### ggml は遅延初期化される

ggml のバックエンド登録はプロセス起動時ではなく、ランタイムを最初に呼んだ時に走る。
実測で `voice-scribe --version` の stderr は **0 行**だった。`brew test` が
`--version` を叩くので、この性質は都合がよい。

### stdout は汚れていないが、防御は入れる

`1>file 2>file` で分離実測した結果、ggml の初期化ログ 16 行は**すべて stderr** に出て、
stdout には自分の出力しか無かった。それでも image-forge v0.9.0 の教訓に従い、
Phase 2b の MCP 実装では次の 2 層を入れる:

1. 発生源 — `whisper_log_set` / `ggml_log_set` でログコールバックを差し替える
2. transport — 本物の stdout を dup して専用にし、fd 1 を stderr へ向ける

「今は漏れていない」は上流の実装詳細に依存した観測にすぎず、契約ではない。

### 受け入れる代償

- **クロスコンパイル不可。darwin/arm64 単一リリース。** Metal に Linux/Windows/amd64
  のターゲットが無いため。RFP で意図的に決めたスコープ
- ビルドに cmake と Metal ツールチェインが要る（`make deps`）
- 第三者の MIT コードを静的リンクするので、README に帰属表記が必要
  （whisper.cpp / ggml とも MIT © 2023-2026 The ggml authors。上流の LICENSE を
  実地に確認すること — 年次は更新される）
- `go test ./...` は**そのまま使える**。whisper.cpp の Go バインディングは
  `bindings/go/go.mod` を持つ入れ子モジュールなので、`./...` の展開から自然に外れる
  （image-forge が sd.cpp で `PKGS` フィルタを必要としたのは、あちらの libwebp swig
  バインディングが入れ子モジュールになっていなかったため）。Makefile の `PKGS` は
  上流が go.mod を落とした場合の保険として残してあるだけで、現状は `./...` と等価
