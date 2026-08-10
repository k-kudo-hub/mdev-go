# Dashboard の zellij CLI 負荷削減: 検証記録(evidence)

タブ遷移バグ対策 2/2(負荷源の削減)の記録。数字の出どころと、修正が
どこまで効いてどこから効かないのかを憶測なしで残す。

## 1. 前提として確定している事実

いずれも本 PR より前の調査で実測済み(`.claude/todo/20260810-fix-dashboard-cli-load.md`)。

| 事実 | 値 |
|------|----|
| `screen_detect_tick` 内部の `zellij action list-panes -t -c -j` の実行時間 | 1.1〜1.5 秒 |
| Dashboard の 1 回の Refresh が呼ぶ zellij CLI | `screen_detect_tick`(= list-panes)+ `list-tabs` |
| Dashboard のポーリング間隔 | 2 秒(`tui.DashboardInterval`) |
| 修正前の Go 版 Dashboard の CLI 占有率 | 60〜73% |
| 並行 CLI クライアントによる zellij サーバの劣化 | 人工負荷で独立再現済み |

以下では 1 回の Refresh が CLI を占有する時間を T、ポーリング間隔を S と書く。
確定事実から T ≒ 1.3 秒(1.1〜1.5 秒の中央)、S = 2 秒。

**本 PR で新たに実測した値は無い**。zellij が動く実セッションでの計測は
worktree からは行っていないため、以下はすべて上の実測値に基づく算術である。

## 2. 占有率の理屈

### 2.1 現行 Shell 版(逐次実行)

`dashboard-loop.sh` は「処理 → `sleep 2`」の順で回る。CLI が走っているのは
処理中の T の間だけで、1 周期は T + S になる。

```
占有率 = T / (T + S) = 1.3 / (1.3 + 2) = 0.394 ≒ 39%
```

TODO が言う「約 40%」はこれである。処理が遅れた回はそのぶん周期が伸びるため、
占有率は 40% を超えない(自己抑制)。

### 2.2 修正前の Go 版(固定間隔・ガード無し)

`tickMsg` は前回の完了と無関係に S ごとに発火し、そのたびに Refresh を発行する。
1 周期は T ではなく S で決まる。

```
占有率 = T / S = 1.3 / 2 = 0.65 = 65%
```

実測の 60〜73% は T = 1.2〜1.46 秒に対応し、この式と整合する。

さらに重要なのは T ≥ S に劣化したときの挙動である。ガードが無いと平均の
同時実行数が T / S となり、T が S を超えた瞬間から CLI クライアントが
複数本に増える。並行クライアントはサーバを劣化させて T をさらに伸ばすため、
「遅い → 重なる → もっと遅い」の正のフィードバックが閉じる。これが
`new-tab` の暗黙フォーカス切替が遅延・喪失した状況である。

### 2.3 修正後(完了起点のペーシング + in-flight ガード)

次の合図を張るのは「ポーリングで出した読み直しが着弾したとき」だけにした。
1 周期は Shell 版と同じ T + S になる。

```
周期 = T + S
占有率 = T / (T + S)
```

| 状況 | T | 修正前 | 修正後の周期 | 修正後の占有率 |
|------|---|--------|--------------|----------------|
| 平常時 | 1.3 秒 | 65%(平均 0.65 本) | 3.3 秒 | **39%** |
| 軽い劣化 | 3 秒 | 平均 1.5 本が並行 | 5 秒 | 60% |
| 重い劣化 | 5 秒 | 平均 2.5 本が並行 | 7 秒 | 71% |

修正前は T が S を超えると平均 T/S 本の CLI クライアントが並行し、サーバを
劣化させて T をさらに伸ばす正のフィードバックが閉じていた。修正後は同時に走る
ポーリングの読み直しが常に 1 本で、遅い回はそのぶん周期が自動で伸びる。
**現行 Shell 版の「処理 → sleep 2」と等価な自己抑制**である。

in-flight ガード(`refreshGate`)の役割はこの形では変わる。ポーリングの読み直しが
走っている間はタイマーが存在しないため tick 自体が来ない。ガードが効くのは
「キー操作で出した読み直しが走っている最中にタイマーが発火した」場合で、
そこで読み直しを重ねないための備えである。

### 2.4 修正後(スクリーン検出の条件化)

