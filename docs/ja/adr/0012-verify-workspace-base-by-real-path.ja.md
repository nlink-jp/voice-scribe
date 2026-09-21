# ADR-0012: workspace の base dir は実パスで検証する — `os.Root` は自分のアンカーを保証しない

- Status: Accepted
- Date: 2026-09-21

## Context

ADR-0003 以来、このサーバーのパス封じ込めは**カーネルに任せる**方針だった。
ワークスペース相対のパスは `os.Root` 越しに開き、文字列照合はしない。ADR-0010 で
`work_dir` が呼び出しごとの必須引数になり、ワークスペースは `<work_dir>/<workspace_id>`
という位置に固定された。

`os.Root` が保証するのは **root の「中」の操作**である。root のパス自身は通常どおり
解決される。ここに穴があった。

旧 `EnsureUnder` は `filepath.Join(work_dir, id)` で base を組み立て、`os.Mkdir` し、
`fs.ErrExist` を無視し、その base を `os.OpenRoot` に渡していた。つまり
**`<work_dir>/<id>` という名前に先回りして symlink が置かれていると、封じ込めの
アンカーがリンク先に着地する。**以後の読み書きは文字起こしも含めて全部 `work_dir` の
外へ落ち、しかも**呼び出しは成功を報告する**。

リンクを置けるのは誰か。`work_dir` に書けるもの全部である —— 同じ work dir を使う
別のツール、サンドボックス内で動くコード、以前のジョブの残骸。`work_dir` 自体は
呼び出し側が請け合った道だが、**その下にある名前まで請け合ったわけではない。**

実測: symlink された base に対する `os.OpenRoot` は、エラーを一切返さずリンク先へ書く。

この失敗の形は、ADR-0010 が既定ワークスペースルートを消して潰したものと同じである
——「ジョブは成功し、返ったパスは意図した場所ではない」。ADR-0010 は既定値という
入口を塞いだが、この経路は残っていた。

そして封じ込めだけでは足りない理由がもう一つある。**`BaseDir` はこの後、root の外の
コードに渡る。** `os.OpenRoot` に渡され、録音については音声デコーダに渡される。
どちらも自分でパスを解決する。

## Decision

### 1. 作成は `work_dir` の `os.Root` 経由で行う

`makeBaseDir` は `os.OpenRoot(work_dir)` を開き、`root.Mkdir(id, 0o755)` で作る。
root から出るパスは root 自身が拒否するので、**作成の時点でリンクを辿れない。**
`MkdirAll` ではなく `Mkdir` なのは ADR-0010 のままの理由による —— work dir は
呼び出し側のもので既に存在するはずであり、親が無いのは呼び出し側の打ち間違いである。

### 2. 作った後に実パスで照合する

`filepath.EvalSymlinks(work_dir)` に `id` を join したものと、
`filepath.EvalSymlinks(filepath.Join(work_dir, id))` を比較する。

**この照合が本体である。**作成を root 経由にしただけでは足りない。`BaseDir` は
この後 root の外のコードへ渡るので、「作った物が意図した場所にあるか」を
**渡す前に一度だけ直接問う**必要がある。

### 3. 不一致は `path_not_allowed` で拒否し、名指しする

エラーは workspace id と**実際の解決先**を告げる。`workspace_failed` ではなく
`path_not_allowed` に寄せるのは、呼び出し側がこれを「自分の指定した場所の問題」として
直せるようにするためで、ADR-0010 がエラーコードを分けた理由と同じである。

### 4. 両辺を解決してから比べる

片側だけ `EvalSymlinks` すると、攻撃と無関係な理由で不一致になる。macOS の
`t.TempDir()` は `/var` の下に出るが `/var` 自体が `/private/var` へのリンクであり、
未解決の `work_dir` は解決済みの base と必ず食い違う。**比較は解決後どうしで行う。**

### 5. `work_dir` 自体は検証しない

