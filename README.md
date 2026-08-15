# mdev-go

Zellij 上で複数のコーディングエージェント(Claude Code / Codex CLI)セッションを、1 つのダッシュボードから統括する CLI。

タスクごとにタブを立て、どれが応答待ちでどれが終わったのかを一覧で見ながら、番号 1 つで行き来する。終わったタスクは会話の要約つきでログへ残してから畳む。

## これは claude-conductor の後継である

もとは [claude-conductor](https://github.com/k-kudo-hub/claude-conductor) という Shell Script 製の `mdev` だった。機能が増えるにつれて、jq と awk に依存した状態管理・シェル依存の文字列処理・テストの書きにくさが積み上がり、1 つの変更が別の場所を静かに壊すようになった。そこで**振る舞いを 1 つずつ突き合わせながら** Go へ移し、このリポジトリへ移転した。

移植は勘に頼らず、現行版へ同じ入力を流して得た出力を期待値として固定する方法(golden テスト)で進めた。`scripts/gen-golden-*.sh` がその記録である。設定ファイルや作業ログの形式は Shell 版と互換なので、**移行しても過去のデータはそのまま読める**。

移行の判断は [docs/adr/](docs/adr/) に残してある。

## インストール

macOS(Apple Silicon / Intel)向け。`curl` と `zellij` が要る。

```sh
curl -fsSL https://raw.githubusercontent.com/k-kudo-hub/mdev-go/main/install.sh | bash
```

`install.sh` がするのは「最新のバイナリを取得して SHA-256 で照合し、
`~/.claude-conductor/bin/mdev` へ置いて `mdev install` を呼ぶ」ところまでである。
設定(hooks・codex の notify・レイアウト・config のマージ)はすべて `mdev install`
が行う。**何度実行しても同じ状態になり、2 回目はファイルを 1 つも書き換えない。**

最後に案内される 1 行を `.zshrc` へ足すと、`mdev` / `dev` / `zs` が使えるようになる。

```sh
source "$HOME/.claude-conductor/init.zsh"
```

### 更新と取り除き

```sh
mdev update       # 新しい版へ自己置換し、設定を貼り直す
mdev uninstall    # hooks と codex notify を外し、データも消す
mdev uninstall --keep-data   # 設定の解除だけ(作業ログは残す)
```

### バイナリを手で置く場合

**ブラウザからダウンロードしたバイナリは macOS の隔離属性で起動できない。**
その場合は属性を外す。

```sh
xattr -d com.apple.quarantine ~/.claude-conductor/bin/mdev
```

`install.sh` は curl 経由なので隔離属性は付かない(念のため外す処理も入っている)。

### settings.json を元へ戻す

`mdev install` は Claude Code の `~/.claude/settings.json` の hooks を書き換える前に、**同じディレクトリへ退避する**。意図しない結果になった場合はそこから戻せる。

```sh
ls -t ~/.claude/settings.json.mdev-backup-*   # 新しい順に並ぶ
cp ~/.claude/settings.json.mdev-backup-<タイムスタンプ> ~/.claude/settings.json
```

退避が作られるのは **hooks を実際に書き換えたときだけ**である(何も変わらない実行では作られないので、バックアップが際限なく増えることはない)。ファイル名の時刻は UTC。

hooks だけを外したい場合は `mdev uninstall --keep-data` でも戻せる(mdev を指す hook だけを取り除き、作業ログやタスクの記録は残す)。

### Shell 版へ戻す

Shell 版(claude-conductor)へは `v0.9.1` から戻せる。

```sh
git clone https://github.com/k-kudo-hub/claude-conductor
cd claude-conductor && git checkout v0.9.1 && ./install.sh
```

`v0.9.1` が Shell 実装として完結している最後のタグである。

## 使い方

### セッションを開く

```sh
mdev              # 今いるディレクトリのセッションへ入る(無ければ作る)
mdev <名前>       # 名前を指定して入る
mdev --new        # 時刻付きで新しく作る
dev               # 単一の開発セッション(エージェント + エディタ + git)
zs                # 既存のセッションを選んで入る
```

`mdev` は **attach-or-create** である。同じディレクトリから何度実行しても同じセッションへ戻るので、時刻付きのセッションが積み上がらない。

### ダッシュボードの操作

| キー | 動作 |
|---|---|
| `n` | 新しいタスクを作る |
| `<番号>` | そのタスクのタブへ移る |
| `d` + `<番号>` | そのタスクを削除する(作業ログをアップロードしてから閉じる) |
| `r` | News を取り直す(News ペイン) |

タスクタブの下部にも操作バーが出る(`m`: ダッシュボードへ / `w`: 返答待ちへ退避 / `dd`: 削除)。

### その他のコマンド

```sh
mdev install           # 設置と移行(何度実行しても同じ状態になる)
mdev update            # 新しい版へ自己置換し、設定を貼り直す
mdev uninstall         # 取り除く(--keep-data で設定の解除だけ)
mdev sessions clean    # 溜まったセッションと残骸を片付ける
mdev news fetch        # AI 関連ニュースを取得する
mdev test <worktree>   # worktree のソースを隔離環境で試す(開発用)
mdev version           # 版を表示する
```

## 設計

`cli` / `tui` → `app` → `domain` の一方向で、外部とのやり取りは `infra` が `app` の port を実装する形に閉じている(ports & adapters)。`domain` は標準ライブラリだけで動き、時刻すら受け取る側にある。依存の向きは CI で機械的に検査している。

| 層 | 役割 |
|---|---|
| `internal/domain` | 判断そのもの(純粋関数)。Shell 版との互換はここで固定する |
| `internal/app` | 手順の組み立てと port の定義 |
| `internal/infra` | zellij・git・ファイル・外部 CLI との接続 |
| `internal/cli` / `internal/tui` | 入出力の変換と画面 |
| `assets` | 配布物(レイアウト・既定設定)の埋め込み |

## ステータス

Shell 版からの移行は完了段階。判断の記録は [docs/adr/](docs/adr/) を参照。

- [ADR-0001: Shell Script 版 mdev を Go でリライトする](docs/adr/0001-rewrite-mdev-in-go.md)
- [ADR-0002: ports & adapters によるアーキテクチャ設計](docs/adr/0002-ports-and-adapters-architecture.md)
- [ADR-0003: 内部品質を担保するガードレール](docs/adr/0003-quality-guardrails.md)
- [ADR-0004: 配布モデルと最終統合](docs/adr/0004-distribution-and-final-integration.md)

## 開発

### 必要なもの

- Go 1.25 以上(`go.mod` の go ディレクティブは `1.25`)
- GNU Make

lint / アーキテクチャ検査 / カバレッジ検査のツールは個別にインストールしない。
`Makefile` がバージョンを固定した `go run <module>@<version>` で実行するため、
ローカルと CI で必ず同じバージョンが使われる。

| ツール | バージョン | 用途 |
|--------|-----------|------|
| [golangci-lint](https://golangci-lint.run/) | v2.12.2 | Lint(depguard / errcheck / errorlint / gocritic ほか) |
| [go-arch-lint](https://github.com/fe3dback/go-arch-lint) | v1.17.0 | ADR-0002 の依存方向の検証 |
| [go-test-coverage](https://github.com/vladopajic/go-test-coverage) | v2.18.3 | カバレッジ閾値の検証 |

初回実行時はツールのビルドのため時間がかかる。go-test-coverage を v2.18.4 以降に
上げる場合は Go 1.26 が必要になる(CI の setup-go は `GOTOOLCHAIN=local` を設定する
ため、ツールチェーン自動取得では解決されない)。

### よく使うコマンド

```sh
make help        # ターゲット一覧
make check       # CI と同一の検査を一括実行(fmt-check → lint → arch → cover → build)
```

個別に実行する場合:

```sh
make fmt         # gofmt で整形する
make fmt-check   # gofmt の差分がないことを検査する
make lint        # golangci-lint run ./...
make arch        # go-arch-lint check(ADR-0002 の依存方向)
make test        # go test -race -covermode=atomic -coverprofile=cover.out ./...
make cover       # make test の後にカバレッジ閾値を検査する
make build       # bin/mdev をビルドする
make install     # $CONDUCTOR_HOME/bin/mdev(既定 ~/.claude-conductor/bin)へ配置する
make clean       # bin/ と cover.out を削除する
```

### worktree を隔離環境で試す

`mdev test` は worktree のソースから mdev を組み、その worktree の中の
`.mdev-test/` をデータの置き場所にしてセッションを新しい端末の窓に開く。
**設置済みの環境には一切触れない**ため、2 つの worktree を同時に試せる。

```sh
mdev test <ブランチ名>        # .worktree/<ブランチ名> から
mdev test <パス>              # パスを直に指定
mdev test                     # .worktree/ から選ぶ
mdev test <名前> --dry-run    # 起動せずに解決した内容だけを出す
```

### 品質基準

| 検査 | 設定ファイル | 基準 |
|------|--------------|------|
| 依存方向 | `.go-arch-lint.yml` | cli / tui → app のみ、app → domain のみ、infra → app と domain のみ、domain は internal 依存なし |
| Lint | `.golangci.yml` | golangci-lint v2 の standard + depguard / errcheck / errorlint / gocritic |
| 採用ライブラリ | `.golangci.yml`(depguard) | 標準ライブラリ / cobra / bubbletea v2 / bubbles v2 / golang.org/x のみ。domain は標準ライブラリのみ |
| カバレッジ | `.testcoverage.yml` | 全体 70% 以上、`internal/domain` と `internal/app` は 90% 以上 |

これらの設定を変更する場合は、対応する ADR の追加・改訂を同じ PR に含めること(ADR-0003)。

### PR とリリース

- PR には `bump:major` / `bump:minor` / `bump:patch` のいずれかのラベルが必須
  (無い場合は `Bump label check` が落ちる)
- main へマージすると、ラベルに応じた semver タグと GitHub Release が自動生成される

## License

MIT