codex(screen 方式)を設定していない環境では、Refresh から list-panes が
丸ごと消え、残るのは `list-tabs` 1 回だけになる。T を支配していたのが
list-panes(1.1〜1.5 秒)である以上、この環境の占有率は大きく下がる。

ただし **`list-tabs` 単体の実行時間は本 PR では実測していない**ため、
「何 % になる」とは書かない。確実に言えるのは「1.1〜1.5 秒の呼び出しが
1 回ぶん丸ごと無くなる」ことだけである。

## 3. 修正 1: 完了起点のペーシングと in-flight ガード(4 ペイン共通)

`internal/tui/pane.go` に `refreshGate` と `rearmCmd` を置き、Dashboard /
Waiting / Done / News が同じ仕組みを共有する。

### 不変条件: ポーリングのチェーンはちょうど 1 本

「未着弾のポーリング読み直し」と「予約済みのタイマー」のどちらか一方だけが
常に存在し、着弾と発火で互いに入れ替わる。

```
Init ─→ [読み直し] ─着弾→ [タイマー] ─発火→ [読み直し] ─着弾→ …
```

`*RefreshedMsg` に `poll`(ポーリング起源かどうか)を持たせ、真のときだけ
着弾で次の合図を張る。キー操作で出した読み直しはチェーンの一部ではないので
何も張らない。張ると押した回数ぶんチェーンが増え、bash と zellij のプロセス
生成がその本数ぶん多重に走る。

| 経路 | 扱い |
|------|------|
| `Init` | 最初の読み直しだけを返す(`poll = true`)。ここでタイマーも張るとチェーンが 2 本になる |
| `tickMsg`(通常) | `take()` して読み直しを発行する(`poll = true`)。**次の合図は張らない**(着弾側へ移る) |
| `tickMsg`(in-flight でスキップ) | 読み直しを発行せず `tickCmd` だけを張る。チェーンはタイマーとして 1 本のまま |
| `tickMsg`(凍結中: `busy` / `awaiting` / `fetching`) | 同上。加えて印を立てない。立てると凍結が解けた後に二度と読み直せなくなる |
| `*RefreshedMsg` | ハンドラの**先頭**で `release()`。`poll` が真なら次の合図を張る。エラーでも、待ち受け中で内容を捨てる場合も張る(絶やすと二度と回らない) |
| キー操作・後始末(ジャンプ / restore / reload / 削除の完了 / 通知の期限切れ / 待ち受けの時間切れ) | `force()` して発行(`poll = false`)。利用者の操作への反応は前回の完了を待たずに出す。着弾しても合図は張らない |

### 起動直後の 1 回をどう数えるか

`Init()` は値レシーバでモデルを書き換えられないため、Init の中で印を立てられない。
`New*Model` が `refreshGate{inFlight: true}` で始めることで、Init が必ず出す
1 回目の Refresh もガードの内側に入る。

### テストでの固定方法

`tickMsg` は非公開型なので内部テスト(`internal/tui/tick_internal_test.go`)から
流し込む。判別には `immediate` ヘルパを使う。読み直しのコマンドと `tea.Batch` は
その場でメッセージを返すのに対し、タイマー(`tea.Tick`)だけなら間隔ぶん
待たないと返らない。この差で「予約されたのが読み直しか、タイマーか、
その両方(= チェーンが 2 本)か」を実時間を待たずに見分けている。

