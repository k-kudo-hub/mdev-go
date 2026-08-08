# ADR-0001: Shell Script 版 mdev を Go でリライトする

- Status: Accepted
- Date: 2026-08-08

## Context

[claude-conductor](https://github.com/k-kudo-hub/claude-conductor) は Zellij 上で複数のコーディングエージェントセッションを統括するツール(コマンド名 `mdev`)で、Shell Script で実装されている。2026-08-08 時点の規模は以下の通り。

- 実装スクリプト(`scripts/*.sh` + `install.sh` + `uninstall.sh` + `init.zsh`): 約 3,200 行
- テスト(`test.sh`): 3,810 行
- 外部依存: Zellij ≥ 0.40(スクリーン検出には ≥ 0.44)、jq、fzf、terminal-notifier(任意)

Shell Script 実装には以下の構造的な制約がある。

1. **macOS 標準の bash 3.2 で動作させる必要がある**。bash 4+ の機能(`read -i`、連想配列)が使えず、都度分岐や回避策を書いている
2. **型がない**。JSON の読み書きはすべて jq の文字列組み立てであり、pending ファイル・task registry・config のスキーマ不整合をテスト実行まで検出できない
3. **依存注入の仕組みがなく、テストが重い**。`test.sh` は本体関数を直接呼ぶ方針(再実装の検証ではなく実コードの検証)を取っているが、Zellij・ファイルシステム・時刻への依存を差し替える標準的な手段がなく、テストコードが実装の 1.2 倍の行数に達している
4. **機能を都度足し算する形で開発してきた**ため、状態遷移(Notification / Stop / Waiting、done 確定の遅延判定)のロジックが複数のループスクリプト(`dashboard-loop.sh`、`done-loop.sh`、`waiting-loop.sh`)と hook スクリプトに分散している

一方で、現行版は日常利用されており、機能(ダッシュボード、Done/Waiting ペイン、hooks 連携、codex のスクリーン検出、セッション復元、作業ログアップロード、セルフアップデート)は安定している。失ってよい機能はない。

## Decision

`mdev` を Go の単一バイナリとしてフルリライトする。開発は claude-conductor リポジトリへ直接コミットせず、新規リポジトリ **k-kudo-hub/mdev-go** で行う。

### リライトの方針

- **単一バイナリ + サブコマンド構成**にする。現行の各スクリプトは `mdev` のサブコマンドに対応させる(`mdev hook <event>`、`mdev dashboard`、`mdev task create`、`mdev restore`、`mdev update`)。jq / fzf への外部依存はバイナリ内に取り込む(JSON は標準ライブラリ、選択 UI は TUI で実装)
- **データ互換を維持する**。pending ファイル(`~/.claude-pending/{session}/*.json`)、task registry(`$CONDUCTOR_HOME/tasks/{session}/{session_id}.json`)、config(`~/.claude-conductor/config.json`)の形式は現行版と互換にする。これにより移行期間中、現行版と Go 版が同じデータを読み書きでき、hook だけ Go 版に差し替える・ダッシュボードだけ Go 版にするという段階移行が成立する
- **機能パリティを移行完了の条件とする**。現行 README に記載された全機能が対象

### 移行フェーズ

| フェーズ | 内容 | 置き換え対象スクリプト |
|----------|------|------------------------|
| 1 | config 読み込み、pending ストア、task registry、hook サブコマンド | `pending-*.sh`、`record-output.sh`、`task-lib.sh`、`registry-lib.sh`、`lock-lib.sh` |
| 2 | ダッシュボード TUI(Dashboard / Done / Waiting / News ペイン) | `dashboard-loop.sh`、`done-loop.sh`、`waiting-loop.sh`、`news-loop.sh`、`fetch-news.sh` |
| 3 | タスク作成・セッション管理・Zellij 制御 | `task-create-loop.sh`、`task-control.sh`、`agent-launch.sh`、`waiting-toggle.sh`、`init.zsh` の関数群 |
| 4 | スクリーン検出(codex)、セッション復元 | `screen-detect-lib.sh`、`codex-notify.sh`、`restore-session.sh`、`restore-task.sh` |
| 5 | セルフアップデート、作業ログアップロード、インストーラ | `update.sh`、`update-lib.sh`、`check-update.sh`、`upload-log.sh`、`install.sh`、`uninstall.sh` |

各フェーズ完了時点で、実環境(`mdev-test` 相当の隔離セッション)での動作確認を行ってから次フェーズへ進む。

### やらないこと

- claude-conductor リポジトリへの機能追加(移行期間中はバグ修正のみ)
- リライトと同時の機能追加・仕様変更。現行仕様の再現を先に完了させる(仕様変更は移行完了後に ADR を立てて行う)

## Consequences

### 得られるもの

- bash 3.2 制約からの解放。言語機能の分岐が不要になる
- 型によるスキーマの静的検証。pending / registry / config の不整合がコンパイル時に検出される
- interface による依存注入が言語標準で使え、Zellij・ファイルシステム・時刻を差し替えた高速なユニットテストが書ける
- jq / fzf のインストール不要化(配布物は単一バイナリ + レイアウト KDL)
- 状態遷移ロジックを 1 箇所(ドメイン層)に集約できる(設計は ADR-0002)

### 失うもの・リスク

- スクリプトを直接編集して即試す運用はできなくなる(ビルドが必須になる)
- Zellij の挙動(`dump-screen`、`list-panes` の出力形式)に依存する部分は Go にしても外部依存のまま残る。ここは統合テストではなく実環境確認でカバーする
- 移行期間中は 2 実装の併存によりデータ形式の変更が凍結される

### 制約

- データ形式の互換性が破られていないことを、現行版の実データ(fixture 化したもの)を入力とするテストで担保する
