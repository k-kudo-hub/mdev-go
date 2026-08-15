# ユーザーテスト 01: hooks を Go 版へ切り替える

> **歴史的記録**: この手順書は `mdev hooks switch` / `mdev hooks restore` を前提に書かれている。
> **両コマンドは v0.14 で廃止された**(FLAVOR による 2 系統の切り替えを廃止し、hooks の設定は
> `mdev install` が内包したため)。当時の検証内容の記録として残しており、本文は当時のままである。
>
> **現在の復元手順は [README の「settings.json を元へ戻す」](../README.md#settingsjson-を元へ戻す)を参照すること。**


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

戻すとき(`mdev hooks restore`)も同じ 4 箇所を逆向きに差し替えるだけである。
詳しくは「0. 事前準備」の「復元のしくみ」を読む。

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

### 復元のしくみ(先に読む)

`mdev hooks restore` は**バックアップを書き戻すコマンドではない**。
switch と逆向きの差し替えを、そのときの `settings.json` に対して行う。

| | 対象 | hooks 以外の変更の扱い |
|---|---|---|
| `hooks switch` | 4 つのコマンド文字列 | 触らない |
| `hooks restore` | 同じ 4 つ(逆向き) | 触らない |

これは、切り替えてから戻すまでの間に Claude Code 自身が
`settings.json` へ書いた変更(`permissions.allow` の追加が典型)を
消さないためである。バックアップの全文を書き戻すとそれが黙って失われる。

バックアップは 2 つの役目に絞られる。

1. `settings.json` ごと失われている場合のフォールバック
   (このときだけ `restore` が全文を書き戻す)
2. 何かがおかしくなったときに人が手で戻すための保険

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

バックアップのファイル名は**対象ファイル名から作られる**
(`<対象ファイル名>.mdev-backup-<UTCタイムスタンプ>`)。
上のように別ディレクトリのコピーを対象にすれば、実ファイルの
バックアップと混ざることはない。同じディレクトリに別名でコピーを
置いた場合も、名前が違えば互いのバックアップを拾わない。

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

### 出るかもしれない警告

いずれも切り替え自体は成功している。エラーではない。

**`警告: .../bin/mdev が見つかりません。`**

切り替え後の hooks が呼ぶバイナリが未設置である。`make install` を
実行していないか、`CONDUCTOR_HOME` が別の場所を指している。このまま
使うと会話は普通に進むのにダッシュボードだけが反応しなくなる。
`--dry-run` の段階でも出るので、そこで気付いたら先に配置する。

**`警告: 切り替えられなかった conductor スクリプトの呼び出しが残っています`**

既知の 3 種類と一致しない `pending-*.sh` の呼び出し(引数付き・別名)が
`.hooks` に残っている。一覧に出たイベントだけは Shell 版のまま動く。
意図したものかを手で確認する。

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
$MDEV hooks restore --dry-run   # 戻す内容の確認だけ。書き込まない
$MDEV hooks restore
```

switch と対称に、戻す 4 件が before / after 付きで表示される。

- [ ] 4 件すべてが表示され、「hooks を conductor のスクリプトへ戻しました。」と出る
- [ ] `diff ~/settings.json.before-usertest01 ~/.claude/settings.json` が無出力
      (テスト中に Claude Code が `settings.json` を書き換えていた場合は、
      その分だけ差分が残る。それが**残っているのが正しい**)
- [ ] もう一度 `$MDEV hooks restore` を実行すると
      「hooks は既に conductor のスクリプトを指しています。変更はありません。」と出る
- [ ] バックアップのファイルは消えていない(restore は使わない)
- [ ] Claude Code を再起動し、2-1 の 4 項目が Shell 版のままでも動く

## 3. 問題が起きたときの復元手順

上から順に試す。どの段階でも **Claude Code の再起動が要る**。

### 手順 A: `mdev hooks restore`

```sh
~/.claude-conductor/bin/mdev hooks restore
```

`.hooks` の 4 つのコマンド文字列を conductor のスクリプト呼び出しへ戻す。
hooks 以外のキーには触らないので、切り替え後に加わった設定は残る。

`settings.json` そのものを失っている場合は、このコマンドが自動で
最新のバックアップの全文を書き戻す(その旨が出力に出る)。

### 手順 B: バックアップから手で戻す

`mdev` 自体が動かない場合(ビルドが壊れている、バイナリを消した)。

```sh
ls -t ~/.claude/settings.json.mdev-backup-*        # 新しい順に並ぶ
cp ~/.claude/settings.json.mdev-backup-<最新> ~/.claude/settings.json
```

この方法は**切り替え直前の状態に丸ごと戻す**。切り替え後に加わった
hooks 以外の変更は失われるので、手順 A が使えるならそちらを先に試す。

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
`mdev hooks restore` はバックアップを作らず、消しもしない。
テストが終わったら不要なバックアップは消してよい。

```sh
ls ~/.claude/settings.json.mdev-backup-*
```

ファイル名は `<対象ファイル名>.mdev-backup-<UTCタイムスタンプ>` である。
`mdev` が「最新のバックアップ」として選ぶのは、この形の名前に
**完全に一致する**ものだけである。手で付けた名前
(`settings.json.mdev-backup-手動` の類)や、書き込み中に中断して
残った `.tmp-<乱数>` 付きのファイルは候補にならない。

## 5. 片付け

```sh
$MDEV hooks restore                      # Shell 版へ戻す(そのまま使い続けるなら不要)
rm -f ~/.claude/settings.json.mdev-backup-*
rm -f ~/settings.json.before-usertest01
```
