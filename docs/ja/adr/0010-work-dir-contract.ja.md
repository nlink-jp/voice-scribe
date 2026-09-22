# ADR-0010: work dir は呼び出しごとの `work_dir` で受け取り、既定ルートを持たない

- Status: Accepted —— その実装（§3 の検査と Amendment のブラックリスト）は ADR-0013（nlink-jp/pathguard）で
  置き換えた
- Date: 2026-09-13

## Context

組織 ADR-021（ファイル渡し MCP サーバーの work dir 契約）の**参照実装**として、
このリポジトリが最初に契約を実装する。9 サーバーのうち 6 つが
「work dir + `workspace_id`」型で、その中で本サーバーが最小（対象ツール 1 本）、
かつ移植元となる `workspace.Manager.EnsureIn` の本体がここにあるため。

現状の問題は 2 つある。

**1. 綴りと意味が散っている。** フリート全体で同じ引数が `workspace_root` /
`workspaceRoot` / `workspace_dir` の 3 綴りで存在し、役割も「workspace を作る
root」「直接書き込む dir」「入力相対パスの基点」の 3 種が混在している。モデルは
サーバーごとに別の名前を覚える必要があり、strict decode はその取り違えを
（正しく）拒否する。

**2. 省略時に黙ってサーバー既定へ落ちる。** 本サーバーの `workspace_root` は
省略可で、省略すると `~/.local/share/voice-scribe/mcp-workspaces` に書く。
呼び出し側のファイルツールはそこを開けないので、**ジョブは成功し、返ったパスは
開けない**という形でしか失敗が現れない。

4 ランタイム（Claude Code / ChatGPT Codex / gem-agent / lagent）の実測では、
MCP の `roots` は Codex が capability を宣言せず空配列を返し、Claude Code は
scratchpad を含まないプロジェクト dir しか返さない。環境変数は Codex が剥がす。
**呼び出しごとの引数だけが 4 ランタイム共通の経路**であり、既定値を持つ設計は
「運用者が書いた場所が、呼び出し側にたまたま読めるか」に賭けているに等しい。

## Decision

### 1. 引数は `work_dir`、`transcribe` では必須

`workspace_root` は廃止し、`work_dir` に統一する。意味は
**「呼び出し側が読み戻せる絶対パス」**。`transcribe` は入力の録音を
`work_dir` 配下から解決し、文字起こしも配下に書くので、条件付きではなく
`required` に置く。`workspace_id` は据え置き（`work_dir` の中の作業単位）。

### 2. 解決順は 引数 → `_meta` → エラー

`params._meta["jp.nlink/work_dir"]` を第 2 経路として読む。自前ランタイム
（gem-agent / lagent）が全 `tools/call` に付けられる、スキーマ非依存の経路で、
サーバー側はツールごとの実装を持たずに受けられる。どちらも無ければ
`work_dir_required` を返す。**サーバー既定は無い。**

### 3. 検証は閉じた一覧

`absolute` / `~` 無し / `..` 無し / 存在する dir / 書込可 / システム位置でない。
それぞれ `work_dir_invalid`・`work_dir_not_found`・`work_dir_not_writable`・
`work_dir_denied` を返す。既存の `path_not_allowed` から分けるのは、
「渡し忘れ」「場所が無い」「拒否した」を呼び出し側が区別して直せるようにするため。

**dir は作らない。** 呼び出し側の work dir は必ず既に存在するので、
存在しないパスは打ち間違いであり、黙って作ると「返ったが開けない」に戻る。

### 4. 既定ワークスペースルートを削除する

`defaultWorkspaceRoot()` と `Manager` の既定ルート（`Ensure` / `Root` /
`List` / `Delete`）を削除する。到達不能な既定は、次に誰かが「省略時の
フォールバック」として復活させるための足場でしかない。

### 5. 結果は行き先を反響する

`transcribe` / `check_job` の結果に解決済みの `work_dir`（絶対パス）を載せる。
`_meta` 経由で work dir を注入された呼び出し側は、自分のリクエストではなく
結果からしか行き先を知れない。

### 6. 強制はテスト

`work_dir` を露出しないツールが無いこと、旧綴りがスキーマに残っていないこと、
`additionalProperties:false` であることをツールスキーマ走査のテストで固定する。
散文の規約は次にツールを足す人が再決定してしまう。

## Consequences

- **破壊的変更。** `workspace_root` を送る呼び出しは strict decode に弾かれ、
  `work_dir` 省略は `work_dir_required` になる。どちらも名前を告げる大声の
  失敗で、黙って既定へ落ちるより良い。mcp-tactics 側の更新が対になる。
- 既定ルート削除で `Manager` は「呼び出し側 dir の下に workspace を作る」
  だけの型になる。ワークスペース一覧・削除ツールを将来足すなら、
  `work_dir` を受け取る形で作り直すことになる。
- 本リポの `internal/mcp/workdir` が他 8 サーバーへの移植元になる。

## Amendment (2026-09-13): 読み取りは `work_dir` の外も許す

組織 ADR-021 §7 が確定し、`allowed_paths` 方式の廃止とともに「**読みはブラックリスト
以外どこでも、書きは `work_dir` 配下だけ**」になった。本サーバーもこれに合わせる。

- `audio` は**絶対パスを受け付ける**。その場で読み、コピーしない。1 時間の録音を
  ワークスペースへ複製してから文字起こしするのは純粋な無駄であり、呼び出し側は
  そのファイルを自分でも読めるからである。相対パスは従来どおりワークスペース相対で、
  `os.Root` によるカーネル封じ込めが効く
- 拒否するのは資格情報・エージェント制御ファイルの位置（`~/.ssh`、`~/.aws`、
  `~/.gnupg`、`~/.config/gcloud`、`~/Library/Keychains`、`~/.claude`、`~/.codex`、
  任意の `.env`）。**照合はパスの両方の綴り（渡されたまま／symlink 解決後）× 項目側の
  両方の綴り**で行う —— pcap-analyzer が実機で見つけた穴で、解決してから照合すると
  `~/.ssh/config` のようなリンク経由のパスが素通りする
- 出力は従来どおり `work_dir` 配下のみ。ブラックリストは床であって境界ではない

## References

- 組織 ADR-021（ファイル渡し MCP サーバーの work dir 契約）
- ADR-0003（MCP サーバー）: workspace と os.Root 封じ込めの前提
