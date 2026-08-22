# ペインを alt screen で描画してリサイズ時の画面崩れを解消する

## 概要

全ペインが Bubble Tea のインラインモード(通常スクリーンバッファ)で動いているため、古い描画フレームが zellij のスクロールバックへ蓄積し、ペインのリサイズ時に zellij がそれを新しい幅で折り返し直して画面上へ再出現させる(スクリーンショットの `SCROLL: 0/68`、重複ヘッダー、行の断片)。全モデルの `View()` が返す `tea.View` に `AltScreen = true` を設定し、スクロールバックを持たない全画面バッファで描画することで、リサイズ時も画面をクリーンに保つ。

## 調査で判明した事実

- `internal/tui/pane.go:449` の `tea.NewProgram(model).Run()` はオプションなしで、リポジトリ全体に `AltScreen` の記述が 1 箇所も無い
- `charm.land/bubbletea/v2 v2.0.8` では alt screen は Program オプションではなく `tea.View` のフィールド(`AltScreen bool`)で毎フレーム指定する
- `View()` を持つモデルは 6 つ: `dashboard.go` / `waiting.go` / `done.go` / `news.go` / `taskcreate.go` / `taskcontrol.go`
- `Once()`(ゴールデンテストの経路)は `View()` を通らないため、ゴールデン出力への影響は無い

## TODO

- [x] 全 6 モデルの `View()` が `AltScreen = true` の `tea.View` を返すことを検証するテストを作成(失敗を確認)
- [ ] `paneView` ヘルパー(`tea.NewView` + `AltScreen = true`)を実装し、全 `View()` の `tea.NewView` 呼び出しを置き換えてテストを通す
- [ ] `make check`(fmt-check / lint / arch / cover / build)を実行して修正

## 完了条件

- 全 6 モデルの `View()` が `AltScreen = true` を返し、それを検証するテストがパスする
- `make check` がパスする
- `make install` したバイナリで zellij ペインをリサイズしても、古いフレームの残骸(重複ヘッダー・行の断片)が画面に残らない
- ペイン枠に zellij の `SCROLL:` 表示が出ない(スクロールバックへ書き込まれない)

## 備考

- alt screen によりペイン内容を zellij のスクロールバックで遡れなくなるが、対象は自動更新ペインであり、その履歴が今回の崩れの原因そのものなので失うものは無い
- alt screen の復帰(終了時に元のバッファへ戻す)は Bubble Tea が自動で行う
