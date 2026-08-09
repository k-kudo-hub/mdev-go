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
3. **採用: `.hooks` のバイト範囲を特定し、その中の該当文字列トークンだけをバイト単位で差し替える**
   - `encoding/json` の `Decoder.Token()` と `Decoder.InputOffset()` でトップレベルの
     `hooks` の値のバイト範囲を求め、その範囲内のトークンだけを走査して
     「オブジェクトの値として現れた文字列」で対象に一致するものの範囲を集める。
   - 集めた範囲を後ろから前へ差し替えるだけなので、**再シリアライズを一切行わない**。

### 結論

採用案 3 では JSON を書き戻さないため、キー順・インデント・空白・改行・
未知キー・触っていない hook コマンドの表記(エスケープの仕方を含む)がすべて
**バイト単位でそのまま保存される**。したがって
「jq 相当の正規化された出力」という妥協は不要で、
差分は置換した 4 箇所の文字列リテラルのみになる。

置換後に入れる文字列は `json.Marshal` で JSON 文字列としてエンコードする。
対象文字列は ASCII の英数字と `$ { } : - / . 空白` のみで構成され、
`json.Marshal` の HTML エスケープ(`< > &`)の対象文字を含まないため、
エスケープによる見た目の変化は起きない。

### 副次的な確認

`.hooks` 内の文字列トークンのうち「キー」ではなく「値」として現れたものだけを対象にする。
イベント名(`Notification` 等)や `matcher` / `type` といったキーが偶然一致することを防ぐためで、
デコーダのフレームスタック(オブジェクト/配列と、次のトークンがキーかどうか)を追って判定する。

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