| テスト | 固定している取り決め |
|--------|----------------------|
| `TestPaneInitStartsChainWithRefreshOnly`(4 ペイン) | Init が張るのは最初の読み直しだけで、タイマーを束ねていないこと(束ねるとチェーンが 2 本になる) |
| `TestPaneChainAlternatesRefreshAndTimer`(4 ペイン) | **不変条件そのもの**。着弾 → タイマーだけ / タイマー発火 → 読み直しだけ、を 2 周ぶん回して、毎回ちょうど 1 本だけが次へ渡ることを見る |
| `TestPaneForcedRefreshDoesNotRearmPolling`(4 ペイン) | `poll = false` の着弾は何度来てもコマンドを返さないこと(押した回数ぶん増殖させない) |
| `TestPaneTickSkipsRefreshWhileInFlight`(4 ペイン) | 実行中の読み直しに重ねず、次の合図だけを予約すること。着弾後の tick は通常どおり発行すること(印の下ろし忘れが無い) |
| `TestPaneTickSkipsRefreshWhileForcedRefreshInFlight`(Dashboard / Done / News) | ガードが実際に効く経路。キー操作の読み直しが走っている最中にタイマーが発火 → 重ねず予約し直す → その着弾は合図を張らない → 次の発火では発行する、の 4 段階 |
| `TestPaneRefreshErrorReleasesInFlightAndRearms`(Dashboard / Waiting) | **エラーで返った着弾でも印が下り、かつ次の合図が張られる**こと。どちらを忘れても 1 回の失敗でポーリングが永久に止まる |
| `TestDashboardTickIsFrozenWhileAwaiting` / `...WhileBusy` / `TestDoneTickIsFrozenWhileAwaiting` / `TestNewsTickIsFrozenWhileFetching` | 凍結中の tick が読み直しを発行しないこと(既存の取り決め)に加えて、**凍結中の tick が印を立てない**こと。凍結を解いた直後の tick が発行できることで確かめる |
| `TestDashboardDroppedRefreshReleasesInFlight` | 待ち受け中に着弾して内容を捨てた読み直しでも印が下りること |
| `TestPaneKeyDrivenRefreshDoesNotReschedulePolling`(外部テスト。Dashboard / Done / News) | 実際のキー操作(ジャンプ / restore / reload)で出た着弾が合図を張らないこと。Waiting は終了キー以外を受け付けないためこの経路を持たない |

「立て忘れ」は各テストの最後で「発行できるはずの tick が発行しているか」、
「下ろし忘れ」は「発行してはいけない tick が発行していないか」の両方向から
見ている。チェーンの本数は「読み直しとタイマーのどちらか一方だけが返るか」で
見ている。

## 4. 修正 2: スクリーン検出の条件化

- `domain.Config.HasScreenDetectionAgent()` を追加。`agents` のどれかの
  `detection` が `"screen"` かを返す。判定は既存の `AgentDetection` を通すため、
  未設定・空文字・null はすべて hooks 扱いで false になる(現行 `task-lib.sh` の
  既定と同じ)。
- `DashboardPane.Refresh` はこの判定が真のときだけ `ScreenDetectTick` を呼ぶ。
  設定を見るだけの静的判定で、pending の中身やタブの状態は見ない。
- codex を設定しているユーザーの挙動は変わらない(従来どおり毎回呼ぶ)。

依存方向は変えていない。判定は domain の純粋関数で、app は既存の
`ConfigLoader` port(`PaneStore.Load`)から設定を得ているだけである(ADR-0002)。

## 5. 検証結果

```
$ go test ./cmd/mdev/ -run Golden
ok  	github.com/k-kudo-hub/mdev-go/cmd/mdev	0.800s
```

ゴールデン 23 ケース(dashboard 7 / waiting 4 / done 6 / news 6)は全通過。
`--once` は `Once()` を通り tick を回さないため、ペーシングの変更もガードも
影響しない。スクリーン検出の条件化も、ゴールデンの入力に `config.json` を置く
ケースが無く(= agents が空 → 検出を呼ばない)、かつ元々サンドボックスに
スクリプトが無いため副作用が無い。よって出力は 1 バイトも変わらない。

```
$ make check
gofmt: no diff
golangci-lint: 0 issues.
go-arch-lint: OK - No warnings found
go test -race: 全パッケージ ok
Total test coverage: 90.2% (1608/1782)  # domain 93.5% / app 94.4%(閾値 90%)
go build -o bin/mdev ./cmd/mdev
```

## 6. 挙動差と残る未検証事項

1. **再描画の間隔が「間隔ちょうど」から「読み直しの時間 + 間隔」に変わる。**
   平常時の Dashboard で 2 秒周期 → 約 3.3 秒周期になる。これは現行 Shell 版と
   同じ周期であり、移植の観点ではむしろ現行に近づく。負荷と引き換えに
   表示の更新はそのぶん遅くなる。
2. **`list-tabs` 単体の実行時間を実測していない。** 修正 2(スクリーン検出の
   条件化)の効果を数値で言い切れないのはこのためである。
3. 修正後の実セッションでの再テスト(タブ遷移が安定するか)は利用者に依頼する。
   本 PR は負荷源の削減のみで、根本対策 1/2(claude-conductor 側の `create_task`
   明示フォーカス)は別 PR である。
