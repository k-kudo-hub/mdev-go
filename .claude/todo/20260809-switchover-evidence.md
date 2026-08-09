# 実環境 hooks 切り替え: 調査・判断の記録

このタスク(`.claude/todo/20260809-add-real-env-hook-switchover.md`)で調べた事実と、
その結果として採った設計判断を着手順に記録する。

## 0. 安全のための前提

- 実環境の `~/.claude/settings.json` と `~/.claude-conductor/` は本作業中は一切変更しない。
  自動テストはすべて `t.TempDir()` 配下の fixture に対して行う。
- `make install` の検証も `CONDUCTOR_HOME` を一時ディレクトリへ向けて行う。
- 実際の切り替え(`mdev hooks switch`)はユーザーテストでユーザー自身が実行する。

## 1. 置換対象の実体(実測)

`/Users/kazuto/projects/claude-conductor/hooks.json` を読み、install.sh:115-129 が
`jq '.hooks = (.hooks // {}) + $hooks'` で `~/.claude/settings.json` にマージすることを確認した。
`.hooks` 配下に現れる conductor スクリプトの呼び出しは次の 4 箇所である。

| イベント | コマンド文字列 | 切り替え後 |
|---|---|---|
| `Notification` | `${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh` | `.../bin/mdev hook notify` |
| `Stop` | 同上 | `.../bin/mdev hook notify` |
| `PostToolUse` | `${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-post-tool.sh` | `.../bin/mdev hook post-tool` |
| `UserPromptSubmit` | `${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-resolve.sh` | `.../bin/mdev hook resolve` |

`Notification` / `Stop` にはもう 1 つ terminal-notifier のインラインコマンドがあるが、
通知の責務で hook 処理と独立しているため触らない。

### 前置き(`${CONDUCTOR_HOME:-$HOME/.claude-conductor}`)を維持する理由

mdev-test は `CONDUCTOR_HOME` を worktree 配下へ向けることで本番環境から隔離する。
絶対パスへ展開してしまうとこの隔離が hooks に効かなくなるため、
**置換はコマンド文字列の末尾だけを対象にし、前置きはそのまま残す**。

実装上は「`/scripts/pending-notify.sh` で終わる文字列の、その接尾辞だけを
`/bin/mdev hook notify` に差し替える」という規則にした。これにより
`${CONDUCTOR_HOME:-...}` 形式でも、ユーザーが絶対パスに書き換えていた場合でも、
前置きを壊さずに切り替えられる。

## 2. JSON の未知キー・キー順・インデントの保全方法

### 検討した選択肢

1. `map[string]any` へ Unmarshal → Marshal
   - Go の map はキー順を持たないため `encoding/json` はキーをアルファベット順に並べ替える。
     現行 jq(キー順を保持し、インデント 2 で出力する)と一致せず、
     ユーザーの settings.json のキー順が壊れる。**不採用**。
2. 生バイト列に対する単純な文字列置換
   - キー順・インデントは完全に保たれるが、`.hooks` の外(`permissions` 等)に
     同じ文字列があった場合まで書き換えてしまう。指定「`.hooks` 内のみ」に反する。**不採用**。
3. **採用: 該当する文字列トークンのバイト範囲だけを差し替える**
   - `encoding/json` の `Decoder.Token()` で全体を 1 回走査し、コンテナのフレーム
     スタック(オブジェクト/配列・直近のキー・次がキーかどうか)を持つ。
   - `Decoder.InputOffset()` は「直前のトークンの終端 = 次のトークンの始端」を返す。
     文字列トークンでは、その位置から最初に現れる `"` が開き引用符、
     読み終えた直後の `InputOffset()` が閉じ引用符の直後なので、
     トークンのバイト範囲が正確に求まる。
   - 集めた範囲を**後ろから前へ**差し替える(前方の範囲がずれない)。
     **再シリアライズを一切行わない**。

### 結論

採用案 3 では JSON を書き戻さないため、キー順・インデント・空白・改行・
未知キー・触っていない hook コマンドの表記(エスケープの仕方を含む)がすべて
**バイト単位でそのまま保存される**。したがって
「jq 相当の正規化された出力」という妥協は不要で、
差分は置換した 4 箇所の文字列リテラルのみになる。