呼び出し側が請け合った 1 本の道であり、ADR-0010 の検証（絶対・`~` 無し・`..` 無し・
存在する dir・書込可・システム位置でない）を既に通っている。ここで守るのは
**その下の `<id>`** である。

### 6. テストで固定する

`TestEnsureUnderRefusesLinkedWorkspaceDir` が、リンクを置いた work_dir に対する
`EnsureUnder` が `path_not_allowed` で落ち、**リンク先に何も作られていない**ことを
要求する。この検査は**未修正のソースに対して落ちることを確認済み** —— 修正前は
リンク先に `output/` が作られ、エラーはどこにも出ずにワークスペースが成功する。

## Consequences

- 正常な呼び出しは何も変わらない。実ディレクトリなら実パスは一致する。
- ワークスペース作成 1 回あたり `EvalSymlinks` が 2 回増える。ジョブ 1 本の
  文字起こしに対して無視できる。
- `path_not_allowed` の意味が広がった（従来は「パスがワークスペースの外」だけ）。
  `internal/mcp/tools/usage.md` のエラー表を同じコミットで更新済み。モデルが読む面が
  仕様の一部である以上、ここを置き去りにできない。
- 拒否は**作成時に 1 度**起きる。アンカーは一度決まれば以後の全ての読み書きに効くので、
  入口で断るのが最も安く、最も早い。
- data-toolbox-mcp が同じ形を先に持っている（`internal/workspace/manager.go` の
  `makeWorkDir`、bind mount を podman に渡すため）。ただし**あちらに ADR は無く、
  拒否は素の `fmt.Errorf` で安定コードを持たない。**この形を他サーバーへ移植するときは、
  拒否を各サーバーの `toolerr` 側に寄せること。
- 同型の穴は「**root の外へパスを渡す箇所**」に現れる。`os.Root` を使っているという
  事実は、その外へ出た瞬間の保証を何も含まない。

## Alternatives considered

| 案 | 不採用の理由 |
|---|---|
| `os.Root` の封じ込めだけに頼る | `os.Root` は root の中の操作を封じ込めるが、root パス自身は通常どおり解決する。実測で、symlink された base に対して `os.OpenRoot` はエラー無しでリンク先へ書いた |
| `os.Lstat` で symlink かどうかだけ判定する | TOCTOU。判定してから使うまでの間に置き換えられる。中間要素がリンクの場合も見ない。実パス比較は「作った物が意図した場所にあるか」を直接問うので、両方に当たる |
| 文字列の prefix 一致（`strings.HasPrefix`）で確かめる | 解決前の綴りを比べても何も言えない。ADR-0003 が捨てた文字列照合そのものに戻る |
| base がリンクなら、その先を `work_dir` とみなして続行する | 呼び出し側が請け合ったのは `work_dir` であってリンク先ではない。成功を報告しながら別の場所に書くこと自体が元の欠陥であり、それを仕様にすることになる |
| 作成時ではなく読み書きのたびに検証する | アンカーは作成時に 1 度決まり、以後の全ての操作に効く。入口で 1 度断れば足りるものを、毎回の `EvalSymlinks` に変える理由が無い |
| `work_dir` も同じ照合に掛ける | 呼び出し側が請け合った唯一の道であり、ADR-0010 の検証を既に通っている。ここを疑い始めると、呼び出し側が指定できる場所が無くなる |

## References

- ADR-0003（MCP サーバー）: `os.Root` によるカーネル封じ込めを前提に置いた記録
- ADR-0010（work dir 契約）: `<work_dir>/<workspace_id>` の構造と、
  「成功したが呼び出し側が読めない」という失敗の形
- 組織 ADR-021（ファイル渡し MCP サーバーの work dir 契約）
- `internal/mcp/workspace/manager.go`: `makeBaseDir`
- `internal/mcp/workspace/containment_test.go`: `TestEnsureUnderRefusesLinkedWorkspaceDir`
- data-toolbox-mcp `internal/workspace/manager.go`: `makeWorkDir`（同型、bind mount 向け）
