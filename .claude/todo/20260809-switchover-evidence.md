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