置換後に入れる文字列は `json.Encoder` に `SetEscapeHTML(false)` を設定して
エンコードする。既定の `json.Marshal` は `<` `>` `&` を `<` などへ
エスケープするため、コマンド文字列にこれらが含まれていた場合に
元の表記と無用な差が出る。それを避けている。

`json.Valid` を先に通してから走査するため、壊れた JSON・空入力・
トップレベルの値の後ろに余分なデータがある入力はすべてエラーになる
(テストで 5 パターンを固定した)。

### 副次的な確認

対象は「`.hooks` 配下のオブジェクトの `command` キーの**値**」に限定した。
判定の条件は 3 つで、いずれもフレームスタックから決まる。

1. スタックの底(トップレベル)のオブジェクトの現在のキーが `hooks`
2. その 1 つ内側がオブジェクト(= イベント名のオブジェクト。イベント名は
   ここのキーとして取れるので、変更一覧の表示に使う)
3. 直近のフレームがオブジェクトで、現在のキーが `command`

これにより、イベント名や `matcher` / `type` といった**キー**の位置に対象文字列が
現れても、`command` 以外のフィールドの値であっても置換されない。
`.hooks` の外(`permissions` など)も同様に対象外になる。

## 3. hook の終了コード方針

Claude Code 公式ドキュメント <https://code.claude.com/docs/en/hooks> の
"Exit code behavior" を確認した(2026-08-09 時点)。

> **Exit 0** means success. Claude Code parses stdout for JSON output fields. JSON output is only processed on exit 0.
> **Exit 2** means a blocking error. Claude Code ignores stdout and any JSON in it. Instead, stderr text is fed back to Claude as an error message.
> **Any other exit code** is a non-blocking error for most hook events. The action proceeds, and the transcript shows a `<hook name> hook error` notice followed by the first line of stderr.

イベント別の exit 2 の扱い:

| Hook event | Can block? | exit 2 で起きること |
|---|---|---|
| `Notification` | No | stderr をユーザーにのみ表示 |
| `Stop` | Yes | Claude の停止を妨げ、会話を継続させる |
| `PostToolUse` | No | stderr を Claude に見せる(ツールは実行済み) |
| `UserPromptSubmit` | Yes | プロンプトの処理をブロックし、プロンプトを消す |

### 結論: 現状の exit 1 を維持する

mdev の hook は pending ファイルとレジストリの更新という**補助的な副作用**であり、
失敗しても会話を止めるべきではない。`Stop` で exit 2 を返すと会話が止まらなくなり、
`UserPromptSubmit` で exit 2 を返すとユーザーの入力が消える。どちらも
「pending が書けなかった」ことへの反応として過大で、実害が大きい。
exit 1 は「その他の終了コード」に該当し非ブロッキングで、
stderr の 1 行目が transcript に出るため失敗に気付くこともできる。
`internal/cli/root.go` の `exitError = 1` をそのまま使う。

## 4. CLI の設計上の判断

### `mdev hook`(単数)と `mdev hooks`(複数)

`hook` は Claude Code が発火させる側、`hooks` は利用者が Claude Code の設定を
書き換える側で、責務が違う。名前が紛らわしいので、取り違えていないことを
確かめるテスト(`TestHookAndHooksAreDistinctCommands`)を置いた。

### 対象ファイルの差し替え口 `MDEV_SETTINGS_FILE`

Claude Code のユーザー設定は公式ドキュメント
<https://code.claude.com/docs/en/settings> で `~/.claude/settings.json` に固定と
明記されており、置き場所を変える環境変数は存在しない(`CLAUDE_CONFIG_DIR` の
記載も無い)。

そのままだと `mdev hooks switch` は必ず実環境のファイルを触ることになり、
ユーザーテストの前に安全な予行演習ができない。そこで mdev 側の逃げ道として
`MDEV_SETTINGS_FILE` を用意し、指定があればそのファイルを対象にする
(`store.SettingsPath`)。既存の `CONDUCTOR_HOME` の扱いと同じ形にしてある。
手順書ではまずコピーに対して switch → restore を試す手順を先に置いた。

### restore が復元前のバックアップを作らない理由

