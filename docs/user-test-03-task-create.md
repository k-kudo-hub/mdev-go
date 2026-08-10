# ユーザーテスト 03: タスク作成ペインと task-control を Go 版へ切り替える

Go 版 `mdev` が実環境で動く 3 つ目のユーザーテストである。
Main タブ下部の **TaskCreate ペイン**(`n` でタスクを作る画面)の起動コマンドを
Shell スクリプトから `mdev pane task-create` へ差し替えて確認する。

ユーザーテスト 02(ダッシュボード系 4 ペイン)と同じく
`~/.claude-conductor/layouts/multi.kdl` を書き換えるが、**対象は 1 行だけ**である。
バックアップからコピーし直せば必ず元に戻せる。

## 何が変わるか

| ペイン | 切り替え前 | 切り替え後 |
|---|---|---|
| TaskCreate | `${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/task-create-loop.sh` | `${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev pane task-create` |

### task-control ペインは差し替え不要

タスクタブの下に出る操作バー(`m` / `w` / `dd`)は **レイアウトファイルに
書かれていない**。タスクを作った側がその場で起動するペインだからである。

- Go 版の `mdev pane task-create` が作ったタスク
  → `mdev pane task-control <タブ名>` が起動する(**Go 版**)
- Shell 版の `task-create-loop.sh` が作ったタスク、および
  `restore-task.sh` / `restore-session.sh` が復元したタスク
  → `bash .../scripts/task-control.sh <タブ名>` が起動する(**Shell 版**)

つまり切り替え後も、**復元されたタスクだけは Shell 版の操作バーで動く**。
2 つは同じ pending / レジストリを読み書きするデータ互換の実装なので混在してよい。
復元経路の Go 化はフェーズ 4 で行う。

見分け方: Go 版のバーは削除の確認中に `Press d to confirm delete...` を
**バーの代わりに**出す(Shell 版はバーの右端に上書きする)。表示している
文字列は同じである。

### 変わらないもの

- ダッシュボード系 4 ペイン(ユーザーテスト 02 で切り替え済み)
- `${CONDUCTOR_HOME:-$HOME/.claude-conductor}` という前置き
  (絶対パスへ展開すると `mdev-test` の worktree 隔離が効かなくなる)
- Shell のまま呼ばれ続けるもの: `upload-log.sh`(削除時のログ送信)、
  `restore-task.sh` / `restore-session.sh`(復元)、`screen-detect-lib.sh`
- 作られるタブの中身。環境変数(`TASK_TAB_NAME` / `TASK_TYPE` / `TASK_AGENT`)、
  エージェントの起動コマンド、レイアウトの当て方はすべて Shell 版と同じ

### 見た目が変わるところ(意図的)

`fd` と `fzf` を使わなくなるため、選択画面が自前のものに変わる。

| 項目 | Shell 版(fzf) | Go 版 |
|---|---|---|
| 画面 | 全画面(alt-screen)に切り替わる | ペインの中にそのまま出る |
| 絞り込み | fzf の既定(部分列 + スコア順に並べ替え) | 部分列のみ。**並べ替えはしない** |
| キー | fzf の全機能 | `↑` `↓` `Ctrl-P` `Ctrl-N` `Enter` `Esc` `Backspace` |
| `FZF_DEFAULT_OPTS` | 効く | **効かない** |
| タスク名の入力 | bash 3.2 では候補の提示のみ(編集不可) | 常に編集できる(`Ctrl-U` で全消し) |
| 作成の失敗 | 無言 | 赤字で 2 秒表示してメニューへ戻る |
| 終了 | できない | `Ctrl-C` |

`fd` / `fzf` は Shell 版の経路が使い続けるのでアンインストールしないこと。

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
cp ~/.claude-conductor/layouts/multi.kdl ~/multi.kdl.before-usertest03
```

このファイルは `install.sh` が配置するものなので、最悪の場合は
claude-conductor の再インストールでも戻せる(手順 C)。

### 端末で先に見比べる(推奨)

レイアウトを触る前に、操作バーの出力を同じ環境で並べて確認できる。
`--once` は 1 回描画して終了するモードである。

```sh
# zellij セッションの中で、実在するタスクタブ名を指定して実行する
TAB=<タスクタブ名>
diff <($MDEV pane task-control "$TAB" --once) \
     <(CONDUCTOR_TASKCTL_ONCE=1 bash ~/.claude-conductor/scripts/task-control.sh "$TAB")
