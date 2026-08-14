# 統合 6-2: Shell 全廃の前提実装(ADR-0004 D5 + D4-4 の embed)

## 概要

ADR-0004 実施フェーズ 2。Go 版環境で load-bearing な残存 Shell 3 経路(fetch-news / codex-notify / agent-launch)を Go サブコマンド化し、conductor の資産(layouts / config.default.json / hooks.json)を mdev-go へ移設して go:embed する。これで 6-3(インストーラ Go 化)の前提が揃い、`scripts/` を配布物から消せる状態になる。

## TODO

### A. `mdev news fetch`(ADR D5-1)

- [ ] cli: `mdev news fetch [--force]` のテスト+配線(実装済みの infra/news Fetcher + 保持期間削除をそのまま使用。--force は当日ファイルがあっても再取得、無印は当日ファイルがあればスキップ = fetch-news.sh の挙動互換)
- [ ] 検証: init.zsh 相当の呼び出し(`mdev news fetch`)が fetch-news.sh と同一のファイルを生成すること(既存 golden/28b 相当の期待値と突き合わせ)

### B. `mdev codex notify`(ADR D5-2)

- [ ] conductor の scripts/codex-notify.sh + registry-lib.sh の仕様調査(入力 JSON・pending/registry への書き込み・通知)と domain のテスト+実装(既存 registry/pending domain を再利用)
- [ ] cli+app: `mdev codex notify` 配線(codex の notify 契約: 引数 JSON を受けて非対話で完了)
- [ ] 検証: codex-notify.sh と同一入力 → 同一の pending/registry 変化(golden 方式)

### C. `mdev agent launch`(ADR D5-3)

- [ ] conductor の scripts/agent-launch.sh + task-lib.sh の agent_command 相当の仕様確認と cli+app のテスト+実装(config の agent.command を word-split して exec。task_create.go の同等ロジックを共通化)

### D. 資産の移設と embed(ADR D4-4)

- [ ] conductor から layouts/multi.kdl・layouts/dev.kdl・config.default.json・hooks.json を mdev-go の assets/ へ移設(出所と「conductor 側が正本でなくなる」旨を conductor 側は 6-4 まで現状維持のままにする注記)
- [ ] go:embed のテスト+実装: `mdev assets <name>` または内部 API として、CONDUCTOR_HOME に実ファイルがあれば優先・無ければ embed を返す解決関数(ADR の「カスタマイズの退避路」)
- [ ] config.default.json / pricing の読み込み(store/config.go・pricing.go)を「実ファイル → embed」のフォールバック付きに変更(既存の config.json → config.default.json フォールバックの後段に embed を追加)

### E. 検証・仕上げ

- [ ] make check(カバレッジ閾値維持)
- [ ] 実環境相当の smoke 手順整理: `mdev news fetch` の実フィード実行 / codex notify の実データ入力 / embed フォールバックの動作(CONDUCTOR_HOME の該当ファイルを退避して確認)

## 完了条件

- `mdev news fetch` / `mdev codex notify` / `mdev agent launch` が Shell 版と同一の観測可能な結果を出す(golden 方式で機械検証)
- embed された資産が「実ファイル優先・無ければ embed」で解決される
- 全テスト・lint・カバレッジ通過

## 備考

- **このフェーズでは実環境の切り替えはしない**(init.zsh の呼び出し先変更・codex config.toml の書き換え・layouts の実配置変更は 6-3 の `mdev install` で行う)。6-2 は「Go 側に受け皿を完成させる」まで
- このフェーズのマージで生まれるリリース(v0.12.0 想定)が **6-1 第 2 段(自己更新の実演)の素材**になる: v0.11.0 のバイナリで `mdev update` → v0.12.0 へ自己置換、をユーザーテストとして実施
- dev.kdl の Agent ペインは `${CONDUCTOR_HOME:-...}/scripts/agent-launch.sh` 固定のため、embed 版 dev.kdl では `bin/mdev agent launch` を指す形に書き換えて移設(実環境への適用は 6-3)
