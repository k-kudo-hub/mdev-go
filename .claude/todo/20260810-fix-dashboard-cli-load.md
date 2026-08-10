# Dashboard の zellij CLI 負荷を削減する(タブ遷移バグ対策 2/2)

## 概要

タブ遷移バグの調査で、Go 版 Dashboard の CLI 占有率(60〜73%)が zellij サーバを劣化させ、`new-tab` の暗黙フォーカス切替が遅延・喪失することが判明した。
Shell 版と同等の自己抑制(占有率約 40%)に戻すため、tick に in-flight ガードを入れ、不要な screen 検出呼び出しを省く。

## 調査で確定した事実

- Dashboard の Refresh は毎回 `screen_detect_tick`(内部の `zellij action list-panes -t -c -j` が実測 1.1〜1.5 秒)+ `list-tabs` を実行する
- 現行の tickMsg ハンドラ(internal/tui/dashboard.go)は前回 Refresh の完了を待たずに 2 秒ごとに発火する(in-flight ガード無し)。Shell 版は「処理 → sleep 2」の逐次実行で、遅延時に周期が自動で伸びる
- 並行 CLI クライアントによるサーバ劣化は人工負荷で独立再現済み

## TODO

- [x] tui: in-flight ガードのテストを作成(Refresh 未完了中の tick は refresh を発行せず次の tick だけ予約 / 完了後の tick は通常どおり発行 / busy・awaiting ガードとの共存)→ 4 ペイン共通の仕組みとして実装
- [x] app: config に `detection == "screen"` のエージェントが 1 つも無い場合は `ScreenDetectTick` を呼ばないテストを作成 → 実装(codex 未設定環境でのコスト削減。設定の静的判定のみで、codex 設定があるユーザーでは従来どおり毎 tick 呼ぶ)
- [x] ゴールデン 23 ケースが不変であることを確認(表示・`--once` に影響しないこと)
- [x] evidence(`.claude/todo/20260810-cli-load-evidence.md`)に占有率の理屈(refresh 約 1.3 秒 + 間隔 2 秒 → 逐次化で約 40%)と修正の効果を記録
- [x] `make check` 緑

## 完了条件

- in-flight 中の tick が refresh を発行しないことがテストで固定されている
- ゴールデン全通過・`make check` 緑

## 備考

- 根本対策 1/2(claude-conductor 側の `create_task` 明示フォーカス)は別 PR。本 PR は負荷源の削減のみ
- 修正後のユーザー再テストで、実セッションでの遷移が安定することを確認する
