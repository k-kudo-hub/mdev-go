# mdev-go

[claude-conductor](https://github.com/k-kudo-hub/claude-conductor)(Shell Script 製の `mdev`)を Go でリライトするプロジェクト。

Zellij 上で複数のコーディングエージェント(Claude Code / Codex CLI)セッションをダッシュボードから統括する。

## ステータス

設計フェーズ。リライト計画は [docs/adr/](docs/adr/) を参照。

- [ADR-0001: Shell Script 版 mdev を Go でリライトする](docs/adr/0001-rewrite-mdev-in-go.md)
- [ADR-0002: ports & adapters によるアーキテクチャ設計](docs/adr/0002-ports-and-adapters-architecture.md)
- [ADR-0003: 内部品質を担保するガードレール](docs/adr/0003-quality-guardrails.md)

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
| [go-test-coverage](https://github.com/vladopajic/go-test-coverage) | v2.19.0 | カバレッジ閾値の検証 |

初回実行時はツールのビルドのため時間がかかる。go-test-coverage は Go 1.26 を要求するため、
実行時に Go ツールチェーンが自動取得される(本体の `go.mod` には影響しない)。

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
make clean       # bin/ と cover.out を削除する
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