復元前に現在の内容を退避すると、それが「最新のバックアップ」になり、
次の restore が切り替え後の内容を復元してしまう。復元は常に
「switch が作った最新のバックアップ」へ戻す一方向の操作にした。

### switch が変更なしのときにバックアップを作らない理由

同じ理由である。切り替え済みの状態で switch を再実行したときにバックアップを
作ると、その内容は切り替え後のものになり、restore が機能しなくなる。
これにより「同じ秒に 2 回バックアップを作ってファイル名が衝突する」経路も
実質的に塞がれる。

### settings.json のパーミッション

`writeFileAtomic` は 0644 固定だったため、権限指定版
`writeFileAtomicMode` を足して既存ファイルのパーミッションを引き継ぐようにした。
利用者が 0600 に絞っている設定ファイルを mdev の都合で緩めないためである。

## 5. `make check` で判明したこと

### ADR-0002 違反(go-arch-lint が検出)

cli のテストで `domain.HookCommandChange` を組み立てていたため、
`Component cli shouldn't depend on internal/domain` で落ちた。

`app.SwitchHooksResult.Changes` の要素型が domain の型のままだったのが原因である。
cli / tui は app にしか依存できない(ADR-0002)ので、境界に出す型は app が持つ
ことにし、`app.HookCommandChange` を定義してユースケースの中で移し替えた
(`toHookCommandChanges`)。`app.HookEnv` / `app.RecordEnv` と同じ扱いである。

型エイリアス(`type HookCommandChange = domain.HookCommandChange`)でも
import は消えるが、go-arch-lint は import しか見ないため、それは
ガードレールをすり抜けるだけで ADR の意図には反する。採らなかった。

### 最終結果

```
gofmt: no diff
golangci-lint: 0 issues
go-arch-lint: OK - No warnings found
go test -race: 全パッケージ ok
Total test coverage: 93.8% (859/916)  ... internal/domain 97.3% / internal/app 100.0%
go build: OK
```

## 6. 実環境を変更していないことの確認

- 自動テストの settings.json はすべて `t.TempDir()` 配下に作る
  (`internal/infra/store/settings_test.go` の `newSettingsFile`)。
  実環境のパスを組み立てるのは `cmd/mdev/main.go` だけで、そこにテストは無い。
- 手動の動作確認は `mktemp -d` に置いたコピーへ `MDEV_SETTINGS_FILE` を
  向けて行った。
- `make install` の確認は `CONDUCTOR_HOME=$(mktemp -d)/.claude-conductor` で行い、
  実行後に `ls ~/.claude-conductor/bin` が
  `No such file or directory` であることを確認した。
- `~/.claude/settings.json` の更新時刻は作業開始時のまま
  (`2026-08-08 22:37:52` = このタスクの着手より前)で、
  mdev のバックアップも 1 つも作られていない。

```sh
$ stat -f '%Sm %N' -t '%F %T' ~/.claude/settings.json
2026-08-08 22:37:52 /Users/kazuto/.claude/settings.json

$ ls ~/.claude/settings.json.mdev-backup-*
no matches found

$ ls ~/.claude-conductor/bin
ls: /Users/kazuto/.claude-conductor/bin: No such file or directory
```

## 7. `/code-review` 指摘対応(PR #4)

PR #4 に対するコードレビューの指摘を検証し、対応した。指摘単位で 1 コミットにしている。

### 7-1. restore を逆向きの外科的書き換えにする(最重要)

**指摘**: `Restore` がバックアップの**全文**を書き戻すため、

1. switch 後に Claude Code 自身が `settings.json` へ書いた変更
   (`permissions.allow` の追加が典型)が silent に消える
2. `settings.json` が欠損・読めないとき `Read` エラーで失敗し、
   「設定ファイルごと失った」という復旧の主目的シナリオで使えない

**検証**: 実ファイルで再現した。切り替え後に `permissions.allow` へ
1 要素足してから `restore` すると、旧実装ではその要素が消える。

**対応**: 置換規則を `from` / `to` の対称な形にし、走査(`scanHookCommands`)と
編集(`applyEdits`)の機構をルールセットのパラメータで共用した上で、
規則を反転した `RestoreHookCommands` を足した。`Restore` の流れは

