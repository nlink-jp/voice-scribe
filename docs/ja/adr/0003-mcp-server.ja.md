# ADR-0003: MCP サーバーの実装判断

- Status: Accepted
- Date: 2026-08-08

## Context

RFP は MCP サーバーを Phase 2b と位置づけ、ツール 4 本・非同期ジョブ・`workspace_root`
＋ `os.Root` 封じ込め・二段返却を決めていた。骨格は data-toolbox-mcp → video-studio-mcp
→ image-forge と 3 回移植されているものを流用する。

ここには**実装中に決めたこと**だけを記録する。RFP で既に決まっていた事項（ツール構成、
二段返却の方針、`models pull` を露出しない判断）はそちらを参照。

## Decision

### 骨格は image-forge から丸ごと移植し、ドメイン語彙を書き換えた

`jsonrpc` / `transport` / `mcpserver` / `toolerr` / `workspace` / `job` をコピーし、
モジュールパスを書き換えた。**あわせて移植元のドメイン語彙も全部書き換えた** —
「generation project」「init/mask images」「rendered PNGs」といった記述が残っていると、
次に読む人間が画像生成サーバーのつもりで読むことになる。エラーコードも
`render_failed` → `transcribe_failed` ほか、この領域の失敗に合わせて張り替えた。

### セッションはリクエストごとに開いて閉じる（常駐しない）

RFP の Phase 2c は「常駐エンジンのモデル切替（reload key）」を挙げていたが、**採らない**。

ジョブは単一 worker で直列化されるので、常駐で得られるのは「連続呼び出しの間のリロードを
省ける」ことだけである。その対価は、何時間もアイドルしうるサーバーが 500MB 超を保持し
続けることになる。**リロードが実際に問題だという実測が出てから**入れればよい。

### 二段返却の閾値を超えてもファイルは必ず書く

インライン返却するかどうかは「テキストも一緒に返すか」だけを決める。**閾値がファイルの
存在有無を変える設計にはしない** — 残すと決めたエージェントが取り直しを強いられるし、
チューニング項目が成果物の有無を左右するのは驚きが大きい。

抜粋は**ルート境界で切る**。日本語テキストをバイト単位で切ると置換文字になり、
それはまさにこのツールの主たる利用者層の文字である。

### stdout の隔離は「静かなサーバー」では検証にならない

`claimStdout()` が本物の stdout を dup して transport 専用にし、fd 1 を stderr に向ける。
発生源側（`engine.SetLogHandler`）と合わせて 2 層。

**検証で一度誤りかけた**: 実 E2E で stdout が JSON のみ・stderr が 0 行だったので
「隔離が効いている」と読みかけたが、これは**ログハンドラが info レベルを落としている
だけ**で、隔離層が働いた証拠ではない。機構そのものを直接叩くユニットテストに置き換えた
（protocol ハンドルに書けば transport に出る／`os.Stdout` に書けば stderr に出る）。

**教訓: 防御が二重にあるとき、片方だけで説明がつく観測を両方の証拠にしてはいけない。**

### ランタイムログは警告以上だけを stderr へ

ggml の初期化ログ 16 行は毎回出るが、MCP クライアントの stderr を埋める価値はない。
モデルのロード失敗などはエラーレベルで出るので素通しされる。

## Consequences

### 実測（2026-08-08、実 stdio クライアント）

```
server:       voice-scribe
instructions: present
tools:        get_usage, transcribe, check_job, list_models
usage:        5804 bytes of markdown
transcribe -> job_id -> check_job -> done
model: kotoba-whisper-v2.2 | segments: 6 | speakers: [田中, 佐藤] | returned: inline
```

stdout の全行が JSON として parse できることを、実際に転記を走らせた状態で確認した。

### usage.md は機械検査で腐敗を防ぐ

クライアントがこのサーバーを操作する前に読む唯一の文書なので、コードとの乖離は
実害である。テストで次を固定した:

- 登録されている全ツールが usage.md に出てくる
- 返しうる全エラーコードが回復表にある
- `transcribe` のスキーマにある全引数が usage.md に出てくる
- 全ツールのスキーマが `additionalProperties:false`（引数タイプミスを黙って無視しない）

### 受け入れる代償

- 連続呼び出しのたびにモデルをロードし直す（上記のとおり意図的）
- ジョブは永続化しない。再起動後の `job_id` は `job_not_found` で、再投入が回復手順
- `models pull` は MCP から使えない。エージェントは CLI 手順を案内される
