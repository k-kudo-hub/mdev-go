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

## 6. 実装中に確認したこと

### 6-1. create_task の呼び出し列(Shell 実測)

モック zellij(`query-tab-names` は Main と対象タブを返し、
`go-to-tab-name` は index を返す)を PATH の先頭に置き、
`create_task /tmp/proj dev my-task "" codex` を走らせた結果:

```
1: action new-tab -n my-task --cwd /tmp/proj -- env TASK_TAB_NAME=my-task TASK_TYPE=dev TASK_AGENT=codex codex
2: action query-tab-names
3: action go-to-tab-name my-task
4: action new-pane --direction down --cwd /tmp/proj -- bash <CONDUCTOR_HOME>/scripts/task-control.sh my-task
5-34: action resize decrease up            (30 回)
35: action focus-previous-pane
36: action new-pane --direction right --cwd /tmp/proj -- nvim
37: action new-pane --direction down --cwd /tmp/proj -- lazygit
38: action move-focus left
```

Go 版(`TestCreateTaskSequence` / `TestApplyLayoutDev`)はこの並びを
そのまま固定している。**唯一違うのは 4 行目**で、Go 版は
`<CONDUCTOR_HOME>/bin/mdev pane task-control my-task` を起動する。

### 6-2. task-control バーのバイト一致(実バイナリで確認)

隔離した HOME / CONDUCTOR_HOME で、空白を含むタブ名 `my task` を与えて
実バイナリと Shell を比較した。

```
$ diff <($MDEV pane task-control "my task" --once) \
       <(CONDUCTOR_TASKCTL_ONCE=1 bash .../task-control.sh "my task")
(差分なし)
```

通常表示・WAITING 表示とも `od -c` でバイト一致。ゴールデンにも
4 ケース(通常 / WAITING / Notification / 別タブの Waiting)を追加した。

### 6-3. タスク作成メニューのバイト一致

`domain.RenderTaskCreateMenu` は `task-create-loop.sh` の main_loop 冒頭と
バイト一致する(`od -c` で確認済み)。区切り線は 26 本で
Dashboard / Waiting / Done と同じ。

### 6-4. Bubble Tea v2 のキー名

`tea.KeyPressMsg.String()` の実測値(v2.0.8)。選択 UI の分岐がこの
文字列に依存するため記録する。

| 構成 | String() |
|------|----------|
| `{Code: 'c', Mod: tea.ModCtrl}` | `ctrl+c` |
| `{Code: tea.KeyEscape}` | `esc` |
| `{Code: tea.KeyEnter}` | `enter` |
| `{Code: tea.KeyBackspace}` | `backspace` |
| `{Code: tea.KeyUp}` / `{Code: tea.KeyDown}` | `up` / `down` |
| `{Code: 'p', Mod: tea.ModCtrl}` | `ctrl+p` |
| `{Code: 'n', Text: "n"}` | `n` |

修飾キー付きは必ず 2 文字以上の名前になるので、
「1 ルーンなら絞り込みの入力」という判定でそのまま弾ける。

## 7. TODO からの設計変更

### 7-1. TabActor は 1 回ごとの上限を引数で受ける

TODO は「全て proc.Command + 10 秒上限」と書いていたが、固定 10 秒だと
全体予算を超えたところで撃った 1 本が最大 10 秒はみ出す(予算 30 秒に対して
最悪 40 秒)。Shell の `_zj_budget_cap` は「残り予算」と「1 回の上限」の
小さいほうを渡してこれを防いでいる。

そこで `app.TabActor` の各メソッドは先頭に `timeout time.Duration` を取る形に
した。10 秒(`commandTimeout`)は**上限の頭打ち**として実装側に残っている。
`TestTabControllerClampsTheCap` がこれを固定している。

### 7-2. 削除フローは TaskDeleter として抽出した

TODO は「既存の PrepareDelete / CommitDelete に統合」と書いていた。
`DashboardPane` のメソッドをそのまま task-control から呼ぶと、Dashboard 用の
port(PendingLister / ConfigLoader / Focuser の 3 つは task-control の削除に
要らない)まで組み立てる必要が
あり、`close-tab` フォールバックの有無も分けられない。

削除フローだけを `app.TaskDeleter` に切り出し、`DashboardPane` は
そこへ委譲する形にした。外から見たメソッド(`PrepareDelete` /
`CommitDelete`)は変えていないので、tui と既存テストは無変更である。

`CloseActiveOnMissingID` が 2 経路の非対称を表す。

- Dashboard: false。Main タブの中で動いているため、`close-tab` へ落ちると
  Main を閉じてしまう
- task-control: true。自分のタブの中で動いているので「今のタブ」でよい

### 7-3. DirLister に IsDir を足した

現行版は「起点が 1 つも実在しない」と「起点はあるが配下が空」を区別して
いる(前者だけが赤字 + 2 秒、後者は黙ってメニューへ戻る)。列挙結果だけでは
区別できないため、`[[ -d ]]` に対応する `IsDir` を port に足した。

## 8. TODO の項目とコミットの対応

1 項目 = 1 コミットを原則にしたが、次の 3 か所はまとめた。分けると
コンパイルが通らない、または片方だけでは意味を成さないためである。

| コミット | まとめた TODO 項目 | 理由 |
|---|---|---|
| 名前の既定値 + 部分列フィルタ | 2 項目 | 同じ n フローの純粋関数で、単独では使い道が無い |
| task-control のユースケース | ToggleWaiting + 削除ユースケース統合 | TaskDeleter の抽出を先に入れないと TaskControlPane を組み立てられない |
| tui + cli | Model 2 つ + --once + コマンド登録 + ゴールデン | Panes への登録と CLI の引数追加は分けるとコンパイルが通らない |

