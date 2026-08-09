# ユーザーテスト 02: ダッシュボード系 4 ペインを Go 版へ切り替える

Go 版 `mdev` が実環境で動く 2 つ目のユーザーテストである。
Zellij レイアウトの Main タブに並ぶ 4 ペイン(Dashboard / Waiting / Done / News)の
起動コマンドを、Shell スクリプトから `mdev pane <name>` へ差し替えて確認する。

ユーザーテスト 01(hooks)と違い、こちらは**レイアウトファイルを書き換える**。
`~/.claude-conductor/layouts/multi.kdl` の 4 行だけが対象で、
バックアップからコピーし直せば必ず元に戻せる。

## 何が変わるか

| ペイン | 切り替え前 | 切り替え後 |
|---|---|---|
| Dashboard | `${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/dashboard-loop.sh` | `${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev pane dashboard` |
| Waiting | `.../scripts/waiting-loop.sh` | `.../bin/mdev pane waiting` |
| Done | `.../scripts/done-loop.sh` | `.../bin/mdev pane done` |
| News | `.../scripts/news-loop.sh` | `.../bin/mdev pane news` |

変わらないもの:

- TaskCreate ペイン(`task-create-loop.sh`)と task-control ペイン
  (タスクタブの下に出る操作バー)。どちらもフェーズ 3 で移植する
- `${CONDUCTOR_HOME:-$HOME/.claude-conductor}` という前置き
  (絶対パスへ展開すると `mdev-test` の worktree 隔離が効かなくなる)
- Shell のまま呼ばれ続けるもの: `upload-log.sh`(削除時のログ送信)、
  `restore-task.sh`(Done からの復帰)、`fetch-news.sh`(ニュース取得)、
  `restore-session.sh`(起動時のタブ再生成)、`screen-detect-lib.sh`
  (スクリーン検出)。Go 版ペインはこれらを今までと同じ引数で呼ぶ

表示は ANSI レベルで現行と一致することがゴールデンテストで固定されている
(`cmd/mdev/testdata/golden-panes`、23 ケース)。目で見て違いがあれば、
それは**バグか、テストが取りこぼしているケース**である。

## 0. 事前準備

```sh
cd <mdev-go のチェックアウト>
make check            # 緑であること
make install          # ~/.claude-conductor/bin/mdev へ配置
~/.claude-conductor/bin/mdev pane --help
```

以下では Go 版バイナリを `$MDEV` と書く。

```sh
MDEV=~/.claude-conductor/bin/mdev
```

### バックアップを取る

```sh
cp ~/.claude-conductor/layouts/multi.kdl ~/multi.kdl.before-usertest02
```

このファイルは `install.sh` が配置するものなので、最悪の場合は
claude-conductor の再インストールでも戻せる(手順 C)。

### 端末で先に見比べる(推奨)

レイアウトを触る前に、Go 版と Shell 版の出力を同じ環境で並べて確認できる。
`--once` は 1 回描画して終了するモードである。

```sh
# zellij セッションの中で実行する(ZELLIJ_SESSION_NAME が要る)
diff <($MDEV pane dashboard --once) \
     <(CONDUCTOR_DASHBOARD_ONCE=1 bash ~/.claude-conductor/scripts/dashboard-loop.sh)

diff <($MDEV pane waiting --once) \
     <(CONDUCTOR_WAITING_ONCE=1 bash ~/.claude-conductor/scripts/waiting-loop.sh)

diff <($MDEV pane done --once) \
     <(CONDUCTOR_DONE_ONCE=1 bash ~/.claude-conductor/scripts/done-loop.sh)

diff <($MDEV pane news --once) \
     <(CONDUCTOR_NEWS_ONCE=1 bash ~/.claude-conductor/scripts/news-loop.sh)
```

- [ ] 4 つとも差分なし

> Dashboard の `--once` は現行の ONCE 経路と同じく、起動時のタスク復元
> (`restore-session.sh`)とスクリーン検出を実際に走らせる。
> 登録済みのタスクがあるとタブが作られることがある点に注意する。

## 1. レイアウトを差し替える

```sh
$EDITOR ~/.claude-conductor/layouts/multi.kdl
```

4 か所の `args` 行を書き換える。Dashboard の例:

```kdl
pane {
    name "Dashboard"
    command "bash"
    args "-c" "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev pane dashboard"
}
```

同様に Waiting / Done / News も `pane waiting` / `pane done` / `pane news` にする。

`sed` で一括に書き換えてもよい。

```sh
KDL=~/.claude-conductor/layouts/multi.kdl
sed -i '' \
  -e 's|/scripts/dashboard-loop\.sh|/bin/mdev pane dashboard|' \
  -e 's|/scripts/waiting-loop\.sh|/bin/mdev pane waiting|' \
  -e 's|/scripts/done-loop\.sh|/bin/mdev pane done|' \
  -e 's|/scripts/news-loop\.sh|/bin/mdev pane news|' \
  "$KDL"
```

