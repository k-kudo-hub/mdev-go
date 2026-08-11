# ユーザーテスト 04: スクリーン検出とセッション/タスク復元を Go 版へ切り替える

Go 版 `mdev` が実環境で動く 4 つ目のユーザーテストである。
これまでと違い **レイアウトファイル(`multi.kdl`)は一切触らない**。
切り替わるのは既に Go 版で動いている 2 つのペインの内部だけで、
`make install` でバイナリを置き換えた時点で有効になる。

| 経路 | 切り替え前 | 切り替え後 |
|---|---|---|
| codex の状態検出(Dashboard の毎ポーリング) | `bash -c '. screen-detect-lib.sh; screen_detect_tick'` | Go の `ScreenDetector` |
| 起動時のタスク復元(Dashboard の起動時) | `bash restore-session.sh` | Go の `SessionRestorer` |
| Done からの復元(`r` + 番号) | `bash restore-task.sh` | Go の `TaskRestorer` |

**前提: ユーザーテスト 02(ダッシュボード系 4 ペイン)を適用済みであること。**
Dashboard と Done がまだ Shell 版のままなら、この 3 つは Shell のまま動くので
この手順の対象にならない。

## 何が変わるか

### 目に見えない変化(最も重要)

2 秒ごとのポーリングで起きていた **bash と jq のプロセス起動がゼロになる**。
これまでは 1 周ごとに

- `bash`(screen-detect-lib.sh の source)1 個
- `zellij action list-panes` 1 個 + `jq` 1 個
- codex ペインの枚数ぶんの `zellij action dump-screen`

が起きていた。Go 版は zellij の呼び出しだけを残し、JSON の解釈も分類も
プロセス内で行う。**画面に出るものは変わらない**。

### 挙動が変わるところ(意図的)

| 項目 | Shell 版 | Go 版 | 理由 |
|---|---|---|---|
| Done から復元したタスクの `resume` | `claude_session_id` が `screen-…` でも渡す | 渡さず新規セッションで起動 | **バグ修正**。`screen-…` はタブ名から作った合成 ID で、実在しない。渡すと codex が起動時に失敗する |
| 復元時の `query-tab-names` / Main 帰還 | 上限なしの zellij 呼び出し | 10 秒の上限つき | 劣化した zellij サーバで復元がハングしない |
| 検出中のファイル書き込みの失敗 | 黙って捨てる | Dashboard にエラーとして出る | 書けないまま古い一覧を出し続けるより気づけるほうがよい |
| 起動時に作り直せなかったタスク | 何も出ない | Dashboard に黄色の `Warning:` 行 | そのタスクは画面に出てこないままなので、消えると手掛かりが残らない |
| Done からの復元の失敗 | 何も出ない | Done に赤い `Error:` 行を 2 秒 | 無反応だと押し直しを誘い、同じ名前のタブが増える |
| daily ログの `restored: true` の付け方 | `jq -c` でファイル全体を出し直す | 対象行にキーを差し込むだけ | 触っていない行の表記(大きな整数の指数表記化・空白の詰め方)を変えない |
| daily のロックを取れないとき | そのまま書き戻す | 復元を失敗させる(Done に残す) | 書き戻しは並行する完了記録の追記を消しうる。復元は再試行できるが記録は戻らない |
| 復元と検出にかかる時間の上限 | Shell 呼び出しに 60 秒 / 15 秒 | 同じ 60 秒 / 15 秒を Go 側で持つ | 劣化した zellij サーバでダッシュボードが出てこなくなるのを防ぐ |

### 変わらないもの

- 検出の判定そのもの(neutral / blocked / working / idle の分類と、
  idle を 1 秒の保留を経て確定させる手順)
- pending ファイルの形と置き場所、`.screen-state` の形式
- 復元の判定(タブごとに `updated_at` が最新の 1 件、dir が消えたエントリは捨てる、
  resume は「セッション ID + transcript のパス + そのファイルが実在」の 3 条件)
