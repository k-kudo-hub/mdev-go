# ユーザーテスト 01: hooks を Go 版へ切り替える

Go 版 `mdev` が実環境で動く最初のユーザーテストである。
`~/.claude/settings.json` の hooks を Shell スクリプト呼び出しから
`mdev hook` サブコマンドへ差し替え、現行の Shell 製ダッシュボードが
そのまま動くことを確認する。

## 何が変わるか

置き換わるのは `.hooks` の中の **4 つのコマンド文字列だけ**である。

| イベント | 切り替え前 | 切り替え後 |
|---|---|---|
| `Notification` | `${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh` | `${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify` |
| `Stop` | 同上 | 同上 |
| `PostToolUse` | `.../scripts/pending-post-tool.sh` | `.../bin/mdev hook post-tool` |
| `UserPromptSubmit` | `.../scripts/pending-resolve.sh` | `.../bin/mdev hook resolve` |

変わらないもの:

- `Notification` / `Stop` にある terminal-notifier のインラインコマンド
- `permissions` など `.hooks` 以外のすべてのキー
- キーの並び順・インデント・空白(JSON を組み立て直さず、該当する文字列
  リテラルのバイト範囲だけを差し替えるため)
- ダッシュボード・Waiting・Done・タスク作成・`record-output.sh` などの
  Shell スクリプト(今回は一切触らない)

`${CONDUCTOR_HOME:-$HOME/.claude-conductor}` という前置きは維持する。
これを絶対パスへ展開すると、`mdev-test` の worktree 隔離が hooks に効かなくなる。

## 0. 事前準備

```sh
cd <mdev-go のチェックアウト>
make check            # 緑であること
make install          # ~/.claude-conductor/bin/mdev へ配置
~/.claude-conductor/bin/mdev --help
```

`mdev` は claude-conductor の shell 関数なので、Go 版は必ず
**フルパス `~/.claude-conductor/bin/mdev`** で呼ぶ。以下ではこれを `$MDEV` と書く。

```sh
MDEV=~/.claude-conductor/bin/mdev
```

### 自分で取る保険(推奨)

`mdev hooks switch` は自動でバックアップを作るが、それとは別に
手元にも 1 つ控えを置いておく。

```sh
cp ~/.claude/settings.json ~/settings.json.before-usertest01
```

### コピーで予行演習する

実ファイルに触る前に、コピーに対して切り替えと復元を試す。
`MDEV_SETTINGS_FILE` を指定すると対象ファイルを差し替えられる。

```sh
TMP=$(mktemp -d)
cp ~/.claude/settings.json "$TMP/settings.json"

MDEV_SETTINGS_FILE="$TMP/settings.json" $MDEV hooks switch --dry-run
MDEV_SETTINGS_FILE="$TMP/settings.json" $MDEV hooks switch
diff <(jq -S . ~/.claude/settings.json) <(jq -S . "$TMP/settings.json")   # 4 箇所だけ違う
MDEV_SETTINGS_FILE="$TMP/settings.json" $MDEV hooks restore
diff ~/.claude/settings.json "$TMP/settings.json"                         # 差分なし
```

最後の `diff` が無出力なら、往復で 1 バイトも変わらないことが確認できている。

## 1. 切り替える

```sh
$MDEV hooks switch --dry-run   # 変更内容の確認だけ。書き込まない
$MDEV hooks switch
```

期待される出力:

```
settings.json: /Users/<you>/.claude/settings.json

置き換える hook コマンド(4 件):
  [Notification]
    - ${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh
    + ${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify
  [Stop]
    ...
  [PostToolUse]
    ...
  [UserPromptSubmit]
    ...

バックアップ: /Users/<you>/.claude/settings.json.mdev-backup-<UTCタイムスタンプ>
hooks を mdev へ切り替えました。
```

確認:

- [ ] 4 件すべてが表示される
- [ ] バックアップのパスが表示され、そのファイルが実在する
- [ ] `$MDEV hooks switch` をもう一度実行すると
      「hooks は既に mdev を指しています。変更はありません。」と出る(冪等)
- [ ] `diff ~/settings.json.before-usertest01 ~/.claude/settings.json` の差分が
      4 行だけで、いずれも `command` の行である

**切り替え後は Claude Code を起動し直す。** 起動済みのセッションは
古い hooks 設定を保持している可能性がある。

## 2. 確認項目

`mdev` でダッシュボードセッションを開き、`n` でタスクを 1 つ作って進める。

### 2-1. 通常のタスクフロー

| # | 操作 | 期待される見え方 | hook |
|---|------|------------------|------|
| 1 | タスクに、承認が要る操作(ファイル書き込みなど)を指示する | Dashboard に赤い ■ とタスク名が出る | `Notification` |
| 2 | タスクタブへ移り、承認する | 表示が消え、自動で Main タブへ戻る | `PostToolUse` |
| 3 | ターンが完了するまで待つ | Dashboard に緑の ■ と `done` が出る | `Stop` |
| 4 | タスクタブへ移り、次のプロンプトを送る | pending の表示が消え、Main タブへ戻る | `UserPromptSubmit` |

- [ ] 1: 赤 ■ が出る
- [ ] 2: 承認後に表示が消え、Main タブへ戻る
- [ ] 3: 緑 ■ `done` が出る
- [ ] 4: プロンプト送信で表示が消え、Main タブへ戻る