確認:

```sh
diff ~/multi.kdl.before-usertest02 ~/.claude-conductor/layouts/multi.kdl
grep -c 'bin/mdev pane' ~/.claude-conductor/layouts/multi.kdl   # 4 が返る
```

- [ ] 差分が 4 行分(`args` の行)だけである
- [ ] TaskCreate ペインの行が変わっていない

### 再起動する

レイアウトは**セッションの作成時にだけ**読まれる。既存のセッションを
開き直しても反映されない。いったん終了してから新しく開く。

```sh
zellij kill-session <セッション名>   # または zellij delete-session <セッション名>
mdev                                  # 開き直す
```

- [ ] 4 つのペインがすべて表示され、枠が空のまま固まっているものが無い

> ペインが即座に閉じる・空のままになる場合は、そのペインだけ
> `bash -c '... pane dashboard'` を端末で直接実行してエラーを見る。
> バイナリが未配置(`make install` 忘れ)が最も多い。

## 2. 確認項目

`n` でタスクを 1 つ作って進めながら見ていく。
**目視でしか確認できない項目**(赤 ■ / 自動帰還 / done 表示)がここに集まっている。

### 2-1. Dashboard の表示

| # | 操作 | 期待される見え方 |
|---|------|------------------|
| 1 | タスクに承認が要る操作(ファイル書き込みなど)を指示する | **赤い ■** とタスク名・時刻が出て、下の行にメッセージが出る |
| 2 | タスクタブで承認する | 表示が消え、**自動で Main タブへ戻る** |
| 3 | ターンが完了するまで待つ | **緑の ■** と末尾に `done` が出る |
| 4 | タスクを 2 つ以上動かす | Zellij のタブの並び順で上から並ぶ |
| 5 | タスクが 1 つも待っていない状態にする | `All tasks running` が出る |

- [ ] 1: 赤 ■ が出る
- [ ] 2: 承認後に表示が消え、Main タブへ自動で戻る
- [ ] 3: 緑 ■ と `done` が出る
- [ ] 4: タブ順に並ぶ
- [ ] 5: `All tasks running` が出る
- [ ] 見出しが `Current Tasks [<セッション名>]`、フッタが
      `Pending: <件数>  [num]: jump / d+[num]: delete` になっている

### 2-2. Dashboard のジャンプ

- [ ] 数字キー(1-9)でそのタスクのタブへ移動する
- [ ] claude のタスクへジャンプしても、一覧の表示は消えない
      (hooks がライフサイクルを持つため。承認や次のプロンプトで消える)

### 2-3. Dashboard の削除(最重要)

`d` を押してから 3 秒以内に番号を押す。

- [ ] `d` を押すと `Delete tab number...` が出る
- [ ] 3 秒待つと案内が消え、**何も削除されない**
- [ ] `d` + 番号でタブが閉じ、Dashboard の一覧からも消える
- [ ] ログのアップロードが有効なら、閉じる直前に URL が数秒表示される
- [ ] 削除後、`~/.claude-conductor/tasks/<セッション名>/` から
      そのタスクのエントリが消えている(復元で蘇らないこと)

```sh
ls ~/.claude-conductor/tasks/<セッション名>/
tail -1 ~/.claude-conductor/daily/<セッション名>/$(date +%F).jsonl | jq .
```

- [ ] daily log に 1 行追記されている

**アップロードが失敗したときの確認**(任意)。
`upload-log.sh` が非 0 で終わると、**何一つ削除してはいけない**。
設定でアップロードを有効にしたうえで、ネットワークを切るなどして失敗させる。

- [ ] `Upload failed. Deletion cancelled.` が出る
- [ ] タブが閉じていない
- [ ] Dashboard の一覧からタスクが消えていない
- [ ] レジストリのエントリも残っている

### 2-4. Waiting

タスクタブで `w` を押して Waiting へ退避する。

- [ ] Waiting ペインに黄色の ■ とタスク名が出る
- [ ] 同じタスクが Dashboard 側からは消える
- [ ] もう一度 `w` を押すと Waiting から消え、Dashboard へ戻る
- [ ] 0 件のときは `No waiting tasks` が出る

### 2-5. Done

- [ ] タスクを削除すると Done ペインに ⚡ 付きで出る
- [ ] 先頭に `<件数> tasks  <turns> turns / <calls> calls / $<金額>` の統計行が出る
- [ ] 時刻が完了時刻の `HH:MM` になっている
- [ ] PR をマージした / Slack へ投稿した / ドキュメントを書いたタスクには
      🚀 / 💬 / 📝 が付く
- [ ] 0 件のときは `No tasks completed yet` が出る

restore(`r` を押してから 3 秒以内に番号):