1. `Read` 成功 → 現在の内容へ逆置換。変更が無ければ no-op を報告し、
   あれば原子的に書き込む(hooks 以外の差分は 1 バイトも触らない)
2. `Read` が `fs.ErrNotExist` → 最新のバックアップの全文で書き戻す
   (この経路だけ)。バックアップも無ければエラーにせず状態を報告する
3. その他の `Read` エラー → エラー

`SettingsStore.Read` の契約に「不存在は `errors.Is(err, fs.ErrNotExist)` で
判別できること」を明記した。実装は `os.ReadFile` のエラーを `%w` で包んでいるため
既に成り立っているが、権限エラーまで「不存在」に倒れると全文書き戻しへ
誤って落ちるので、成り立つ側と成り立たない側の両方をテストで固定した。

**往復恒等のプロパティ**: `restore(switch(x)) == x` と
`switch(restore(y)) == y` をバイト比較で固定した(`TestSwitchRestoreRoundTripIsIdentity`)。
入力は install.sh がマージした実物・インデント 4 の変種・1 行の最小構成・
絶対パス前置き・対象 0 件の 5 通り。

唯一の例外は、対象の文字列リテラルが `\/` のような非正規なエスケープで
書かれていた場合である。1 回目の変換で素直な表記へ正規化されるため
バイト単位では戻らない。正規化自体は既存テストで固定されているので、
プロパティのコメントに例外として明記した。

**結果の型**: `RestoreHooksResult` を `Changes` / `SettingsMissing` /
`BackupPath` / `RestoredFromBackup` の形へ更新し、cli の出力と `--dry-run`
表示を switch と対称(before / after の一覧)にした。

### 7-2. バックアップ名を対象ファイル名から導出する

**指摘**: どの対象でも `settings.json.mdev-backup-<ts>` の固定名のため、
`MDEV_SETTINGS_FILE` で同一ディレクトリ内のコピーを対象にすると、
実ファイルの復元が予行演習のバックアップを拾う。

**検証**: 同じディレクトリに `settings.json` と `settings.copy.json` を置いて
それぞれ `Backup` を呼ぶテストで再現した(両方が同じ名前になり、
実ファイル側の `LatestBackup` がコピーの内容を返した)。

**対応**: 前置きを `filepath.Base(s.path) + ".mdev-backup-"` に変更し、
`LatestBackup` も同じ導出で絞り込むようにした。

### 7-3. `LatestBackup` が tmp 残骸を拾い得る

**指摘**: `writeFileAtomicMode` の一時ファイル(`<名前>.tmp-<乱数>`)が
クラッシュで残ると、`settings.json.mdev-backup-<ts>.tmp-<乱数>` は前置きが
一致し辞書順でも大きいため「最新のバックアップ」に選ばれ得る。

**検証**: 残骸を置いたテストで再現した。書きかけの内容で `settings.json` を
復元することになり、復旧手段そのものが壊れる。

**対応**: 前置きを除いた残りが `20060102T150405Z` として `time.Parse` できる
名前だけを候補にした。手で付けた名前も同時に外れる。

### 7-4. Write 失敗時にバックアップパスが報告されない

**指摘**: `app.Switch` は Write 失敗時も `result.BackupPath` を持って返すが、
cli が `err` だけを見て結果を捨てている。

**対応**: 失敗時に「settings.json は変更されていません。バックアップ: <path>」を
stderr へ出す。書き込みは原子的な置き換えなので、失敗した時点で
`settings.json` が元のままであることは断定できる。退避の前に失敗した場合は
`BackupPath` が空なので何も出さない。

### 7-5. 切り替え先バイナリの未設置チェック

**指摘**: 切り替え後の hooks が指す
`${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev` が無くても切り替えは
成功するため、「切り替えたのにダッシュボードが無反応」という形でしか
気付けない。

**対応**: switch 実行時(dry-run 含む)に存在を確認し、無ければ警告を出す。
エラーにはしない(hook は非ブロッキングなので会話は壊れない)。

ADR-0002 に沿った置き場所は次のとおり。

| 層 | 担当 |
|---|---|
| infra | `MdevBinaryStore` が `CONDUCTOR_HOME` 配下の存在確認を行う |
| app | `MdevBinaryLocator` port を定義し、`MissingBinaryPath` として判断を返す |
| cli | 無反応になる旨と `make install` の案内を表示する |