```

- [ ] 差分なし(そのタスクが Waiting のときも差分なし)

タスク作成ペインには `--once` が無い(キー入力を待つ画面で、
1 回だけ描いても意味がないため)。

## 1. レイアウトを切り替える

`~/.claude-conductor/layouts/multi.kdl` の `TaskCreate` ペインの `args` 行を
書き換える。

```kdl
pane size="30%" {
    name "TaskCreate"
    focus true
    command "bash"
    args "-c" "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev pane task-create"
}
```

変更前は次の行だった。

```kdl
    args "-c" "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/task-create-loop.sh"
```

`sed` で行う場合:

```sh
sed -i '' \
  's#scripts/task-create-loop.sh#bin/mdev pane task-create#' \
  ~/.claude-conductor/layouts/multi.kdl
diff ~/multi.kdl.before-usertest03 ~/.claude-conductor/layouts/multi.kdl
```

- [ ] 差分が 1 行だけであること

## 2. セッションを作り直す

レイアウトは zellij セッションの作成時にしか読まれない。
**既存セッションを終了してから**開き直す。

```sh
zellij kill-session <セッション名>   # または zellij delete-session
mdev <ディレクトリ>                  # いつもの起動
```

- [ ] Main タブ下部に `New Task [<セッション名>]` と `[n] Create task` が出る
- [ ] 見出しと区切り線が今までと同じ位置・同じ色である

## 3. タスク作成フローを一通り試す

### 3-1. 正常系(dev)

- [ ] `n` を押すと `検索中...` が一瞬出て、ディレクトリ候補が並ぶ
- [ ] 候補が `search_dirs` 配下のディレクトリだけである
      (ドットで始まるディレクトリが混ざっていないこと)
- [ ] 文字を打つと絞り込まれる。`Backspace` で戻る
- [ ] `↑` `↓` / `Ctrl-P` `Ctrl-N` でカーソルが動く
- [ ] `Enter` で確定し、`Task type:` に **config.json の記述順**で種別が並ぶ
      (キーの右に説明が出る)
- [ ] `dev` を選ぶ
- [ ] `.agents` が 2 件以上あれば `Agent:` が出る。1 件なら出ずに飛ばされる
- [ ] `Task name:` に `<ディレクトリ名>-dev` がプリフィルされている
- [ ] そのまま `Enter` → `作成中...` のあと新しいタブへ移る
- [ ] 新しいタブに **エージェント / nvim / lazygit / 操作バー** が揃っている
- [ ] Main タブへ戻ると TaskCreate ペインがメニュー表示に戻っている

### 3-2. 名前の編集

- [ ] `n` → ディレクトリ → 種別 → `Backspace` で末尾を消し、文字を足せる
- [ ] `Ctrl-U` で全部消えて、そこから打ち直せる
- [ ] 全部消して `Enter` を押すと**既定名**で作られる(空にはならない)

### 3-3. 名前の重複

同じディレクトリ・同じ種別でもう 1 つ作る。

- [ ] タブ名が `<名前>-2` になる(既に `-2` があれば `-3`)

### 3-4. k8s(レイアウトの多いもの)

- [ ] `n` → 種別で `k8s` を選ぶ
- [ ] エージェント / k9s / nvim / シェル / 操作バーが揃っている

### 3-5. レイアウトの無いもの(review / docs / survey)

- [ ] エージェントと操作バーだけのタブになる

### 3-6. キャンセル

各段階で `Esc` を押す。

- [ ] ディレクトリ選択で `Esc` → メニューへ戻る
- [ ] 種別選択で `Esc` → メニューへ戻る
- [ ] エージェント選択で `Esc` → メニューへ戻る
- [ ] 名前入力で `Esc` → メニューへ戻る
- [ ] いずれもタブが作られていない(`zellij action list-tabs` で確認)

### 3-7. codex エージェント

`.agents` に codex がある場合。

- [ ] `Agent:` で `codex` を選んで作る
- [ ] タブで codex が起動する
- [ ] しばらく待つとダッシュボードにそのタスクが出る
      (スクリーン検出が効いている = `TASK_AGENT=codex` が渡っている)

## 4. task-control ペイン(m / w / dd)

**Go 版で作ったタスク**のタブで確認する。

### 4-1. m キー

- [ ] タスクタブの操作バーで `m` → Main タブへ移る

### 4-2. w キー(Waiting)

エージェントが Notification か Stop を出した(= ダッシュボードか Done に
出ている)タスクで行う。

- [ ] `w` を押すとバーが `● WAITING  |  m: Main  |  w: Resume  |  dd: Delete tab` に変わる
- [ ] Main タブの Dashboard からそのタスクが消え、Waiting ペインに出る
- [ ] もう一度 `w` を押すと元に戻る
- [ ] **完了(Done に出ていた)タスクで往復すると Done に戻る**
      (Dashboard に出てきたら不具合)
- [ ] pending がまだ無いタスク(起動直後)で `w` を押しても何も起きない

### 4-3. dd キー(削除)

- [ ] `d` を 1 回押すと `Press d to confirm delete...` が出る
- [ ] そのまま **2 秒**待つと元のバーに戻る(何も消えない)
- [ ] `d` `d` と続けて押すと `Uploading log...` のあとタブが閉じる
- [ ] `upload.enabled = true` の場合、閉じる前にログの URL が 2 秒出る
- [ ] 閉じたあと Done ペインにそのタスクが出る
- [ ] `zellij` を再起動しても、そのタスクが復元されない
      (レジストリからも消えている)

アップロードを失敗させられる場合(リポジトリの設定を壊すなど):

- [ ] `Upload failed. Deletion cancelled.` が出てタブが**閉じない**
- [ ] pending も Done の記録もそのまま残っている

## 5. Shell 版との混在を確認する

- [ ] Done ペインで `r` + 番号 → 復元されたタブの操作バーが動く
      (これは Shell 版の task-control。`m` / `w` / `dd` が同じように使える)
- [ ] `zellij` セッションを作り直すと、登録済みタスクが復元される
      (Shell の `restore-session.sh` 経由。操作バーも Shell 版)

## 6. 異常系

### 6-1. search_dirs が全滅

`~/.claude-conductor/config.json` の `search_dirs` を存在しないパスだけにする。

- [ ] `n` を押すと赤字で `検索対象ディレクトリが見つかりません` が 2 秒出て
      メニューへ戻る

確認後は必ず元に戻すこと。

### 6-2. zellij の外で起動

```sh
$MDEV pane task-create
```

- [ ] メニューは出る。`n` を押すと候補は出るが、作成すると赤字でエラーが出る
      (`zellij` が無いのでタブを作れない)
- [ ] `Ctrl-C` で終了できる

## 7. 元に戻す

### 手順 A: バックアップから戻す

```sh
cp ~/multi.kdl.before-usertest03 ~/.claude-conductor/layouts/multi.kdl
diff ~/multi.kdl.before-usertest03 ~/.claude-conductor/layouts/multi.kdl
```

セッションを作り直すと Shell 版の TaskCreate ペインに戻る。

### 手順 B: sed で戻す

```sh
sed -i '' \
  's#bin/mdev pane task-create#scripts/task-create-loop.sh#' \
  ~/.claude-conductor/layouts/multi.kdl
```

### 手順 C: claude-conductor を入れ直す

```sh
cd <claude-conductor のチェックアウト>
./install.sh
```

`install.sh` は `config.json` を上書きしないので、設定は残る。

### 既に Go 版で作ったタスクはどうなるか

戻したあとも、Go 版で作ったタブの操作バーは `mdev pane task-control` のまま
動き続ける(そのペインのプロセスは既に起動しているため)。`mdev` バイナリを
消さない限り問題は起きない。そのタブを閉じれば完全に元の状態へ戻る。

## 報告してほしいこと

- 上のチェックで落ちた項目(何を押して何が起きたか)
- 選択画面の使い勝手で fzf より不便になったところ
- 作られたタブが Shell 版と違うところ(ペインの数・位置・起動コマンド)
