# 品質ガードレールのセットアップ(フェーズ1 最初のタスク)

## 概要

ADR-0003 の決定に従い、機能実装より先に「設計違反・品質低下を CI が落とす」状態を構築する。
各ガードレールは**意図的な違反で fail することを確認してから**設定を確定する(ガードレール自体のテストファースト)。

## 事前調査の結果

- ローカルの Go: go1.25.5 darwin/arm64(go.mod は `go 1.25` とする。golangci-lint の Go 1.26 対応は v2.9.0 以降のため lint 側の制約はない)
- カバレッジ層別閾値: [vladopajic/go-test-coverage](https://github.com/vladopajic/go-test-coverage) v2 が package 単位の閾値と正規表現による上書きルールをサポートしており、domain/app 90%・全体 70% を表現できる(GitHub Action あり)
- bump ラベルのリリースフロー: claude-conductor の `.github/workflows/bump-label-check.yml`・`tag.yml`・`scripts/bump-version.sh` を移植する

## TODO

- [x] `.gitignore` を作成(バイナリ、cover.out、.worktree/)
- [x] `go.mod` を初期化(module `github.com/k-kudo-hub/mdev-go`、go 1.25)
- [x] ADR-0002 のパッケージ骨格を作成(`cmd/mdev` の main、`internal/{cli,tui,app,domain,infra}` の doc.go)
- [x] domain に最小の実装とテストを 1 組作成(タスク名の重複採番 `UniqueTaskName`。claude-conductor の `ensure_unique_tab_name` を移植)
- [x] `.go-arch-lint.yml` を作成し、ADR-0002 の依存方向を定義
- [x] go-arch-lint の違反検出を確認(domain→infra / cli→tui / app→infra の 3 パターンで fail を確認し、戻す。記録: `20260808-guardrail-evidence.md`)
- [x] `.golangci.yml` を作成(depguard 許可リスト = cobra / bubbletea v2 / bubbles v2 / golang.org/x / 標準ライブラリ、errcheck / errorlint / gocritic 有効。domain は標準ライブラリのみの追加ルール)
- [x] golangci-lint の違反検出を確認(許可外 import / domain からの許可ライブラリ import / 握り潰しエラー / `==` 比較 / `%v` ラップ / if-else 連鎖で fail を確認し、戻す。記録: `20260808-guardrail-evidence.md`)
- [x] `.testcoverage.yml` を作成(全体 70%、`internal/domain`・`internal/app` は 90% の上書きルール。3 パターンの違反検出と空パッケージの扱いを `20260808-guardrail-evidence.md` に記録)
- [x] ツールのバージョン固定方法を確定し適用(go.mod tool ディレクティブを検証したが、3 ツール同居で `go mod tidy` が失敗し `go` ディレクティブも 1.26 に上がるため不採用。Makefile + 固定バージョン `go run` を採用。検証結果は `20260808-guardrail-evidence.md`)
- [x] CI ワークフロー `.github/workflows/ci.yml` を作成(gofmt 差分ゼロ / golangci-lint / go-arch-lint / go test -race -cover + go-test-coverage / go build、macOS ランナー)
- [x] `bump-label-check.yml`・`tag.yml`・`bump-version.sh` を claude-conductor から移植
- [x] `bump:major` / `bump:minor` / `bump:patch` ラベルを mdev-go リポジトリに作成(gh label create で 3 件作成済み)
- [x] README に開発手順(ビルド・テスト・lint の実行方法)を追記

## 完了条件

- PR の CI で gofmt / golangci-lint / go-arch-lint / テスト+カバレッジ閾値 / ビルド がすべて実行され、緑になる
- 各ガードレールが違反を実際に検出することを確認済み(確認手順と結果を PR 説明に記載)
- ローカルでも CI と同一バージョンのツールで同じ検査を実行できる
- bump ラベルなしの PR が bump-label-check で fail する

## 備考

- ブランチ保護(必須チェック指定)はガードレール一式がマージされ CI のジョブ名が確定してから設定する(このタスクの最後にユーザーへ確認)
- golangci-lint のバージョンは 2026-05-06 時点の最新 v2.12.2 を基準にする
- リンター・閾値の設定変更は今後 ADR 改訂を伴う(ADR-0003)