`CONDUCTOR_HOME` は mdev 実行時の環境変数から解決する(`cmd/mdev` が組み立てる)。
hooks のコマンド文字列は環境変数展開の形のまま残すので、hook 実行時に
どのファイルが呼ばれるかは実行環境次第だが、mdev-test で `CONDUCTOR_HOME` を
worktree へ向けている場合に一番起きやすい取り違えはこれで拾える。

ディレクトリは hook から実行できないため「存在しない」扱いにした。

### 7-6. hook サブコマンド名の二重定義

**指摘**: domain の置換規則内の `hook notify` などと cli のサブコマンド名が
独立に定義されている。

**検証**: 片方だけを直しても両方ともコンパイルが通り、両パッケージのテストも
緑のままになる。実環境では hook 実行時の `unknown command` としてしか現れない。

**対応**: domain に `SwitchedHookCommandSuffixes()` を足し、全パッケージを
参照できる `cmd/mdev`(ADR-0002 で唯一許される)のテストで cobra の
コマンドツリーと双方向に突き合わせる。

- 規則にあるサブコマンドがコマンドツリーに存在すること
- コマンドツリーの `mdev hook` の子すべてに対応する規則があること

規則名を実際に `post-tool` → `posttool` と食い違わせて、両方向の検出が
働くことを確認してから戻した。

### 7-7. 未置換の pending スクリプトの警告

**指摘**: 置換規則は末尾一致なので、引数付きの呼び出しや利用者が足した
別のスクリプトは切り替わらずに残る。

**対応**: 切り替え後の内容に `/scripts/pending-` を含むコマンドが残っていれば、
イベント名とコマンドを一覧で警告する(dry-run でも出す)。走査は
`scanHookCommands` を共用する。

### 7-8. 見送った指摘

**`app.HookCommandChange` の DTO コピー**(型エイリアスで簡素化可能)

型エイリアス `type HookCommandChange = domain.HookCommandChange` にすれば
移し替えのコードは消えるが、go-arch-lint は import しか見ないため、
エイリアスは境界検査を形式的に通すだけになる。ADR-0002 の意図は
「cli / tui が domain の型に依存しない」ことなので、レイヤー境界を明示する
DTO を維持する(実装時の判断をそのまま支持する)。今回 `HookCommand` を
足したときも同じ方針を踏襲した。

**コメント規約(曖昧な「など」)**

`grep -rn "など" --include='*.go' .` で 18 箇所。このタスクで書いた 4 箇所
(`app/hookswitch.go` 2・`cli/hooks.go` 2・`domain/hooksettings.go` 1 のうち
重複を除く)を具体的な言い回しへ直し、1 コミットにまとめた。
残る 12 箇所は transcript / pricing / config の既存コメントで、
このタスク以前から存在するため変更範囲外とした。

### 7-9. 対応後の `make check`

```
gofmt: no diff
golangci-lint: 0 issues
go-arch-lint: OK - No warnings found
go test -race: 全パッケージ ok
Total test coverage: 94.4% (938/994)
  internal/domain 97.6% / internal/app 99.2% / internal/cli 96.2% /
  internal/infra/store 89.4%
go build: OK
```

`internal/app` が 100.0% から 99.2% へ下がったのは、`Switch` が
`RemainingPendingScriptCommands` のエラーを返す分岐が到達不能だからである
(切り替え後のバイト列は必ず妥当な JSON になる)。エラーを握り潰すよりは
返す方が正しいので、防御的な分岐として残した。閾値(90%)は満たしている。

`cmd/mdev` にテストファイルを足したため、これまでカバレッジ計測の対象外だった
`main.go` が母数に入った。全体の閾値(70%)は満たしている。

### 7-10. 実環境を変更していないことの再確認

```sh
$ stat -f '%Sm %N' -t '%F %T' ~/.claude/settings.json
2026-08-08 22:37:52 /Users/kazuto/.claude/settings.json

$ ls ~/.claude/settings.json.mdev-backup-*
no matches found

$ ls ~/.claude-conductor/bin
ls: /Users/kazuto/.claude-conductor/bin: No such file or directory
```