- `restore-task` の失敗の扱い(Done にエントリが残る)
- Shell のまま呼ばれ続けるもの: `upload-log.sh`(削除時のログ送信)、
  `fetch-news.sh`(ニュース取得)

### 復元されたタスクの操作バー

**ここが変わる。** これまで復元されたタスクの操作バーは Shell 版
(`bash .../scripts/task-control.sh`)だったが、復元が Go 化されたことで
`mdev pane task-control` に変わる。ユーザーテスト 03 で書いた
「復元されたタスクだけは Shell 版の操作バーで動く」は解消される。

見分け方: Go 版のバーは削除の確認中に `Press d to confirm delete...` を
**バーの代わりに**出す(Shell 版はバーの右端に上書きする)。

## 0. 事前準備

```sh
cd <mdev-go のチェックアウト>
make check            # 緑であること
```

### 今のバイナリを退避する(戻すときに使う)

```sh
cp ~/.claude-conductor/bin/mdev ~/mdev.before-usertest04
```

### 設定に codex があることを確かめる

スクリーン検出は `.agents.<name>.detection == "screen"` のエージェントにしか
働かない。

```sh
jq '.agents.codex | {detection, patterns}' ~/.claude-conductor/config.json
```

- [ ] `detection` が `"screen"` である
- [ ] `patterns.blocked` に承認プロンプトの正規表現が並んでいる

`config.json` に `.agents` が無い場合は `config.default.json` が使われる。
その場合はこのファイルを見ること。

### 入れ替える

```sh
make install
~/.claude-conductor/bin/mdev pane --help
```

Dashboard と Done のペインは**既に動いているプロセス**なので、
入れ替えただけでは古いバイナリのままである。次の手順でセッションを作り直す。

## 1. セッションを作り直す

```sh
zellij kill-session <セッション名>
mdev <ディレクトリ>
```

- [ ] Main タブの 4 ペインが今までどおり出る
- [ ] Dashboard の見出しとタスク一覧が今までと同じ見た目である

## 2. codex タスクの検出

`n` で **codex** のタスクを 1 つ作る(ユーザーテスト 03 の 3-7 と同じ)。

### 2-1. 承認待ち(blocked)の検出

codex に承認が要る操作を頼む。例:

```
touch probe.txt を実行して
```

- [ ] タブに `Would you like to run the following command?` が出る
- [ ] **2 秒以内に** Main タブの Dashboard にそのタスクが出る
- [ ] 表示のイベントが `Notification` 相当(承認待ちの色)である
- [ ] メッセージが承認プロンプトの行そのもの
      (`Would you like to run the following command?`)である
- [ ] そのまま放置しても**メッセージと時刻が書き換わらない**
      (最初に検出した時刻が保たれる)

### 2-2. 承認したら消える(working)

タブへ移って `y` で承認する。

- [ ] Dashboard からそのタスクが消える
- [ ] **承認した直後に Main タブへフォーカスが戻る**
      (タブ内で答えた合図なので自動で引き戻す)

### 2-3. ターンの完了(done)

codex がターンを終えるまで待つ。

- [ ] 1〜2 秒後に Done ペインにそのタスクが出る
- [ ] Dashboard には出ない(完了は Done 側)
- [ ] **ターンの途中で一瞬 Done に出ることが無い**
      (スピナーが消える 1 フレームで誤検出しない)

### 2-4. 新しいプロンプトを送ると消える

Done に出ている状態で、そのタブへ移って新しい指示を送る。

- [ ] Done からそのタスクが消える
- [ ] 送信した直後に Main タブへフォーカスが戻る

### 2-5. 起動直後は done にならない

新しく codex のタスクをもう 1 つ作り、何も指示せずに放置する。

- [ ] 10 秒待っても Done に出てこない
      (エージェントが 1 度も動いていないので完了ではない)