また `ApplyLayout` は TODO の順(CreateTask が先)とは逆に先へ入れた。
CreateTask が呼ぶ側であるため。

## 9. TDD の自己評価

- **赤を確認できた**: Config 拡張 / 名前解決 / 部分列フィルタ /
  waiting-toggle の遷移 / task-control バー / TabController 拡張 /
  ApplyLayout / PendingStore の生読み書き / cli の引数
- **赤が「コンパイル失敗」止まりだった**: `CreateTask.Execute`。
  ApplyLayout と同じファイルに書いたため、テストを足した時点で実装が
  既に存在した。代わりに**変異テスト**で契約が効いていることを確かめた
  (下記)
- **後追いになった**: tui の 2 モデル。実装を書いてからテストを足した

### CreateTask の変異テスト

実装を 3 通りに壊し、いずれもテストが落ちることを確認した。

| 変異 | 落ちたテスト |
|---|---|
| フォーカス検証の失敗を無視して先へ進む | 4 件(ペインを 1 枚も作らない契約) |
| screen-state の削除を new-tab の後ろへ移す | 3 件(順序と失敗時の中止) |
| resize ループの予算チェックを外す | 1 件(30 回回りきり、経過が予算を超える) |

## 10. コードレビュー指摘への対応

`/code-review` の CONFIRMED 7 件と cleanup 2 件。指摘 9 はコーディネータ検証で
却下されたため現状維持とした。

| # | 指摘 | 対応 | 実証 |
|---|------|------|------|
| 1 | 予算枯渇が無制限へ反転 | Execute は残り予算が 0 以下ならレイアウトへ入らない。残りは 1 ステップ 1 回だけ測る | 上限を超える呼び出しを再現する fake(overrunOn)で修正前の失敗を確認 |
| 2 | `-` 始まりのタブ名 | 起動コマンドと CLI の両方に `--` 区切り | `-wip` で起動・表示できることをテストと実バイナリで確認 |
| 3 | スペースの無音破棄 | typedText が "space" を個別に受ける | 特別扱いを外すと落ちることを確認 |
| 4 | `"true"` が効かない | `jq -r` 出力との比較に合わせ両対応 | jq 実測(§10-1)。誤ったコメントとテストの期待値も訂正 |
| 5 | 全滅パース | agents / task_types を per-entry で読み飛ばす | 壊れた 1 件があっても codex の detection=screen が生きることを固定 |
| 6 | 余剰引数の黙殺 | ペインごとに引数の数を厳密に見る | 3 パターンをテスト |
| 7 | 選択ポリシーの重複 | FindByTab を FindRawByTab の上に載せ、TaskControlPane の port を 1 つに | 同じ入力で同じ 1 件を指すことを固定 |
| 8 | refilter の二重走査 | FilterCandidateIndexes で単一パス化 | 重複名でカーソルどおり選べることを固定 |
| 9 | deleter の二重配線 | **却下**(コーディネータ検証で前提の一部が誤り・実害僅少)。現状維持 | — |

### 10-1. jq の実測(指摘 4 の根拠)

`[[ "$(jq -r '.skip_task_name_input // false' ...)" == "true" ]]` の結果:

| 設定値 | `jq -r` の出力 | 有効か |
|--------|----------------|--------|
| `true` | `true` | 有効 |
| `"true"` | `true` | **有効** |
| `false` / `"false"` / `null` | `false` | 無効 |
| `1` | `1` | 無効 |

`jq -r` は文字列を引用符なしで出すため、真偽値 true と文字列 "true" が
同じ出力になる。当初「文字列は別物」と書いていたのは誤りだった。

### 10-2. jq の実測(指摘 5 の根拠)

`{"agents":{"claude":{...},"broken":5,"codex":{...}}}` に対して:

- `jq -r '.agents // {} | keys_unsorted[]'` → `claude broken codex`(値を見ない)
- `jq -r --arg a codex '.agents[$a].command // empty'` → `codex`(1 件だけ読む)

つまり現行版は 1 件壊れても他のエージェントを失わない。Go 版が map 全体を
一度に読んで空へ落ちていたのは、この経路との差異である。

`task_types` の一覧表示(`to_entries[] | "\(.key)  \(.value.description)"`)は
オブジェクトでない値に当たるとその手前までしか出さない(実測)。レイアウト
適用は型ごとに読むため無事である。Go 版は per-entry で読み飛ばす形に揃えた
(1 つの設定ミスで選択肢が全部消えるより、その 1 つだけが説明なしで並ぶ
ほうが直しやすい)。

### 10-3. 却下した指摘の要旨

コーディネータ検証で除外されたもの。判断の根拠を残す。

1. **screen-state の削除失敗で fail-fast するのは過剰では** — 意図的である。
   消せないまま作ると古い "working" が新しいタブの最初のポーリングで即 Stop に
   見え、古い "blocked" は Main への不要なジャンプを起こす。タブを作る前に
   止めるほうが安全である
2. **Config を毎回読み直している** — TaskCreatePane の各メソッドが
   `Config.Load()` を呼ぶ。設定ファイルは小さく、呼ばれるのはキー操作の
   たびに 1 回である。キャッシュを持つと、設定を書き換えたあとペインを
   再起動するまで反映されない別の分かりにくさが生まれる
3. **close-active フォールバックの非対称** — Dashboard は落ちず task-control は
   落ちる、という差は現行版のもので、`TaskDeleter.CloseActiveOnMissingID` の
   コメントと evidence §7-2 に理由ごと文書化済みである
