# フェーズ3 タスク作成 + task-control 移植: 検証記録(evidence)

憶測を残さないための実測ログ。着手時から随時追記する。
一次情報は claude-conductor v0.7.4 の `scripts/task-lib.sh` /
`scripts/task-create-loop.sh` / `scripts/task-control.sh` /
`scripts/waiting-toggle.sh` と `test.sh` の該当セクション。

## 1. 移植元の実測(bash / jq の癖)

いずれも実行環境(macOS 15 / bash 3.2 / jq 1.7)で実測した。

| # | 対象 | 実測 | 移植方針 |
|---|------|------|----------|
| 1 | `read -r -a arr <<< $'a\nb c'` | `${#arr[@]}` = **1**、`arr[0]="a"` | `read` は 1 行しか読まない。agent command の語分割は**最初の行だけ**を `strings.Fields` する |
| 2 | `echo '{"action":"resize"}' \| jq -r '.direction'` | `null`(文字列) | direction キーが無いステップは `zellij action move-focus null` を撃つ。Go も文字列 `"null"` を渡して再現する |
| 3 | 同上 `.command // empty` | 空文字 | command 省略時は `--` 以降を付けない分岐 |
| 4 | `.amount // 1`(missing / null / false) | いずれも `1` | 既定 1 |
| 5 | `amount=abc; for (( j=0; j<amount; j++ ))` | 反復 **0 回** | 数値に解釈できない amount は 0 回。Go も 0 回にする |
| 6 | `echo '{}' \| jq -r '.search_depth'` | `null`(文字列) | Shell は `fd --max-depth null` を撃って fd がエラー終了 → 候補ゼロ。Go は既定 1 にフォールバック(**挙動差**。§4 参照) |
| 7 | `jq '.prev_event = .event \| ...'` の出力 | 2 スペースのプリティ出力・新規キーは**末尾**に追加 | Go は compact 出力。ゴールデンは JSON 等価比較で突き合わせる(§5) |

### pending ファイルのキー集合

`pending-notify.sh` / `codex-notify.sh` / `screen-detect-lib.sh` の 3 か所が
pending を書く。キーは
`tab / session / claude_session_id / message / event / time / agent`
に加えて非空のときだけ `transcript_path / dir / task_type` が付く。
`waiting-toggle.sh` はこれに `prev_event` を足し引きする。

いずれも `domain.Pending` の全フィールドで表せるが、**waiting-toggle は
`domain.Pending` を経由せず生 JSON のキー順を保ったまま変換する**方式にした。
jq は未知のキーをそのまま持ち越すため、構造体を経由すると将来キーが増えたときに
黙って落とすことになる(データを失う方向の差異は許容しない)。

## 2. zellij の挙動(移植元コメントの追認)

`task-lib.sh` の `_focus_tab_verified` が記録している zellij 0.44.1 の実測を
そのまま前提として引き継ぐ。

- `zellij action go-to-tab-name <name>` は存在しないタブ名でも **rc=0**。
  成否の差は stdout だけで、ヒット時のみタブ index を出力する
- したがって「stdout 非空 = フォーカス成功」で判定する。既存の
  `zellij.Focuser.FocusTab` は stdout を捨てるため、`FocusTabVerified` を新設した
- `zellij action new-tab` の rc=0 は「サーバが受理した」であって
  「タブが登録された」ではない。`query-tab-names` に名前が現れるまで待つ

## 3. create_task の防御シーケンス(Shell v0.7.4 と同一)

```
screen-state 削除
  → new-tab            (失敗 → その rc をそのまま返す。ペインは 1 枚も作らない)
  → query-tab-names ポーリング (10s 予算。失敗 → rc=3、ペイン 0 枚)
  → go-to-tab-name ポーリング  (10s 予算。stdout 空のうちは再試行。失敗 → rc=3、ペイン 0 枚)
  → new-pane down (task-control) (予算切れでも最低 1 秒は試す)
  → resize decrease up × 30      (予算切れで打ち切り、rc=0)
  → focus-previous-pane          (予算切れで打ち切り、rc=0)
  → apply_layout(残り予算)       (rc は無視)
```

最優先の不変条件は **「登録待ちかフォーカス検証に失敗したらペインを 1 枚も
作らない」**。フォーカスが Main に残ったまま `new-pane` を撃つと Main タブを
割ってしまうため。test.sh 17b8 の 4 ケース(遅延登録・フォーカス再試行・
永久未登録・永久未フォーカス)がこの契約を固定している。

## 4. 意図的な挙動差(改善方向)

| # | 箇所 | Shell | Go | 理由 |
|---|------|-------|----|------|
| 1 | ディレクトリ探索 | `fd`(外部依存) | 内製 WalkDir + ドット始まり除外 | 外部依存の削減。深さ 1 運用では .gitignore 解釈が結果に効かない |
| 2 | 探索結果の並び | fd は並列走査で不定 | ルート順 → パスの昇順で安定 | 選択 UI の並びが実行ごとに変わらない |
| 3 | 選択 UI | `fzf`(外部依存 + alt-screen) | 自前の部分列フィルタ | depguard の許可リストに fuzzy ライブラリが無い。`FZF_DEFAULT_OPTS` は効かなくなる |
| 4 | `search_depth` 欠落 | `fd --max-depth null` が失敗 → 候補ゼロ | 既定 1 | 設定の書き忘れで機能ごと沈黙するより既定で動くほうが良い |
| 5 | タスク名入力 | bash 3.2 では `[候補]` 提示のみ(編集不可) | 常にプリフィル編集可 | bash 4 経路(`read -e -i`)の体験に統一 |
| 6 | create_task 失敗時 | 無言(戻り値を捨てている) | エラーを 2 秒表示 | 失敗が利用者に見えない状態を残さない |
| 7 | task-control の `dd` | `.screen-state` を消さない | 消す | Dashboard の削除経路(`CommitDelete`)と統合した副産物。**Shell 側の欠落バグの修正**。消さないと同名タブを作り直したとき前タスクの状態を引き継ぐ |
| 8 | Ctrl-C | 受け付けない | ペインを終了する | Bubble Tea が raw モードにするため素通りしない |
| 9 | 進行表示 | `\r` で最終行を上書き | 本体の下に 1 行足す | 差分描画のため。`--once` は通らないのでゴールデンに影響しない |

## 5. ゴールデン

- **task-control バー**: `CONDUCTOR_TASKCTL_ONCE=1` の Shell 出力と
  `mdev pane task-control <tab> --once` をバイト列で比較(通常 / WAITING)
- **waiting-toggle**: 同じ pending 入力に Shell 版と Go 版をそれぞれ当て、
  出力ファイルを **JSON 等価**で比較する。jq はプリティ出力(2 スペース)、
  Go は compact なのでバイト比較はできない。等価比較でキーと値の一致を見る
- 既存 39 ケース(`cmd/mdev/testdata/golden-panes`)は不変