### 2-6. Waiting の保護

codex タスクが Dashboard か Done に出ている状態で、そのタブの操作バーで `w`。

- [ ] Waiting ペインへ移る
- [ ] **そのまま 10 秒放置しても Waiting から動かない**
      (検出が勝手に戻したり消したりしない)
- [ ] もう一度 `w` で元の場所(Dashboard か Done)へ戻る

### 2-7. 全画面ビューアで誤検出しない(neutral)

`config.json` に `patterns.neutral` を設定している場合のみ。既定では空なので、
設定していなければこの項目は飛ばしてよい。

- [ ] codex の diff ビューアを開いても、Dashboard の表示が変わらない

### 2-8. claude のタスクは影響を受けない

claude のタスクも 1 つ動かしておく。

- [ ] claude のタスクは今までどおり hook 経由で Dashboard / Done に出る
- [ ] 承認待ちや完了の出方が変わっていない

## 3. mdev の再起動での復元

### 3-1. セッションを落として作り直す

タスクタブがいくつかある状態で行う。

```sh
zellij kill-session <セッション名>
mdev <ディレクトリ>
```

- [ ] 起動時に登録済みのタスクのタブが作り直される
- [ ] タブ名が元と同じである
- [ ] **最後に Main タブへフォーカスが戻る**(半端なタブに残らない)
- [ ] 作り直されたタブでエージェントが**前回の会話を引き継いでいる**
      (claude なら `--resume`、codex なら `resume <id>`)
- [ ] 作り直されたタブに操作バーが出て、`m` / `w` / `dd` が効く
      (**Go 版のバー**になっている)

会話が引き継がれているかは、タブの中で「さっきの続きから」と聞けば分かる。
引き継がれていない場合、原因は transcript が消えていることである。
記録されたパスを確認する。

```sh
# レジストリに記録された transcript のパスを確認する
jq -r '[.tab, .claude_session_id, .transcript_path] | @tsv' \
  ~/.claude-conductor/tasks/<セッション名>/*.json
```

### 3-1b. 作り直せなかったタスクの知らせ

`dir` が消えたタスクを含むレジストリで作り直したとき。

- [ ] Dashboard の一覧の下に黄色の `Warning:` 行が出る
- [ ] その行がポーリングで消えない(ペインを開き直すまで残る)
- [ ] 一覧そのものは今までどおり出ている(警告で潰れていない)

### 3-2. 作業ディレクトリが消えたタスク

記録された `dir` が無いタスク(worktree を閉じたタスク)がある状態で作り直す。

- [ ] そのタスクのタブは作られない
- [ ] **レジストリからそのエントリが消えている**(次回以降も試されない)

```sh
ls ~/.claude-conductor/tasks/<セッション名>/
```

### 3-3. 既にあるタブは作り直さない

タスクタブが開いている状態で、Dashboard ペインだけを再起動する
(タブを閉じずに `mdev pane dashboard` を別の場所で 1 回動かす、
もしくはセッションを落とさずに待つ)。

- [ ] 同じ名前のタブが二重に作られない

### 3-4. レジストリが空なら何も起きない

```sh
mv ~/.claude-conductor/tasks/<セッション名> /tmp/tasks-backup
zellij kill-session <セッション名>
mdev <ディレクトリ>
```

- [ ] タブが 1 つも作られず、Main だけで立ち上がる
- [ ] エラーも出ない

確認後は戻すこと。

```sh
mv /tmp/tasks-backup ~/.claude-conductor/tasks/<セッション名>
```

## 4. Done からの復元(`r` + 番号)

### 4-1. claude タスクの復元

完了した claude のタスクを Done ペインで戻す。

- [ ] `r` を押すと `Restore number...` が出る
- [ ] 番号を押すとタブが作り直される
- [ ] **Done の一覧からそのエントリが消える**
- [ ] タブで会話が引き継がれている
- [ ] 操作バーが出て `m` / `w` / `dd` が効く(Go 版のバー)

