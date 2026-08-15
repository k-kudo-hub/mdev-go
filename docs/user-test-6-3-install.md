# ユーザーテスト 6-3: インストーラと init の Go 化

統合 6-3 の実地確認。**これが合格すると実環境が Go 完結になる**(`scripts/` が消え、`init.zsh` がシムになり、更新元が mdev-go へ切り替わる)。

このフェーズは**実環境を実際に書き換える**。これまでのユーザーテストと違い、読み取りだけでは終わらない。戻し方を先に読んでから始めること。

## 前提

- 現在: Shell 版の `~/.claude-conductor`(`scripts/` あり)+ Go 版バイナリ + `FLAVOR=go` の混在状態
- このブランチのリリース(v0.13.0 想定)が出ていること

## 事前準備: 戻せるようにする

```sh
cp -R ~/.claude-conductor ~/.claude-conductor.bak-6-3
cp ~/.claude/settings.json ~/.claude/settings.json.bak-6-3
cp ~/.codex/config.toml ~/.codex/config.toml.bak-6-3
```

`daily/` と `tasks/` は `mdev install` が触らない設計だが、実地確認では必ず控えを取る。

## テスト項目

### 1. `mdev install`(移行の本番)

```sh
~/.claude-conductor/bin/mdev install
```

**期待**（出力にこの順で出る）:

- `✓ 依存: zellij / エージェント: claude, codex`
- `✓ 資産を配置: init.zsh`(既存の init.zsh がシムへ置き換わる)
- `✓ VERSION を <版> にしました`(**バイナリ自身の版**)
- `✓ REPO_URL を https://github.com/k-kudo-hub/mdev-go にしました`
- `✓ hooks を mdev へ切り替えました`(既に Go 版なら出ない)
- `✓ codex の notify を mdev へ向けました`
- `✓ layouts/*.kdl の呼び出しを mdev へ書き換えました`
- `✓ Shell スクリプトを撤去します` + **消えるファイルの一覧**
- `✓ FLAVOR を削除しました`
- `✓ .zshrc は設定済みです`

続けて確認する。

```sh
ls ~/.claude-conductor/scripts 2>&1     # 期待: No such file or directory
cat ~/.claude-conductor/REPO_URL        # 期待: .../mdev-go
cat ~/.claude-conductor/VERSION         # 期待: mdev version と同じ
ls ~/.claude-conductor/FLAVOR 2>&1      # 期待: No such file or directory
grep -c '/scripts/' ~/.claude/settings.json ~/.claude-conductor/layouts/*.kdl   # 期待: すべて 0
grep 'codex-notify' ~/.codex/config.toml                                       # 期待: 何も出ない
```

**ユーザーデータが無傷であること**（最重要）:

```sh
jq '.search_dirs, .upload' ~/.claude-conductor/config.json   # 期待: 自分の設定のまま
ls ~/.claude-conductor/daily ~/.claude-conductor/tasks       # 期待: 変わっていない
jq '.permissions' ~/.claude/settings.json                    # 期待: 自分の設定のまま
```

### 2. 冪等性

```sh
mdev install    # 2 回目
```

**期待**: 依存チェックと `.zshrc` の行だけが出て、`✓ ...しました` が 1 つも出ない。

### 3. 新しいシムでのシェル起動

```sh
exec zsh    # または新しいタブを開く
```

```sh
whence -w mdev    # 期待: mdev: command(関数ではない)
whence -w dev     # 期待: dev: alias
whence -w zs      # 期待: zs: alias
whence -w zj      # 期待: zj: alias
```

**期待**: エラーが 1 つも出ない。`~/.zshrc` は書き換わっていない。

### 4. `mdev`(セッションの起動)

1. 適当なプロジェクトのディレクトリで `mdev`
2. **期待**: ダッシュボードが開き、5 ペインすべてが動く（Dashboard / Waiting / Done / News / タスク作成）
3. いったん detach（`Ctrl+q`）してもう一度 `mdev`
4. **期待**: **同じセッションへ戻る**（新しいセッションが増えない）
5. `zellij list-sessions` で 1 つだけであることを確認

```sh
mdev --new        # 期待: 時刻付きの別セッションができる
mdev <名前>       # 期待: その名前のセッションになる
```

**注意**: 子コマンドと同じ名前（`news` / `install` / `test` など）はセッション名として使えない。その語はコマンドとして解釈される。

打ち間違いは差し戻される。

```sh
mdev instal
# 期待: 'instal' はコマンドではありません('install' の打ち間違いでしょうか)。
#       セッション 'instal' を開くには: mdev attach instal
```

名前を指定して開くときは、開く直前に `セッション 'X' を開きます` が 1 行出る。

### 5. `dev` と `zs`

```sh
dev               # 期待: エージェント + エディタ + git の 3 ペインが開く
```

**期待**: Agent ペインで設定したエージェント CLI が起動する（`agent-launch.sh` は既に消えているので、ここが動けば `mdev agent launch` への切り替えが効いている）。

```sh
zs                # 期待: セッションの一覧が番号付きで出て、選ぶと入れる
```

### 6. タスク作成と操作バー

