# フェーズ2: ダッシュボード系 4 ペインの Bubble Tea 移植

## 概要

Main タブの 4 ペイン(Dashboard / Waiting / Done / News)を `mdev pane <name>` サブコマンド(Bubble Tea v2)として移植する。
Zellij レイアウトのペイン起動コマンドを差し替えるだけで Shell 版と交換でき、表示は ANSI 出力レベルで互換(ゴールデンテストで証明)。完了時にユーザーテスト 02 を実施する。

## スコープの分担(事前調査の依存分析に基づく)

| 機能 | 今回 | 方式 |
|------|------|------|
| 4 ペインの表示・キー入力・ポーリング | ✅ | Go(Bubble Tea v2) |
| Dashboard: ジャンプ(1-9)+ agent 種別による pending クリア | ✅ | Go |
| Dashboard: 削除(d+num)フロー | ✅ | record-output / pending / registry / screen-state / close-tab は **Go 部品**、`upload-log.sh` のみ Shell 同期呼び出し(exit 0=続行 / 非 0=削除中止の契約) |
| Done: restore(r+num) | ✅ | `restore-task.sh <tab> <session> <completed_at>` の Shell 呼び出し(終了コード無視 = 現行同等) |
| News: リロード(r)・URL オープン(1-9) | ✅ | `fetch-news.sh --force` の Shell 呼び出し + open/xdg-open |
| 起動時の `restore-session.sh` | ✅ | Shell 呼び出し(暫定。フェーズ 4 で Go 化) |
| 毎 tick の `screen_detect_tick` | ✅ | Shell ラッパ経由の暫定呼び出し(`bash -c 'source screen-detect-lib.sh; screen_detect_tick <session>'`)。省略すると codex タスクが表示されないため必須 |
| task-control ペイン / タスク作成ペイン | ❌ 次タスク(フェーズ 3) | — |
| screen 検出・restore の Go 化 | ❌ フェーズ 4 | — |
| upload-log の Go 化 | ❌ フェーズ 5 | — |

## 表示互換の要点(調査で確定した現行仕様)

- 描画: ホーム移動 + 上書き + `\033[J`(ちらつき防止)→ Bubble Tea の差分描画で置換。ANSI 色・記号(`■`/`⚡`/`⟳`)・区切り線(`─`×26 / News は ×22)・空表示文言は現行と同一の文字列を出す(lipgloss は使わず ANSI 直書き。depguard 許可リスト変更不要)
- Dashboard: タブ順(`list-tabs`)× pending glob 順、Waiting 除外、タブ不一致は非表示、message は **60 バイト切り**、Stop=緑 ■ + "done" / 他=赤 ■
- Done: **当日・全セッション横断**(find)、restored 除外、completed_at 昇順、統計行(count/turns/calls/$cost)、時刻は `completed_at[11:16]`、markers 🚀💬📝
- キー入力: Dashboard は「無関係キーを捨てて残時間を待ち直す」方式だったが、Bubble Tea では tick が独立するため構造的に不要(挙動差は evidence に記録)。2 打鍵(d+num は 3 秒 / r+num は 3 秒)はタイムアウト付き prefix 状態でモデル化
- 既知バグは**そのまま再現**し、改善候補として evidence に記録(スペース入りタブ名が表示されない awk `$3` 相当 / Done の壊れ JSON 1 件で全滅 / バイト幅パディング)。修正は移行完了後に判断

## TODO

### domain(表示ロジックの純粋関数化)

- [x] pending 一覧の並び替え・フィルタのテストを作成(タブ順×glob 順 / Waiting 除外 / タブ不一致除外 / 壊れ JSON スキップ)→ 実装
- [x] Dashboard の行レンダリングのテストを作成(ANSI・番号・■ 色分け・60 バイト切り・空表示・フッタ)→ 実装
- [x] Waiting の抽出・レンダリングのテストを作成 → 実装
- [x] Done の集計・レンダリングのテストを作成(restored 除外 / 統計 / HH:MM / markers / 壊れ JSON 全滅の再現 / 空表示)→ 実装
- [x] News のレンダリングのテストを作成(5 件・description 省略・空 3 ケース)→ 実装
- [x] `list-tabs` 出力のパース(タブ名抽出・id 解決は「先頭 2 列除去」方式)のテストを作成 → 実装

### app / infra

- [x] port を定義(`TabController`(list-tabs / go-to-tab-name / close-tab-by-id)/ `DailyReader` / `NewsReader` / `ShellRunner`(upload-log / restore-task / fetch-news / restore-session / screen_detect_tick))+ fake
- [x] Dashboard ユースケースのテストを作成(一覧構築 / ジャンプ時の agent 別 pending クリア(claude・screen 検出は消さない)/ 削除フローの順序: record → upload(失敗で中止)→ pending 削除 → registry 削除 → screen-state 削除 → close-tab-by-id)→ 実装(record-output は既存 `RecordOutput` を使用)
- [x] Done / Waiting / News ユースケースのテストを作成 → 実装
- [x] infra: DailyReader(find 相当・全セッション横断)/ NewsReader / TabController / ShellRunner を実装

### tui(Bubble Tea v2)

- [ ] 各ペインの Model(Update/View)のテストを作成(tick 2s・5s / キーイベント / 2 打鍵タイムアウト / 削除中の進行表示)→ 実装。View は domain のレンダリング関数へ委譲
- [ ] `--once` モード(1 回描画して終了。現行の `CONDUCTOR_*_ONCE` 相当。restore-session / screen 検出の副作用条件も現行に合わせる)

### cli / 互換検証

- [ ] `mdev pane dashboard|waiting|done|news` サブコマンドを登録
- [ ] ゴールデンテスト: 現行 Shell 版の ONCE モード出力(同一 fixture: pending / daily / news / list-tabs スタブ)と `mdev pane <name> --once` の出力を比較
- [ ] 依存追加: `github.com/charmbracelet/bubbletea/v2`(depguard 許可済み)。bubbles が必要になった場合のみ追加
- [ ] ユーザーテスト手順書 `docs/user-test-02-panes.md`(インストール済み `~/.claude-conductor/layouts/multi.kdl` のペイン起動コマンド差し替え手順・バックアップ・復元・確認チェックリスト)
- [ ] `make check` 緑を確認

## 完了条件

- 同一 fixture に対する 4 ペインの出力が Shell 版 ONCE 出力と一致(ゴールデンテスト)
- 削除フローの順序契約(upload 失敗時は何も消さない)がテストで固定されている
- `make check` 緑
- ユーザーテスト 02 の手順書が完成している(実施はマージ後)

## 備考

- **ユーザーテスト契機**: このタスク完了 = Go 版ペインが実環境のレイアウトで動く 2 つ目のユーザーテストポイント(目視でしか確認できなかった赤 ■ / 自動帰還 / done 表示もここで確認できる)
- Dashboard の削除に `close-tab` フォールバックが無い等の Shell 版の非対称(task-control との差)は現行通り再現
- `upload-log.sh` / `restore-task.sh` / `fetch-news.sh` / `restore-session.sh` / `screen_detect_tick` の Shell 呼び出しは env(`ZELLIJ_SESSION_NAME` / `CONDUCTOR_HOME`)継承が必須
- `_screen_tab_slug`(screen-state 削除に必要)は `cksum`(POSIX CRC)互換実装が必要 — Go の `hash/crc32`(IEEE)とは別物なので実測で一致確認すること
