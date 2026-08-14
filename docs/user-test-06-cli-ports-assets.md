# ユーザーテスト 06: 残存 Shell の Go 化と資産の埋め込み(統合 6-2)

Shell 全廃の前提が揃ったことの実地確認。ここで確かめるのは **受け皿が動くこと**だけで、実環境の切り替え(init.zsh の呼び出し先・`~/.codex/config.toml` の notify・実配置のレイアウト)は次の 6-3 の `mdev install` が行う。

そのため、この手順は既存の設置物を **一切書き換えない**。書き込みが起きるのは `news/` 配下と、自分で作った一時ディレクトリの中だけである。

## 前提

- このブランチの mdev をビルド済み(`make check` で `bin/mdev` ができる)
- 以下では `MDEV` をそのバイナリのパスとする

```sh
cd <mdev-go の worktree>
MDEV=$PWD/bin/mdev
```

## テスト項目

### 1. `mdev news fetch`(実フィード)

```sh
$MDEV news fetch
ls ~/.claude-conductor/news/          # 期待: 当日の <日付>.json がある
```

1. もう一度 `$MDEV news fetch` を実行する
2. **期待**: 当日ファイルの更新時刻が変わらない(既にあるので取りに行かない)
3. `$MDEV news fetch --force` を実行する
4. **期待**: 更新時刻が変わる(取り直す)
5. News ペイン(`mdev` セッションの右下)で表示が崩れていないこと。特に URL に空白や改行が入っていないこと

現行 `fetch-news.sh` との一致は golden で機械検証済み(`scripts/gen-golden-news.sh` が作る fixture と `cmd/mdev/golden_news_test.go`)。実フィードでもバイト単位で一致することを確認してある。

### 2. `mdev codex notify`(実データ入力)

**実環境の pending には書かない**ため、隔離した HOME で試す。

`HOME` を移すと会話ログの引き先(既定は `$HOME/.codex`)まで一緒に動いてしまうので、`CODEX_HOME` は実物を明示して渡す(読み取りのみ)。

```sh
W=$(mktemp -d)
THREAD=$(sqlite3 ~/.codex/state_*.sqlite \
    "SELECT id FROM threads ORDER BY rowid DESC LIMIT 1")

HOME=$W CODEX_HOME=$HOME/.codex CONDUCTOR_HOME=$W/conductor \
    ZELLIJ_SESSION_NAME=smoke TASK_TAB_NAME=smoke-tab \
    $MDEV codex notify "{\"type\":\"agent-turn-complete\",\"thread-id\":\"$THREAD\",\"cwd\":\"$PWD\",\"last-assistant-message\":\"smoke\"}"

cat $W/.claude-pending/smoke/$THREAD.json    # pending が書かれている
cat $W/conductor/tasks/smoke/$THREAD.json    # レジストリが書かれている
```

**期待**:

- pending の `event` が `Stop`、`agent` が `codex`、`tab` が `smoke-tab`
- `transcript_path` に実在する rollout の `.jsonl` が入っている(状態 DB から引けている)
- レジストリの `updated_at` が今の時刻

続けて、退避中のタスクを守ることを確かめる。

```sh
jq '.event = "Waiting"' $W/.claude-pending/smoke/$THREAD.json > $W/tmp.json
mv $W/tmp.json $W/.claude-pending/smoke/$THREAD.json

HOME=$W CODEX_HOME=$HOME/.codex CONDUCTOR_HOME=$W/conductor \
    ZELLIJ_SESSION_NAME=smoke TASK_TAB_NAME=smoke-tab \
    $MDEV codex notify "{\"type\":\"agent-turn-complete\",\"thread-id\":\"$THREAD\",\"cwd\":\"$PWD\"}"

jq -r .event $W/.claude-pending/smoke/$THREAD.json   # 期待: Waiting のまま
rm -rf $W
```

現行 `codex-notify.sh` との一致は golden で機械検証済み(`scripts/gen-golden-codex.sh` の 11 件と `internal/infra/store/golden_codex_test.go`)。

### 3. `mdev agent launch`

**実環境の設定を読むだけで、起動したエージェントはすぐ終了させる。**

```sh
jq -r '.agent.command // "claude"' ~/.claude-conductor/config.json
$MDEV agent launch    # 上で出たコマンドが起動する。確認したら終了する
```

**期待**: 設定に書いたエージェント CLI がそのまま立ち上がる(プロセスは置き換わるので `mdev` は残らない)。

### 4. 埋め込み資産のフォールバック

```sh
$MDEV assets                              # 期待: 4 件の名前が並ぶ
diff <($MDEV assets layouts/dev.kdl) ~/.claude-conductor/layouts/dev.kdl
# 期待: 差分なし(実ファイルが優先される)

W=$(mktemp -d)
CONDUCTOR_HOME=$W $MDEV assets layouts/dev.kdl | grep 'args "-c"'
# 期待: .../bin/mdev agent launch(埋め込みに落ちる)
CONDUCTOR_HOME=$W $MDEV assets layouts/multi.kdl | grep -c 'bin/mdev pane'
# 期待: 5
rm -rf $W
```

**実ファイルを退避して確かめたい場合**(任意。戻し忘れに注意):

```sh
mv ~/.claude-conductor/layouts/dev.kdl /tmp/dev.kdl.bak
$MDEV assets layouts/dev.kdl | grep 'args "-c"'   # 期待: bin/mdev agent launch
mv /tmp/dev.kdl.bak ~/.claude-conductor/layouts/dev.kdl
```

### 5. 設置物が無くても動くこと

```sh
W=$(mktemp -d)
CONDUCTOR_HOME=$W $MDEV pane done --once
# 期待: エラーにならない(埋め込みの config.default.json から単価表が読める)
rm -rf $W
```

## 問題が出た場合

- どの項目も既存の設置物を書き換えないため、失敗しても実環境は元のまま動く
- `news/` だけは書き込みが起きる。当日のニュースが崩れた場合は `~/.claude-conductor/news/<日付>.json` を消して `mdev news fetch` をやり直せばよい

## この段階での注意

- `~/.codex/config.toml` の `notify` は **まだ Shell 版**を指している。切り替えは 6-3
- `init.zsh` は **まだ `fetch-news.sh`** を呼んでいる。切り替えは 6-3
- 実配置の `layouts/dev.kdl` は **まだ `agent-launch.sh`** を指している。切り替えは 6-3