うまくいかない場合は、pending ファイルが書かれているかを直接見る。

```sh
ls -la ~/.claude-pending/<セッション名>/
cat ~/.claude-pending/<セッション名>/*.json | jq .
```

### 2-2. Shell のまま残っている機能との共存

`waiting-toggle.sh` と `record-output.sh` は Shell のままである。
Go 版 hook と混ぜても壊れないことを見る。

- [ ] タスクタブで `w` を押すと Waiting ペインへ移り、もう一度押すと戻る
- [ ] タスクタブで `dd` を押して削除すると、daily log に記録される

```sh
tail -1 ~/.claude-conductor/daily/<セッション名>/$(date +%F).jsonl | jq .
```

### 2-3. `mdev record` の手動実行

Go 版の record が daily log へ追記できることを、hooks とは別に確かめる。
**pending が残っているタスクタブ**(赤 ■ か緑 ■ が出ている状態)の名前を使う。

```sh
# zellij セッションの中で実行する(ZELLIJ_SESSION_NAME が要る)
$MDEV record <タブ名>
tail -1 ~/.claude-conductor/daily/<セッション名>/$(date +%F).jsonl | jq .
```

- [ ] 1 行追記され、`tab` / `session` / `completed_at` / `summary` が入っている
- [ ] `pending` は削除されていない(`ls ~/.claude-pending/<セッション名>/`)

> `mdev record` は pending を消さない。タブを閉じる処理(`dd`)は
> Shell 側が担当したままなので、手動実行は「追記できるか」だけを見る。

### 2-4. 復元

```sh
$MDEV hooks restore
```

- [ ] 「settings.json をバックアップの内容へ復元しました。」と出る
- [ ] `diff ~/settings.json.before-usertest01 ~/.claude/settings.json` が無出力
- [ ] もう一度 `$MDEV hooks restore` を実行すると
      「settings.json はバックアップと同じ内容です。変更はありません。」と出る
- [ ] Claude Code を再起動し、2-1 の 4 項目が Shell 版のままでも動く

## 3. 問題が起きたときの復元手順

上から順に試す。どの段階でも **Claude Code の再起動が要る**。

### 手順 A: `mdev hooks restore`

```sh
~/.claude-conductor/bin/mdev hooks restore
```

`mdev hooks switch` が作った最新のバックアップで `settings.json` を置き換える。
これで元に戻る。

### 手順 B: バックアップから手で戻す

`mdev` 自体が動かない場合(ビルドが壊れている、バイナリを消したなど)。

```sh
ls -t ~/.claude/settings.json.mdev-backup-*        # 新しい順に並ぶ
cp ~/.claude/settings.json.mdev-backup-<最新> ~/.claude/settings.json
```

事前準備で取った控えがあればそちらでもよい。

```sh
cp ~/settings.json.before-usertest01 ~/.claude/settings.json
```

### 手順 C: hooks を書き直す

バックアップも控えも失った場合。claude-conductor の再インストールで
hooks が入り直る(`~/.claude/settings.json` の `.hooks` にマージされる)。

```sh
cd ~/projects/claude-conductor && ./install.sh
```

### 復元できたかの確認

```sh
jq '.hooks' ~/.claude/settings.json | grep -c 'scripts/pending-'   # 4 が返る
jq '.hooks' ~/.claude/settings.json | grep -c 'bin/mdev'           # 0 が返る
jq . ~/.claude/settings.json >/dev/null && echo "JSON OK"
```

## 4. 既知の注意点

### `mdev-test` との関係

hooks はマシン全体で共有される `~/.claude/settings.json` にあるため、
`mdev-test` の worktree 隔離は hooks の**構造**には効かない
(claude-conductor の README にも同じ注意書きがある)。

切り替え後は、hooks は `${CONDUCTOR_HOME:-...}/bin/mdev` を呼ぶ。
`mdev-test` は `CONDUCTOR_HOME` を claude-conductor の worktree へ向けるため、
**その worktree に `bin/mdev` が無いと hook が失敗する**(会話は止まらないが
pending が書かれず、テストセッションのダッシュボードが反応しなくなる)。

切り替えた状態で `mdev-test` を使う場合は、対象の worktree にも
バイナリを置いておく。

```sh
cd <mdev-go のチェックアウト>
make install CONDUCTOR_HOME=<claude-conductor の worktree パス>
```

### hook が失敗したときの見え方

hook の失敗は会話を止めない(終了コード 1 = 非ブロッキング)。
Claude Code の transcript に `<hook name> hook error` と stderr の 1 行目が出る。
Dashboard が反応しないのに会話は普通に進む、という形で現れる。

### バックアップが増える

`mdev hooks switch` は**変更がある場合にだけ**バックアップを作る。
切り替え済みの状態で何度実行してもファイルは増えない。
テストが終わったら不要なバックアップは消してよい。

```sh
ls ~/.claude/settings.json.mdev-backup-*
```

## 5. 片付け

```sh
$MDEV hooks restore                      # Shell 版へ戻す(そのまま使い続けるなら不要)
rm -f ~/.claude/settings.json.mdev-backup-*
rm -f ~/settings.json.before-usertest01
```
