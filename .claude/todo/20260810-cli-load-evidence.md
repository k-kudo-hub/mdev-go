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

判断は 4 ペイン共通の `poller` 型(`internal/tui/pane.go`)に集約してある。
状態を変える入口は 3 つだけで、とくに `arrive` は「実行中の数を減らす」と
「ポーリング起源なら次の合図を張る」を 1 つの操作にまとめてあり、片方だけを
行えない(指摘 8 への対応)。

| 経路 | 扱い |
|------|------|
| `Init` | 最初の読み直しだけを返す(`poll = true`)。ここでタイマーも張るとチェーンが 2 本になる |
| `tickMsg`(通常) | `polling.tick` が読み直しを発行する(`poll = true`)。**次の合図は張らない**(着弾側へ移る) |
| `tickMsg`(実行中でスキップ) | 同じく `polling.tick` が、読み直しを出さず次の合図だけを予約する。チェーンはタイマーとして 1 本のまま |
| `tickMsg`(凍結中: `busy` / `awaiting` / `fetching`) | `polling.rearm`。実行中の数を変えない(変えると凍結が解けた後に二度と読み直せなくなる) |
| `*RefreshedMsg` | ハンドラの**先頭**で `polling.arrive(msg.poll)`。エラーでも、内容を捨てる場合も必ず通す |
| キー操作・後始末(ジャンプ / restore / reload / 削除の完了 / 通知の期限切れ / 待ち受けの時間切れ) | `polling.force` して発行(`poll = false`)。利用者の操作への反応は前回の完了を待たずに出す。着弾しても合図は張らない |

### 実行中の「数」を持つ理由

真偽値ではなく本数(`inFlight int`)で数える。キー操作の読み直しとポーリングの
読み直しは同時に走りうるため、真偽値だと先に着弾したほうが印を下ろしてしまい、
まだ走っているほうへ次のポーリングが重なる(指摘 3。2 章の「同時に走る読み直しは
常に 1 本」という前提が崩れる)。

### 起動直後の 1 回をどう数えるか

`Init()` は値レシーバでモデルを書き換えられないため、Init の中で数えられない。
`newPoller` が `inFlight: 1` で始めることで、Init が必ず出す 1 回目の読み直しも
数の内側に入る。

### モデルを書き換える呼び出しは別の文へ分ける

`return m, m.polling.tick(...)` と書くと、返り値の `m` を評価する時点と、`m` を
書き換える呼び出しの時点の順序が Go の仕様で決まっていない(指摘 4)。
`cmd := ...` と別の文に分けてから return する。規則は `poller` の doc コメントに
書いてある。

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
| `TestPaneTickWaitsForForcedRefreshAfterPollArrives`(Dashboard / Done / News) | **交錯ケース**(指摘 3)。キー操作とポーリングの読み直しが同時に走り、**ポーリングのほうが先に着弾**しても、キー操作のほうが着弾するまで次の読み直しを出さないこと |
| `TestDashboardInitProceedsToRefreshAfterStartup` | 起動時の復元が上限で切られても、最初の読み直しへ進みチェーンが張られること |
| `TestDashboardIgnoresPromptTimeoutAfterNumberKey` / `TestDoneIgnoresPromptTimeoutAfterNumberKey` | 2 打鍵目を受け取った後に古い打ち切りタイマーが発火しても無視すること(指摘 6) |
| `TestNewsKeepsFetchingScreenWhenPollArrives` | 取得中に着弾したポーリングの読み直しが、取得中の画面を差し替えないこと。その間 r を押しても取得が二重に走らないこと(指摘 7) |
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
- `DashboardPane.Refresh` は「設定を読めて、かつこの判定が偽」のときだけ
  `ScreenDetectTick` を省く。設定を見るだけの静的判定で、pending の中身やタブの
  状態は見ない。
- 設定が読めなかったとき(ファイルが 1 つも無い・JSON が壊れている)は従来
  どおり呼ぶ。`ConfigLoader.Load` は `(domain.Config, bool)` を返し、「エージェントが
  1 つも無い」と「読めなかった」を区別する(指摘 5 への対応。7 章)。
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
影響しない。スクリーン検出の条件化も、ゴールデンのサンドボックスには設定
ファイルが無く(= 読めない → fail-open で従来どおり呼ぶ)、かつスクリプトも
無いため副作用が無い。よって出力は 1 バイトも変わらない。

```
$ make check
gofmt: no diff
golangci-lint: 0 issues.
go-arch-lint: OK - No warnings found
go test -race: 全パッケージ ok
Total test coverage: 90.3% (1632/1807)  # domain 93.5% / app 94.4%(閾値 90%)
go build -o bin/mdev ./cmd/mdev
```

## 6. コードレビュー指摘への対応

