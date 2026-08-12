# セッション蓄積の再発対策: 自動掃除 + 未アタッチ減速 + clean コマンド

## 概要

detached セッションの無限ポーリング蓄積(zellij サーバ劣化 → タブ作成遅延・5:5 分割・ヘッダ崩れの根本原因)の再発を防ぐ。ウィンドウを閉じてもセッションが生き続ける zellij の仕様に対し、(A) 起動時の自動掃除で根を絶ち、(B) 残っても無害になるよう未アタッチ時はポーリングを減速し、(C) 手動の一括回収手段を持つ。

## 前提(調査済み)

- `zellij action list-clients` が attach 中クライアントを列挙する(0 行 = detached)— B の検出手段として実機確認済み
- mdev のタスクは registry(`~/.claude-conductor/tasks/<session>/`)から `--resume` 付きで復元されるため、**detached セッションを kill しても作業は失われない**(同名 `mdev` 起動で復元される。ユーザーテスト 04 で実証済み)
- EXITED セッションの resurrection は mdev では不使用(init.zsh が明示的に delete → 再作成する設計)

## TODO

### A. `mdev sessions clean` + 起動時自動掃除(mdev-go)

- [x] domain: セッション一覧パースと掃除対象判定のテスト+実装(list-sessions 出力 → EXITED / alive の分類、mdev 管理セッションの識別は「bin/mdev pane を実行しているサーバ」で行う)
- [x] domain: ps 出力からのゾンビ検出のテスト+実装(zellij --server プロセスと list-sessions の突き合わせ、PPID=1 の zellij action クライアント検出)
- [x] infra: zellij セッション操作(kill-session / delete-session / list-clients)と プロセス操作(ps / kill)の adapter(すべて既存 proc 経由・タイムアウト付き)
- [x] app: SessionCleaner usecase のテスト+実装 — (1) EXITED を全 delete (2) detached な mdev セッション(list-clients が 0 行)を kill+delete (3) ゾンビサーバを TERM→KILL (4) 孤児クライアント(PPID=1 の zellij action)を kill。**使用中(attach あり)セッションには絶対に触れない**。dry-run 対応
- [x] cli: `mdev sessions clean [--dry-run] [--auto]` 配線(--auto は起動前掃除用: 出力を 1〜2 行に抑え、失敗しても exit 0 = セッション起動を止めない)

### B. 未アタッチ時のポーリング減速(mdev-go)

- [x] domain: 減速判定の純粋関数のテスト+実装(attach 確認は 30 秒間隔。未アタッチが確認できたら **読み直しを止め**、30 秒ごとの attach 確認だけを続ける)
- [x] レビュー指摘の反映(kill 安全性: 空白入り名のパース・サーバ判定の厳密化・消す直前の再確認・作成猶予・確認予算 / 機能不全: セッション 0 件・task-control の配線 / 堅牢化: 判断不能は attach 扱い・古い確認結果の破棄・取得中の着弾)
- [x] infra+tui: poller への組み込み(`zellij action list-clients` の呼び出しは既存 zellij adapter 経由・10 秒上限。list-clients 自体の失敗は「attach あり」扱い = 安全側)
- [x] attach 復帰の即時化: キー操作を attach の証拠として扱い即座に通常速度へ。確認で復帰を検知したときは読み直しを 1 本出す(最大 30 秒)

### C. conductor 側: init.zsh から自動掃除を呼ぶ(小 PR) — **本タスクの対象外**(別エージェントが担当)

- [ ] init.zsh の mdev 関数: セッション作成前(fetch-news の隣)に `$CONDUCTOR_HOME/bin/mdev sessions clean --auto` を実行(bin/mdev が実行可能なときのみ・失敗無視)。test.sh にテスト追加

### D. 検証・仕上げ

- [x] make check(カバレッジ閾値維持)+ 実環境での動作確認手順の整理(detached セッションを 1 つ作って clean が回収すること、使用中セッションが無傷なこと)

## 完了条件

- `mdev sessions clean --dry-run` が回収対象を列挙し、実行で EXITED・detached mdev セッション・ゾンビサーバ・孤児クライアントが消える。使用中セッションは無傷
- `mdev` 起動時に自動掃除が走り、蓄積が起きない(--auto は無言に近く、失敗してもセッション起動を妨げない)
- 未アタッチのセッションは読み直しを止め、30 秒ごとの attach 確認だけを行う。attach 後はキー操作で即座に、無操作でも最大 30 秒で通常へ戻る
- 全テスト・lint・カバレッジ通過(mdev-go)、test.sh 全通過(conductor)

## 備考

- attach 復帰の検知に `tea.WindowSizeMsg` は使えない。実測(zellij 0.44.1 + bubbletea v2.0.8)で、**同じ大きさで attach し直すと端末から何の合図も来ない**ことを確認した(サイズが変わったときだけ SIGWINCH 経由で届く)。そのため確認は自分から行う
- `zellij delete-session` に `--force` は付けない。付けないと zellij 自身が動作中セッションの削除を拒む(実機で確認)ため、最後の砦として効く
- `zellij list-sessions` はセッション 0 件のとき rc=1・標準出力は空・標準エラーに「No active zellij sessions found.」を出す。**どんな失敗も 0 件と読んではならない**(0 件とみなすと生きているサーバがすべてゾンビに見える)
- 対象外(別途): タブ構築のレイアウトファイル化(ADR 先行・フォローアップ)、zellij 本体への issue 報告
- kill の安全策: 「mdev 管理セッション」の判定は bin/mdev pane プロセスの有無で行い、`dev` 等の手動セッションは EXITED 以外触らない
