# mdev-go

[claude-conductor](https://github.com/k-kudo-hub/claude-conductor)(Shell Script 製の `mdev`)を Go でリライトするプロジェクト。

Zellij 上で複数のコーディングエージェント(Claude Code / Codex CLI)セッションをダッシュボードから統括する。

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

### Shell 版へ戻す

Shell 版(claude-conductor)へは、そのリポジトリの最終タグから戻せる。

```sh
git clone https://github.com/k-kudo-hub/claude-conductor && cd claude-conductor && ./install.sh
```

## 使い方

```sh
mdev              # 今いるディレクトリのセッションへ入る(無ければ作る)
mdev <名前>       # 名前を指定して入る
mdev --new        # 時刻付きで新しく作る
dev               # 単一の開発セッション(エージェント + エディタ + git)
zs                # 既存のセッションを選んで入る
mdev test <worktree>   # worktree のソースを隔離環境で試す
```

ダッシュボードでは `n` で新しいタスクを作る。

## ステータス

Shell 版からの移行が最終段階。計画は [docs/adr/](docs/adr/) を参照。

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
