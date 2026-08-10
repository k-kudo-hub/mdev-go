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

### 2.3 修正後(in-flight ガード)

前回の Refresh が着弾するまで tick は Refresh を発行しない。したがって
**同時に走る Refresh は常に 1 本以下**になり、発行の周期は S の倍数へ丸められる。

```
周期 = ceil(T / S) × S
占有率 = T / (ceil(T / S) × S)
```

| 状況 | T | 修正前の同時実行数 | 修正後の周期 | 修正後の占有率 |
|------|---|--------------------|--------------|----------------|
| 平常時 | 1.3 秒 | 0.65 本 | 2 秒 | 65% |
| 軽い劣化 | 3 秒 | 1.5 本 | 4 秒 | 75% |
| 重い劣化 | 5 秒 | 2.5 本 | 6 秒 | 83% |

**正直に書くと、T < S である平常時の占有率は 65% のままで下がらない**。
in-flight ガードが効くのは T ≥ S に劣化してからで、そこで同時実行数を 1 に
抑え、正のフィードバックを断ち切る安全弁である。Shell 版と同じ 39% にするには
「着弾してから S 秒待つ」形に tick の張り直し方式そのものを変える必要がある
(6 章)。

### 2.4 修正後(スクリーン検出の条件化)

codex(screen 方式)を設定していない環境では、Refresh から list-panes が
丸ごと消え、残るのは `list-tabs` 1 回だけになる。T を支配していたのが
list-panes(1.1〜1.5 秒)である以上、この環境の占有率は大きく下がる。

ただし **`list-tabs` 単体の実行時間は本 PR では実測していない**ため、
「何 % になる」とは書かない。確実に言えるのは「1.1〜1.5 秒の呼び出しが
1 回ぶん丸ごと無くなる」ことだけである。

## 3. 修正 1: in-flight ガード(4 ペイン共通)

`internal/tui/pane.go` に `refreshGate` を置き、Dashboard / Waiting / Done /
News が同じ仕組みを共有する。

| 経路 | 扱い |
|------|------|
| `tickMsg` | `take()`。実行中なら Refresh を発行せず、次の合図(tickCmd)だけを予約する |
| `*RefreshedMsg` | ハンドラの**先頭**で `release()`。エラーで返った場合も、待ち受け中で内容を捨てる場合も必ず通す |
| キー操作・後始末(ジャンプ / restore / reload / 削除の完了 / 通知の期限切れ / 待ち受けの時間切れ) | `force()`。利用者の操作への反応は前回の完了を待たずに出す |
| 凍結中(`busy` / `awaiting` / `fetching`)の tick | 印を立てない。立てると凍結が解けた後のポーリングが二度と読み直せなくなる |

### 起動直後の 1 回をどう数えるか

`Init()` は値レシーバでモデルを書き換えられないため、Init の中で印を立てられない。
`New*Model` が `refreshGate{inFlight: true}` で始めることで、Init が必ず出す
1 回目の Refresh もガードの内側に入る。これが無いと、起動直後の Refresh と
最初の tick が重なる余地が残る。

### テストでの固定方法(`internal/tui/tick_internal_test.go`)

`tickMsg` は非公開型なので内部テストから流し込む。判別には既存の `immediate`
ヘルパを使う。読み直しを束ねた `tea.Batch` はその場でメッセージを返すのに対し、
タイマー(`tea.Tick`)だけなら間隔ぶん待たないと返らない。この差で
「予約されたのがタイマーだけかどうか」を実時間を待たずに見分けている。

| テスト | 固定している取り決め |
|--------|----------------------|
| `TestPaneTickSkipsRefreshWhileInFlight`(4 ペイン) | 生成直後(Init の読み直しが実行中)の tick は発行しない → 着弾後の tick は発行する → 着弾前の tick は発行しない → 着弾後はまた発行する、の 4 段階。ユースケースの呼び出し回数でも二重発行を検出する |
| `TestPaneRefreshErrorReleasesInFlight`(Dashboard / Waiting) | **エラーで返った着弾でも印が下りる**こと。下ろし忘れると 1 回の失敗でポーリングが永久に止まる |
| `TestDashboardTickIsFrozenWhileAwaiting` / `...WhileBusy` / `TestDoneTickIsFrozenWhileAwaiting` / `TestNewsTickIsFrozenWhileFetching` | 凍結中の tick が読み直しを発行しないこと(既存の取り決め)に加えて、**凍結中の tick が印を立てない**こと。凍結を解いた直後の tick が読み直しを発行できることで確かめる |
| `TestDashboardDroppedRefreshReleasesInFlight` | 待ち受け中に着弾して内容を捨てた読み直しでも印が下りること |

「立て忘れ」は各テストの最後で「発行できるはずの tick が発行しているか」、
「下ろし忘れ」は「発行してはいけない tick が発行していないか」の両方向から
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
`--once` は `Once()` を通り tick を回さないため in-flight ガードの影響を受けない。
スクリーン検出の条件化も、ゴールデンの入力に `config.json` を置くケースが無く
(= agents が空 → 検出を呼ばない)、かつ元々サンドボックスにスクリプトが
無いため副作用が無い。よって出力は 1 バイトも変わらない。

```
$ make check
gofmt: no diff
golangci-lint: 0 issues.
go-arch-lint: OK - No warnings found
go test -race: 全パッケージ ok
Total test coverage: 89.9% (1596/1775)  # domain 93.5% / app 94.4%(閾値 90%)
go build -o bin/mdev ./cmd/mdev
```

## 6. 残る差分・未着手の選択肢

1. **平常時(T < S)の占有率は 65% のままである。** Shell 版と同じ 39% にするには、
   ポーリングの張り直しを「tick から S 秒後」ではなく「着弾から S 秒後」に
   変える必要がある。この変更は「ポーリングのチェーンを張り出すのは Init と
   tickMsg のハンドラだけ」という既存の取り決め(`TestPaneRefreshDoesNotReschedulePolling`
   が固定している。着弾で張り直すと tick 以外の生成元のぶんだけチェーンが
   増殖する)に触れるため、本 PR では採らなかった。in-flight ガードが入った
   今なら安全に実装できるが、設計判断として別途レビューすべきである。
2. **`list-tabs` 単体の実行時間を実測していない。** 修正 2 の効果を数値で
   言い切れないのはこのためである。
3. 修正後の実セッションでの再テスト(タブ遷移が安定するか)は利用者に依頼する。
   本 PR は負荷源の削減のみで、根本対策 1/2(claude-conductor 側の `create_task`
   明示フォーカス)は別 PR である。
