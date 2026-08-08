# ADR-0003: 内部品質を担保するガードレール

- Status: Accepted
- Date: 2026-08-08

## Context

現行の Shell Script 版は機能を都度足し算する形で開発され、構造の劣化を検出する仕組みがなかった。ADR-0002 で依存の方向を定めたが、規約は機械的に強制されなければレビュー漏れで崩れる。リライトでは「設計違反・品質低下を CI が落とす」状態を最初のコミットから維持する。

## Decision

以下のガードレールを、機能実装より先にセットアップする(フェーズ 1 の最初のタスク)。

### 1. アーキテクチャ境界の強制 — go-arch-lint

[fe3dback/go-arch-lint](https://github.com/fe3dback/go-arch-lint) を採用し、ADR-0002 の依存方向を `.go-arch-lint.yml` に定義して CI で検証する。

- `domain` は他 internal パッケージへの依存禁止
- `app` は `domain` のみ許可
- `cli` / `tui` は `app` のみ許可(相互参照禁止)
- `infra/*` は `app`(port 定義)と `domain`(型)のみ許可

依存方向の変更は、この YAML の変更 = ADR の改訂を伴う PR としてレビューされる。

### 2. Lint — golangci-lint v2

[golangci-lint](https://golangci-lint.run/)(2026-05-06 時点の最新は v2.12.2)を採用する。既定の有効リンター(govet, staticcheck 系)に加え、最低限以下を有効にする。

- **depguard**: 許可外ライブラリの import を禁止(ADR-0002 の採用ライブラリ表を許可リストとして反映)
- **errcheck / errorlint**: エラーの握り潰し・比較ミスの検出
- **gocritic**: 一般的なコードスメル検出

バージョンは `go.mod` の tool ディレクティブ等でリポジトリに固定し、ローカルと CI で同一バージョンを使う。

### 3. テスト — TDD とカバレッジの層別基準

- 現行リポジトリで確立した方針「テストは実装の再実装ではなく本体コードを直接検証する」を引き継ぐ
- domain の状態機械はテーブル駆動テストで書き、現行版で実際に踏んだ誤検知シナリオ(done 確定遅延、neutral スクリーン、スピナーのフレーム落ち)を初期テストケースとして移植する
- データ互換(ADR-0001)は、現行版が生成した実ファイルを fixture としたゴールデンテストで担保する
- カバレッジ基準は層で分ける: **domain / app は 90% 以上**、リポジトリ全体は 70% 以上。CI で下回ったら fail
- `go test -race` を常時有効にする(ループ・hook の並行書き込みが現行版で実際に問題になり、`lock-lib.sh` が導入された経緯があるため)

### 4. CI — GitHub Actions

PR ごとに以下をすべて実行し、いずれかの失敗でマージ不可とする。

1. `gofmt` 差分ゼロ検査
2. golangci-lint
3. go-arch-lint
4. `go test -race -cover`(カバレッジ閾値検査を含む)
5. `go build`(macOS)

リリースフローは現行リポジトリの方式(PR への `bump:major/minor/patch` ラベル必須 + マージ時自動タグ)を踏襲する。バイナリ配布の方式(GoReleaser 等)は配布を始めるフェーズ 5 で別 ADR として決める。

### 5. 設計変更の入口 — ADR

docs/adr/README.md の運用ルールに従い、アーキテクチャ・採用ライブラリ・品質基準・データ互換性に触れる変更は ADR を先に書く。go-arch-lint / depguard の設定変更を含む PR は、対応する ADR の追加・改訂を含まなければならない。

## Consequences

### 得られるもの

- 「足し算開発」をしても依存方向・ライブラリ追加・カバレッジ低下が CI で止まり、内部品質が構造的に維持される
- 品質基準が設定ファイル(`.go-arch-lint.yml`、`.golangci.yml`、CI 定義)として明文化され、レビューの属人性が下がる

### 失うもの・制約

- 初期セットアップのコストを機能実装より先に払う
- カバレッジ閾値・リンター設定が厳しすぎると開発速度を落とす。閾値の変更自体は禁止しないが、ADR 改訂として理由を記録する
- go-arch-lint / golangci-lint のバージョン追従という保守作業が増える
