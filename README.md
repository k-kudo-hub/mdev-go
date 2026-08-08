# mdev-go

[claude-conductor](https://github.com/k-kudo-hub/claude-conductor)(Shell Script 製の `mdev`)を Go でリライトするプロジェクト。

Zellij 上で複数のコーディングエージェント(Claude Code / Codex CLI)セッションをダッシュボードから統括する。

## ステータス

設計フェーズ。リライト計画は [docs/adr/](docs/adr/) を参照。

- [ADR-0001: Shell Script 版 mdev を Go でリライトする](docs/adr/0001-rewrite-mdev-in-go.md)
- [ADR-0002: ports & adapters によるアーキテクチャ設計](docs/adr/0002-ports-and-adapters-architecture.md)
- [ADR-0003: 内部品質を担保するガードレール](docs/adr/0003-quality-guardrails.md)

## License

MIT