### 4-2. codex タスクの復元(バグ修正の確認)

**この項目がこのユーザーテストの主眼のひとつである。**

スクリーン検出だけで完了を検出した codex タスク(= hook ではなく画面から
done になったもの)を削除し、Done に入れてから戻す。

そのようなエントリは daily ログで見分けられる。

```sh
jq -r 'select(.agent=="codex") | [.tab, .claude_session_id] | @tsv' \
  ~/.claude-conductor/daily/<セッション名>/$(date +%F).jsonl
```

`claude_session_id` が `screen-` で始まっているものが該当する。

- [ ] `r` + 番号で戻すと **codex が普通に起動する**
      (`codex resume screen-…` で失敗しない)
- [ ] 会話は引き継がれない(新規セッションになる)。これが正しい挙動である

`claude_session_id` が `thread-…`(codex の本物のスレッド ID)のエントリなら:

- [ ] 会話が引き継がれる

### 4-3. 作業ディレクトリが消えたタスク

`dir` が消えているエントリを戻す。

- [ ] タブが作られない
- [ ] **Done の一覧に残ったままである**(次に直してから戻せる)
- [ ] 赤字で `Error: 記録された作業ディレクトリがありません` が 2 秒出る
      (現行版は無反応だった)

### 4-4. 二重に戻らない

同じエントリをもう一度戻そうとする。

- [ ] 一覧から消えているので選べない
- [ ] 同名のタブが 2 つ作られていない

## 5. 負荷の確認(任意)

Dashboard が回っている間のプロセスを見る。

```sh
# 切り替え前は bash / jq / zellij が毎秒現れていた
watch -n 1 'pgrep -fl "screen-detect-lib|jq" | head'
```

- [ ] `screen-detect-lib` を読む bash が 1 度も現れない
- [ ] Dashboard 由来の `jq` が現れない

`zellij action list-panes` と `dump-screen` は Go 版でも呼ばれるので現れてよい。

## 5b. 時間の上限(任意)

zellij サーバが劣化している状況を作れる場合のみ。

- [ ] 登録済みタスクが多くても、起動から 1 分ちょっとで Dashboard が出る
      (残りは黄色の `Warning:` で「次回の起動へ回しました」と出る)
- [ ] 検出が重くても Dashboard のポーリングが止まらない

## 6. 元に戻す

### 手順 A: 退避したバイナリへ戻す

```sh
cp ~/mdev.before-usertest04 ~/.claude-conductor/bin/mdev
zellij kill-session <セッション名>
mdev <ディレクトリ>
```

### 手順 B: main のバイナリを入れ直す

```sh
cd <mdev-go のチェックアウト>
git switch main
make install
```

### 手順 C: ペインごと Shell 版へ戻す

ユーザーテスト 02 の「元に戻す」を行うと、Dashboard と Done が Shell 版に
なり、検出も復元も Shell 版の経路へ完全に戻る。

### 戻したあとに残るもの

- 既に作り直されたタブの操作バーは `mdev pane task-control` のまま動き続ける
  (そのペインのプロセスは既に起動しているため)。タブを閉じれば消える
- `.screen-state` と pending ファイルの形式は Shell 版と同じなので、
  戻しても検出の状態は引き継がれる
- daily ログに付いた `restored: true` はそのまま(形式は同じ)

## 報告してほしいこと

- 上のチェックで落ちた項目(何を押して何が起きたか)
- Dashboard や Done に出た `Warning:` / `Error:` の文言(そのまま貼ってほしい)
- codex の検出でタイミングがずれたところ
  (承認が出るまでの遅れ、完了が出るまでの遅れ、誤検出)
- 復元で会話が引き継がれなかったタスクと、そのときの
  `claude_session_id` / `transcript_path`
- Dashboard に見慣れないエラーが出た場合はその文言
