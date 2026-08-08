# ADR-0002: ports & adapters によるアーキテクチャ設計

- Status: Accepted
- Date: 2026-08-08

## Context

現行の Shell Script 版は、状態遷移ロジック(タスクの Notification / Stop / Waiting、codex の done 確定遅延、neutral スクリーン判定)がループスクリプトと hook スクリプトに分散しており、1 つの状態遷移を変更するのに複数ファイルの修正が必要だった。また Zellij・ファイルシステムへの依存がロジックに直接埋め込まれており、テストで差し替える標準手段がなかった。

リライトでは「機能の足し算」で構造が崩れないよう、依存の方向を最初に固定し、それを機械的に検証できる設計手法を採る必要がある(検証の仕組みは ADR-0003)。

## Decision

ports & adapters(ヘキサゴナルアーキテクチャ)を採用し、以下のパッケージ構成とする。

```
mdev-go/
├── cmd/mdev/            # main のみ。DI の組み立て
├── internal/
│   ├── cli/             # サブコマンド定義(cobra)。app を呼ぶだけ
│   ├── tui/             # ダッシュボード TUI(Bubble Tea)。app を呼ぶだけ
│   ├── app/             # ユースケース。port(interface)をここで定義する
│   ├── domain/          # 純粋ロジック。標準ライブラリ以外に依存しない
│   └── infra/           # adapter。app の port を実装する
│       ├── zellij/      # zellij action の実行・出力パース
│       ├── store/       # pending / registry / config のファイル入出力
│       ├── agent/       # claude / codex プロセスの起動・resume
│       ├── notify/      # terminal-notifier
│       └── github/      # リリース取得(update)、作業ログ push
├── layouts/             # Zellij レイアウト(KDL)
└── docs/adr/
```

### 依存の方向(これのみを許す)

```
cli ─┐
     ├─→ app ─→ domain
tui ─┘    ↑
infra ────┘ (port の実装として)
cmd/mdev ─→ 全パッケージ(組み立てのみ)
```

- **domain** は他のどの internal パッケージにも依存しない。タスク状態機械(Notification / Stop / Waiting / done 確定遅延)、スクリーンパターン判定、タスク名の採番、料金計算をここに置く。すべて「値を受け取り値を返す」純粋関数または純粋な状態機械として書き、時刻は `time.Now()` を呼ばず引数で受け取る
- **app** はユースケースごとに port(interface)を定義し、domain を使って処理を組み立てる。例: `HandleHookEvent`、`RefreshDashboard`、`CreateTask`、`RestoreSession`
- **infra** は port を実装する。Zellij CLI 呼び出し、ファイル入出力、プロセス起動はすべてここに閉じ込める
- **cli / tui** は入出力の変換のみを行い、業務判断を持たない

### 採用ライブラリ

| 用途 | ライブラリ | 選定理由 |
|------|-----------|----------|
| CLI フレームワーク | [spf13/cobra](https://github.com/spf13/cobra) | サブコマンド多数(hook / dashboard / task / restore / update)の構成に適し、Go CLI で最大の採用実績を持つ |
| TUI | [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) v2 | Model-Update-View 構造が「状態を純粋に持ち、描画と分離する」本設計と一致する。v2 が 2026-02-23 にリリース済みで、fzf 相当の選択 UI も [bubbles](https://github.com/charmbracelet/bubbles) で実装できる |
| JSON / 正規表現 / HTTP | 標準ライブラリ | jq を置き換える。スキーマは struct で定義する |

ライブラリの追加はこの表(および今後の ADR)に載せたものだけを許可する(機械的な強制は ADR-0003 の depguard で行う)。

### 状態機械を domain に集約する

現行実装で最も不具合修正が集中した箇所(スクリーン検出の誤検知対策、done 確定の遅延判定)は、「観測されたスクリーン状態 + 前回状態 + 現在時刻」を入力に「次の状態 + 実行すべき副作用」を返す純粋関数としてモデル化する。副作用(pending ファイル書き込み、フォーカス移動)は app 層が port 経由で実行する。これにより誤検知シナリオ(スピナーが 1 フレーム消える、ビューアが承認プロンプトを引用する)をテーブル駆動テストで網羅できる。

## Consequences

### 得られるもの

- 状態遷移の変更が domain の 1 パッケージに閉じる
- domain / app のテストが実プロセス・実ファイルなしで実行でき、シナリオ網羅が現実的になる
- Zellij の出力形式変更(バージョン差)への対応が infra/zellij に閉じる

### 失うもの・制約

- レイヤーを跨ぐたびに interface と型変換を書くコストが発生する。小さな機能でも cli → app → domain の 3 箇所に触れる
- port の粒度設計を誤ると infra の詳細が app に漏れる。port は「ユースケースが必要とする操作」単位で定義し、Zellij のコマンド体系をそのまま interface にしない
- Bubble Tea v2 は破壊的変更を含む初のメジャーバージョンであり、v1 向けの情報がそのまま使えない場面がある