| # | 指摘 | 対処 | 固定したテスト |
|---|------|------|----------------|
| 1 | スクリーン検出が返らないとポーリングが永久に止まる | `ScreenDetectTick` に 15 秒の上限 | `TestRunnerAppliesTimeoutPerScript` / `TestRunCommandCutsOffAtTimeout` |
| 2 | zellij CLI と起動時の復元が返らない場合も同じ | list-tabs / close-tab-by-id / go-to-tab-name に 10 秒、`RestoreSession` に 60 秒 | 同上 + `TestCommandOutputCutsOffAtTimeout` / `TestDashboardInitProceedsToRefreshAfterStartup` |
| 3 | 真偽値の in-flight が desync する(先に着弾したほうが印を下ろす) | 本数で数える(`poller.inFlight int`) | `TestPaneTickWaitsForForcedRefreshAfterPollArrives` |
| 4 | `return m, m.forceRefreshCmd()` の評価順ハザード | 該当 7 か所を `cmd := ...` と別文に分離。規則を `poller` の doc に明記 | (既存の全テストが新しい書き方の上で通る) |
| 5 | 設定を読めなかっただけで検出が止まる | `Load` が `ok` を返し、読めなかったときは検出を走らせる(fail-open) | `TestDashboardPaneRefreshRunsScreenDetectionWhenConfigIsUnreadable` ほか |
| 6 | 削除処理中に古い `promptExpiredMsg` が効く | 2 打鍵目で世代(token)を進める | `TestDashboardIgnoresPromptTimeoutAfterNumberKey` / `TestDoneIgnoresPromptTimeoutAfterNumberKey` |
| 7 | 取得中にポーリングの着弾が来ると取得中の画面が消え、r が二重に効く | 取得中のポーリング着弾では表示を差し替えない | `TestNewsKeepsFetchingScreenWhenPollArrives` |
| 8 | 解放・再アームの判断が 4 ペインに手書きで複製されている | `poller` 型へ集約(`arrive` は解放と再アームを分離不能に) | 4 ペイン共通のテーブルテスト群 |

### 交錯ケース(指摘 3)をどう固定したか

`TestPaneTickWaitsForForcedRefreshAfterPollArrives` は、実際の順序どおりに
メッセージを流して次の 6 段階を見る。

1. 起動時の読み直しが着弾 → タイマーが張られる
2. タイマーの発火 → ポーリングの読み直し(1 本目)が走り出す
3. キー操作(待ち受けの時間切れ / reload)→ 2 本目が走り出す
4. **ポーリングのほう(1 本目)が先に着弾** → 次の合図は張られる
5. その合図での tick → **読み直しを出してはいけない**(2 本目がまだ走っている)
6. 2 本目が着弾して初めて 0 本になり、次の tick で発行できる

真偽値ゲート相当(`arrive` で常に 0 にする)へ退行させると、手順 5 で
`tui.dashboardRefreshedMsg` が予約され 3 ペインとも失敗することを確認した。

### 否定した懸念(REFUTED)

- **`agents` キーを省略した設定で screen 方式を取りこぼすのでは。**
  取りこぼさない。設定の読み込みは「config.json があればそれだけ、無ければ
  config.default.json だけ」というファイル単位のフォールバックで、キー単位の
  マージをしない(`store.ConfigPath`)。したがって `agents` を省いた config.json を
  置いた利用者にはエージェント定義が 1 つも無く、screen 方式のタスクが存在
  しえない。`AgentDetection` も明示されていないものはすべて hooks に落とす。
  設定そのものが読めない場合は指摘 5 の対応で fail-open になる。
- **Waiting にも同じ不具合が波及するのでは。**
  しない。Waiting は終了キー以外のキー入力を受け付けず(`waiting.go`)、
  キー操作起源の読み直し(force)も 2 打鍵目の待ち受けも取得中の画面も持たない。
  ポーリングの読み直し 1 本だけが回るため、指摘 3・6・7 の前提が成立しない。
  それでも仕組みは 4 ペイン共通の `poller` を使い、テーブルテストにも
  含めている(将来キー操作が増えたときに取り残さないため)。

## 7. 挙動差と残る未検証事項

1. **再描画の間隔が「間隔ちょうど」から「読み直しの時間 + 間隔」に変わる。**
   平常時の Dashboard で 2 秒周期 → 約 3.3 秒周期になる。これは現行 Shell 版と
   同じ周期であり、移植の観点ではむしろ現行に近づく。負荷と引き換えに
   表示の更新はそのぶん遅くなる。
2. **`list-tabs` 単体の実行時間を実測していない。** 修正 2(スクリーン検出の
   条件化)の効果を数値で言い切れないのはこのためである。
3. **タイムアウトの値(15 / 10 / 60 秒)は実測ではなく設計値である。**
   正常時の実測(list-panes で 1.1〜1.5 秒)より十分長く、利用者が固まったと
   感じるより短いところに置いた。実環境で切られる頻度は再テストで見る。
4. 修正後の実セッションでの再テスト(タブ遷移が安定するか)は利用者に依頼する。
   本 PR は負荷源の削減のみで、根本対策 1/2(claude-conductor 側の `create_task`
   明示フォーカス)は別 PR である。