- [ ] `r` を押すと `Restore number...` が出る
- [ ] 3 秒待つと案内が消える
- [ ] `r` + 番号でタスクのタブが作り直され、Done の一覧から消える

### 2-6. News

- [ ] ニュースが番号付きで並び、説明行が付く
- [ ] フッタが `[1-<件数>]: open  ·  [r]: reload` になっている
- [ ] 数字キーでブラウザが開く
- [ ] `r` を押すと `⟳ Fetching news...` が出て、取得後に一覧が更新される
- [ ] ニュースがまだ無い日は `No news yet. Press [r] to reload.` が出る

### 2-7. Shell 版との併存

一部だけ Go 版にしても壊れないことを見る。

- [ ] TaskCreate ペイン(`n` でタスク作成)がこれまで通り動く
- [ ] タスクタブ下の操作バー(task-control)がこれまで通り動く
- [ ] `mdev` を一度終了して開き直すと、登録済みのタスクのタブが復元される
      (Dashboard 起動時の `restore-session.sh`)

### 2-8. codex タスク(スクリーン検出)

codex など hooks を持たないエージェントのタスクがある場合のみ。

- [ ] codex タスクの承認待ちが Dashboard に出る(赤 ■)
- [ ] 承認して作業が再開すると表示が消え、Main タブへ自動で戻る

> ここが動かない場合、毎ポーリングのスクリーン検出
> (`screen_detect_tick`)が呼べていない。`CONDUCTOR_HOME` の指す先に
> `scripts/screen-detect-lib.sh` があるかを確認する。

## 3. 元に戻す手順

### 手順 A: バックアップからコピーし直す

```sh
cp ~/multi.kdl.before-usertest02 ~/.claude-conductor/layouts/multi.kdl
```

### 手順 B: sed で逆向きに書き換える

バックアップを失った場合。

```sh
KDL=~/.claude-conductor/layouts/multi.kdl
sed -i '' \
  -e 's|/bin/mdev pane dashboard|/scripts/dashboard-loop.sh|' \
  -e 's|/bin/mdev pane waiting|/scripts/waiting-loop.sh|' \
  -e 's|/bin/mdev pane done|/scripts/done-loop.sh|' \
  -e 's|/bin/mdev pane news|/scripts/news-loop.sh|' \
  "$KDL"
```

### 手順 C: 再インストールする

```sh
cd ~/projects/claude-conductor && ./install.sh
```

### 戻ったかの確認

```sh
grep -c 'scripts/.*-loop\.sh' ~/.claude-conductor/layouts/multi.kdl   # 5 が返る
grep -c 'bin/mdev pane' ~/.claude-conductor/layouts/multi.kdl         # 0 が返る
```

> 5 は 4 ペイン + TaskCreate(`task-create-loop.sh`)の合計である。

**どの手順でもセッションを開き直す**(レイアウトは作成時にだけ読まれる)。

## 4. 既知の注意点

### `mdev-test` との関係

`mdev-test` は `CONDUCTOR_HOME` を claude-conductor の worktree へ向ける。
レイアウトもその worktree の `layouts/multi.kdl` が使われるため、
**そちらを書き換えないと Go 版ペインでは起動しない**。逆に言えば、
実環境のレイアウトを触らずに worktree 側だけで試すこともできる。

worktree にも Go 版バイナリを置いておく。

```sh
cd <mdev-go のチェックアウト>
make install CONDUCTOR_HOME=<claude-conductor の worktree パス>
```

### Shell 版から引き継いだ既知の癖

いずれも**現行と同じ挙動を意図して再現している**。Go 版で直したわけではないので、
気になっても「壊れた」わけではない。

- **スペースを含むタブ名は Dashboard に出ない**。タブ一覧の取得が
  3 列目だけを見るため、名前が途中で切れて pending と一致しなくなる
  (削除時の ID 解決はスペースに対応しており、非対称)
- **daily log が 1 行でも壊れていると Done が全滅する**。
  正常なエントリまで `No tasks completed yet` になる
- **日本語タブ名は Done の桁がずれる**。桁詰めがバイト幅のため
- **メッセージは 60 バイトで切られる**。マルチバイト文字の途中で切れることがある

### Go 版で変わったところ

- **Ctrl+C でペインが終了する**。Shell 版は終了キーを持たなかった。
  誤って閉じた場合はセッションを開き直す
- **削除中の進行表示が本体の下に 1 行足される**形になった。Shell 版は
  最終行を上書きしていた。表示される文言は同じである

詳しい経緯と実測は `.claude/todo/20260809-panes-evidence.md` にある。

## 5. 片付け

```sh
cp ~/multi.kdl.before-usertest02 ~/.claude-conductor/layouts/multi.kdl   # 戻す場合
rm -f ~/multi.kdl.before-usertest02                                     # そのまま使う場合
```