1. ダッシュボードで `n` → タスクを作る
2. **期待**: タブができ、下部に操作バーが出る（`os.Executable()` 経由で今のバイナリが起動する）
3. タスク内で 1 往復し、入力待ちで放置 → **期待**: Waiting に出る
4. `dd` で削除 → **期待**: アップロードののち Done に 1 件増える

### 7. `mdev test`（開発用・**実起動を含む**）

このフェーズでしか確かめられない項目である。`mdev test` の実起動は新しい端末の窓を開いて**実 zellij でセッションを作る**ため、隔離した自動検証では扱えず、スクリプト側では dry-run とビルドまでしか見ていない（`scripts/verify-install-isolated.sh` の (c)）。

まず副作用の無い範囲を確認する。

```sh
cd ~/projects/mdev-go
mdev test --dry-run <ブランチ名>
# 期待: WORKTREE / CONDUCTOR_HOME / BINARY / SESSION / CMD の 5 行が出て、
#       .mdev-test/ は作られない
```

続けて実起動する。**ここからセッションが増える。**

```sh
mdev test <ブランチ名>
```

**期待**:

- 新しい端末の窓が開き、その中でテストセッションが立ち上がる
- セッション名は `test-<ブランチ名>` を 24 文字へ切り詰めたもの
- データは `<worktree>/.mdev-test/` に入り、**`~/.claude-conductor` は変わらない**
- レイアウトのペインは `<worktree>/.mdev-test/bin/mdev` を指す（設置済みのバイナリではない）

```sh
grep -c "$PWD/.worktree/<ブランチ名>/.mdev-test/bin/mdev"     ~/projects/mdev-go/.worktree/<ブランチ名>/.mdev-test/layouts/multi.kdl   # 期待: 5
ls ~/.claude-conductor/layouts/multi.kdl   # 期待: 変わっていない
```

**確認後は必ず片付ける。** テストセッションを残すと、次に `mdev` を開いたときの一覧に並び、掃除の対象にもなる。

```sh
zellij kill-session test-<切り詰めた名前>
zellij delete-session test-<切り詰めた名前>
rm -rf ~/projects/mdev-go/.worktree/<ブランチ名>/.mdev-test
zellij list-sessions    # 期待: テストセッションが消えている
```

### 8. `mdev update`

```sh
mdev update
```

**期待（既に最新のとき）**: `既に最新です（vX.Y.Z）。` の 1 行だけ。**install は走らず、ファイルは 1 つも書き換わらない。**

**期待（新しい版があるとき）**: 2 段で終わる。

1. 1 回目: `mdev 自身を <旧> -> <新> に更新します...` → 自己置換 → **「もう一度 `mdev update` を実行してください」**の案内でそこで終わる
   （今動いているのは置き換える前の中身なので、そのまま設定を貼ると古い実装で貼ることになる）
2. 2 回目: 自分は最新なので自己置換は飛ばし、`<旧> -> <新> に更新します...` → install が設定を貼り直す → `✅ <新> に更新しました。`

```sh
mdev version    # 期待: 2 回目の後は新しい版を名乗る
cat ~/.claude-conductor/VERSION   # 期待: mdev version と同じ
```

この流れは v0.13.1 の配備で実証済みである。

### 9. `mdev check-update`

```sh
mdev check-update --force
```

**期待**: 案内は **1 行だけ**（mdev 本体のみ）。REPO_URL が mdev-go を指しているため、conductor の行は出ない。

## 問題が出た場合

### 戻し方（Go 版のまま戻す）

```sh
rm -rf ~/.claude-conductor && mv ~/.claude-conductor.bak-6-3 ~/.claude-conductor
mv ~/.claude/settings.json.bak-6-3 ~/.claude/settings.json
mv ~/.codex/config.toml.bak-6-3 ~/.codex/config.toml
exec zsh
```

### Shell 版へ戻す

```sh
cd ~/projects/claude-conductor && git checkout <最終タグ> && ./install.sh
```

### 個別の症状

- **ペインが即死する**: `~/.claude-conductor/layouts/multi.kdl` の呼び出しを確認する。`mdev install` を再実行すれば書き換わる
- **hook が効かない**: `jq '.hooks' ~/.claude/settings.json` で `bin/mdev hook` を指しているか確認する
- **codex のタスクが Dashboard に出ない**: `grep notify ~/.codex/config.toml` を確認する。別ツールが notify を使っている場合は install が触らず案内だけ出しているので、その notify から `mdev codex notify '<payload>'` を呼ぶ必要がある
- **`mdev` が「レイアウトがありません」と言う**: `mdev install` を実行する

## 開発側の確認（任意）

隔離環境での通し確認は Go のテストとは別にスクリプトがある。

```sh
make build && scripts/verify-install-isolated.sh bin/mdev
# 期待: 28 件成功 / 0 件失敗
```

**実環境には触れない。** HOME・CONDUCTOR_HOME・CODEX_HOME に加えて **TMPDIR も隔離する**のが要点で、zellij のソケット置き場が `$TMPDIR/zellij-<uid>` で決まるためである。ここを実環境のままにすると、検証で作ったセッションが利用者の一覧に並び、掃除の対象にもなる。スクリプトは TMPDIR が一時ディレクトリの根そのものを指していたら起動を拒否する。

`mdev test` の**実起動はこのスクリプトでは行わない**（隔離のしようがない副作用のため）。実起動の確認は上記の項目 7 で行う。
