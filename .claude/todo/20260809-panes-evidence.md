# フェーズ2 ペイン移植: 検証記録(evidence)

憶測を残さないための実測ログ。着手時から随時追記する。

## 1. Bubble Tea v2 のモジュールパス(重要な前提の訂正)

TODO と ADR-0002 は依存を `github.com/charmbracelet/bubbletea/v2` と書いているが、
**v2 系はモジュールパスが `charm.land/bubbletea/v2` に改称されている**。

実測(2026-08-09):

```
$ go get github.com/charmbracelet/bubbletea/v2@v2.0.8
go: github.com/charmbracelet/bubbletea/v2@v2.0.8 requires ...:
	module declares its path as: charm.land/bubbletea/v2
	        but was required as: github.com/charmbracelet/bubbletea/v2
```

alpha.1 から v2.0.8 まで全バージョンの go.mod を proxy から取得して確認したところ、
v2 系は最初から `module charm.land/bubbletea/v2` を宣言していた。

```
$ curl -s https://proxy.golang.org/github.com/charmbracelet/bubbletea/v2/@v/v2.0.0.mod | head -1
module charm.land/bubbletea/v2
```

採用版: **charm.land/bubbletea/v2 v2.0.8**(2026-08-09 時点の最新)。

対応: `.golangci.yml` の depguard 許可リストと ADR-0002 の採用ライブラリ表を
`charm.land/bubbletea/v2` へ訂正する。lipgloss は使わない(ANSI 直書き)ため
許可リストに入れない。bubbles も現時点では不要(キー入力は 1 打鍵 + prefix
状態のみで、テキスト入力もリストもないため)。

## 2. Bubble Tea v2 の API(モジュールソースで確認)

`$(go env GOMODCACHE)/charm.land/bubbletea/v2@v2.0.8` の実ソースから確認した。
v1 との差分で実装に効くもの:

| 項目 | v1 | v2.0.8 |
|------|----|--------|
| Model.Init | `Init() Cmd` | `Init() Cmd`(同じ) |
| Model.Update | `Update(Msg) (Model, Cmd)` | `Update(Msg) (Model, Cmd)`(同じ) |
| Model.View | `View() string` | **`View() View`**(構造体)。`tea.NewView(s)` で作る |
| キー押下 | `tea.KeyMsg`(構造体) | **`tea.KeyPressMsg`**。`KeyMsg` は press/release 共通の interface |
| Msg | `interface{}` | `= uv.Event`(ultraviolet の型エイリアス) |

`tea.Tick(d, fn)` / `tea.Every(d, fn)` / `tea.Quit` / `tea.WithInput` /
`tea.WithOutput` / `tea.WithoutRenderer` は v1 と同名で存在する。

## 3. cksum(POSIX CRC)互換の実測

`_screen_tab_slug` は `printf '%s' "$1" | cksum | awk '{print $1}'` を使う。
Go の `hash/crc32`(IEEE, 反転あり)とは別物なので、CRC-32/CKSUM
(多項式 0x04C11DB7 / init 0 / 非反転 / xorout 0xFFFFFFFF / 末尾に
バイト長のオクテット列を追加)を自前実装し、実際の `cksum` コマンドと突き合わせた。

| 入力 | `cksum` 実測 | Go 実装 | 一致 |
|------|--------------|---------|------|
| `my-task` | 805046993 | 805046993 | ✅ |
| (空文字) | 4294967295 | 4294967295 | ✅ |
| `あいう` | 2085384042 | 2085384042 | ✅ |
| `a` | 1220704766 | 1220704766 | ✅ |
| `Main` | 1777269351 | 1777269351 | ✅ |
| `hello world` | 1135714720 | 1135714720 | ✅ |
| `タスク-01` | 268066415 | 268066415 | ✅ |

`tr -c 'A-Za-z0-9_.-' '_'` の側はバイト単位で置換する(マルチバイト 1 文字が
`_` 複数個になる)。Go でも byte 単位で走査して再現する。

## 4. 現行スクリプトの既知バグ(そのまま再現する)

- **Dashboard のタブ名は 3 列目のみ**: `zellij action list-tabs | tail -n +2 | awk '{print $3}'`
  なのでスペースを含むタブ名は 3 列目の断片しか取れず、pending の `.tab` と
  一致しなくなり表示されない。一方、削除時の id 解決は「先頭 2 列を除去」方式
  (`sub(/^[^ ]+ +[^ ]+ +/, "", line)`)でスペース入りに対応している。非対称。
- **Dashboard の `for tab_name in $tab_order`** は非クオートの単語分割なので、
  タブ名がスペースを含むと更に分割される。上と同じ結果(表示されない)。
- **Done は壊れ JSON 1 行で全滅**: `jq -s` が失敗 → `daily_stats` が空 →
  `task_count` が空 → `${task_count:-0}` で 0 → "No tasks completed yet"。
- **Done の `%-14s` はバイト幅**: bash の printf はバイト単位で幅を数えるため、
  日本語タブ名の桁がずれる。
- **News の ITEM_COUNT**: ファイルが無い経路では `render` が早期 return するので
  `ITEM_COUNT` が更新されない(前回値が残る)。ONCE 経路では render のみなので影響なし。

## 5. Dashboard 実測(隔離環境)

`env -i` で HOME / CONDUCTOR_HOME を一時ディレクトリへ向け、zellij はスタブに
差し替えて ONCE 出力を採取した。

```
env -i PATH="$SB/bin:/usr/bin:/bin:..." HOME="$SB/home" CONDUCTOR_HOME="$SB/conductor" \
  LC_ALL=C TERM=dumb MOCK_TABS="beta alpha" ZELLIJ_SESSION_NAME=s1 \
  CONDUCTOR_DASHBOARD_ONCE=1 bash dashboard-loop.sh
```

得られた出力(`cat -v`):

```
^[[1m  Current Tasks^[[0m ^[[2m[s1]^[[0m
^[[2m  ──(26 本)──^[[0m

  ^[[0;33m[1]^[[0m ^[[0;32m■^[[0m ^[[1mbeta^[[0m ^[[2m[10:01:00]^[[0m done
      turn finished

  ^[[0;33m[2]^[[0m ^[[0;31m■^[[0m ^[[1malpha^[[0m ^[[2m[10:00:00]^[[0m
      needs permission

^[[2m  ──(26 本)──^[[0m
  ^[[1mPending: 2^[[0m  ^[[2m[num]: jump / d+[num]: delete^[[0m
^[[2m  ──(26 本)──^[[0m
```

MOCK_TABS の順(beta → alpha)がそのまま表示順になり、タブ順が pending の
ファイル名順(a.json = alpha, b.json = beta)より優先されることを確認した。

`domain.RenderDashboard` の戻り値をこの実測ファイルとバイト比較するテストを
一時的に置いて一致を確認済み(恒久版はゴールデンテストが担う)。

## 5b. ロケール依存の 2 か所(重要)

### bash の printf はバイト幅(ロケール非依存)

Done の `%-14s` はロケールを変えても常にバイト幅で詰まる。当初 zsh で
`printf '[%-14s]' 日本語タブ` を試して文字幅に見えたのは**シェルの違い**で、
ペインは `bash` で起動されるためバイト幅が正しい。

```
zsh の printf   : [日本語タブ         ]   <- 文字幅(5 文字 -> 9 個詰め)
bash の printf  : [日本語タブ]            <- バイト幅(15 バイト > 14 で詰めなし)
LC_ALL=en_US.UTF-8 bash: [日本語タブ]     <- ロケールを変えても同じ
/usr/bin/printf : [日本語タブ]
```

Go の `fmt` はルーン幅で詰めるため使えず、`padRightBytes` / `padLeftBytes` を
自前で用意した。

### tr -c はロケール依存(文字単位 / バイト単位が切り替わる)

`_screen_tab_slug` の `tr -c 'A-Za-z0-9_.-' '_'` は BSD tr のため、ロケールで
挙動が変わる。実測:

| ロケール | `あいう` | `タスク-01` |
|----------|----------|-------------|
| `LC_ALL=C` | `_________-2085384042` | `_________-01-268066415` |
| `LC_ALL=UTF-8`(不正な名前) | `_________-2085384042` | `_________-01-268066415` |
| `LC_ALL=en_US.UTF-8` | `___-2085384042` | `___-01-268066415` |
| `LC_CTYPE=UTF-8`(**実行環境の設定**) | `___-2085384042` | `___-01-268066415` |
| ロケール変数なし(`env -i`) | `_________-2085384042` | — |

実行環境は `LC_CTYPE=UTF-8` なので**文字単位**が正しい。`ScreenTabSlug` は
文字単位で実装した。

なお task-lib.sh のコメントは「tr -c mangles multibyte names byte-wise」と
書いているが、これは C ロケールでの話で、実環境の UTF-8 ロケールでは
文字単位になる。コメントの方が実態と合っていない。

**リスク**: screen-state ファイルを書くのは Shell(screen-detect-lib.sh)、
消すのは Go 側なので、ペインが C ロケールで起動されると日本語タブ名の
スラグが食い違って消し漏れる。ASCII のタブ名では一致するため影響は
日本語タブ名に限られる。フェーズ 4 でスクリーン検出を Go 化すれば解消する。

## 5c. list-tabs パースの差分テスト

`ParseTabNames` / `ResolveTabID` を、現行の
`tail -n +2 | awk '{print $3}'` と
`awk 'NR>1 { line=$0; sub(/^[^ ]+ +[^ ]+ +/, "", line); if (line == name) print $1 }'`
に同じ入力を与えて突き合わせた(出力 10 通り × タブ名 8 通り)。全件一致。
連続スペース・タブ文字混在・重複タブ名・2 列しか無い行も含む。

## 6. Shell 版との意図的な差異

### 表示に関わるもの

- **message のバックスラッシュ解釈**: 現行は `echo -e "      $msg"` なので
  message 中の `\n` `\t` 等がエスケープとして解釈される。Go 版は解釈せず
  そのまま出す。hook が書く message は transcript の 1 行で、バックスラッシュ列を
  意図的に含める経路が無いため。
- **pending の値が配列・オブジェクトのとき**: jq -r は複数行の整形済み JSON を
  出すが、Go 版は 1 行の compact JSON を返す。hook は必ず文字列として書くため
  到達しない経路。
- **削除中の進行表示**: 現行は最終行を `\r` で上書きしていた
  (`Uploading log...` / URL / `Upload failed. Deletion cancelled.`)。
  Bubble Tea は画面全体を差分描画するため、本体の下に 1 行足す形にした。
  文言は同じ。`--once` では通知が出ないのでゴールデンには影響しない。

### 並び順

- **glob の並び順**: 現行の `for f in "$PENDING_DIR"/*.json` はロケール依存の
  照合順序で並ぶ。Go 版は `os.ReadDir` のバイト昇順で固定する。pending の
  ファイル名はエージェントのセッション ID(ASCII)なので実際には一致する。
- **daily ファイルの連結順**: 現行は `find` の探索順(ディレクトリの並び)。
  Go 版はセッション名の昇順に固定した。`sort_by(.completed_at)` は安定ソート
  なので、**完了時刻が同着のエントリ同士**の並びだけが環境によって変わりうる。
  決定的にするために固定した。

### 削除フローの失敗経路

- **record-output 失敗時は削除を中止する**(レビューで確認・追記)。現行 Shell は
  `record-output.sh` の終了コードを無視して upload → 削除へ進むが、upload-log は
  record が書いた daily 行に依存しており(古い同名タブの行を拾う危険)、終端は
  タブ close という破壊的操作である。記録に失敗した作業を消さない方が安全な
  ため、Go 版はエラーを表示して何も消さない。「upload 失敗時は何も消さない」
  契約の自然な拡張である。

### キー入力

- **「無関係キーを捨てて残時間を待ち直す」方式が不要になった**。
  現行 Dashboard は `read -t` の戻りが早すぎると再描画が連続し、スクリーン検出の
  idle 確定が一瞬で成立してしまうため、無関係キーでは待ち直す作りだった
  (dashboard-loop.sh:112-131 のコメント)。Bubble Tea では再描画のティックと
  キー入力が別々のメッセージなので、キーが来ても勝手にポーリングが進むことは
  なく、この仕組みは構造的に不要である。
- **Ctrl+C でペインが終了する**。現行の Shell 版ペインは終了キーを持たず、
  zellij がペインごと落とすまで回り続けた。Bubble Tea は端末を raw モードに
  するため、Ctrl+C を拾わないと手動で止められなくなる。移行期の運用しやすさを
  優先して受け付けることにした。

### 到達しない経路の簡略化

- **10 件目以降のキー選択**: 現行はいずれのペインも `[[ "$key" =~ [1-9] ]]` で
  1 文字しか見ないため、番号は振られていても 10 件目以降はキーで選べない。
  Go 版も同じ制限にした(ゴールデンの `dashboard-many` / `done-many` で
  番号表示が 12 まで続くことを固定している)。

## 7. Bubble Tea v2 で分かった制約と工夫

- **`View()` が `tea.View` 構造体を返す**(v1 は `string`)。`tea.NewView(s)` で
  包む。中身は `Content` フィールドで取り出せるため、テストから描画結果を
  文字列として比較できる。
- **`tea.KeyPressMsg` は `Key` の別名型**。テストからは
  `tea.KeyPressMsg{Code: 'd', Text: "d"}` のように組み立てられ、`String()` が
  `"d"` を返す。端末を用意せずにキー入力を流せる。
- **`--once` は Bubble Tea を起動しない**。`tea.NewProgram(...).Run()` は端末を
  要求するため、ゴールデンテストや CI からは通せない。モデルに `Once()` を
  持たせ、`View()` と同じ文字列を組み立てて返す経路を分けた。
- **2 打鍵目のタイムアウトは世代番号で管理する**。`tea.Tick` は取り消せないので、
  待ち受けをやり直すたびに token を増やし、古いタイマーの発火は無視する。
- **メッセージ型が非公開でもテストできる**。`Init()` やコマンドが返した
  メッセージをそのまま次の `Update` へ渡す形にすれば、テストから型名を
  書かずに状態を進められる。

## 8. 依存方向(ADR-0002)で設計を変えた点

当初は tui の `View()` が `domain.RenderDashboard` を直接呼ぶ構造にしたが、
go-arch-lint が `Component tui shouldn't depend on internal/domain` を検出した。
ADR-0002 は tui → app のみを許している。

そこで**描画結果を app のスナップショットが運ぶ**構造へ変えた。

- `app.DashboardSnapshot` / `DoneSnapshot` / `NewsSnapshot` が
  `Text`(domain のレンダリング結果)と、番号キーの解決に要る
  `Tabs []string` / `Count int` だけを公開する
- domain の型(`PendingView` / `DoneRow` / `NewsItem`)は非公開フィールドに
  閉じ込め、tui からは触れないようにする
- tui 側もユースケースを `DashboardService` などの interface として定義した。
  具象型 `*app.DashboardPane` に依存させると、tui のテストが app の port
  (domain の型を受け渡しする)を実装せざるを得ず、テストファイル経由で
  tui → domain の依存が生まれてしまうため

結果として「View は domain のレンダリング関数に委譲する」という当初の狙いは
保たれ(呼ぶのが app になっただけ)、依存方向の規約も守れている。

## 9. ゴールデンテストの構成

`cmd/mdev/testdata/golden-panes` に 23 ケース。全件で Shell 版の ONCE 出力と
バイト単位で一致することを確認済み。

| ペイン | ケース |
|--------|--------|
| dashboard | basic(Stop/Notification 混在・60 バイト切り)/ empty / broken-json / waiting-excluded / unknown-tab / many(12 件)/ missing-keys |
| waiting | basic / empty / multiple / closed-tab |
| done | basic(markers・全セッション横断)/ empty / broken-json(全滅)/ restored-excluded / multibyte-tab(桁ずれ)/ many(12 件) |
| news | basic / empty / no-items / broken-json / missing-description(null・空・キー無し)/ many(7 件) |

テストが `cmd/mdev` にあるのは、実行時と同じ依存グラフ(infra の実装まで)を
組み立てる必要があるためである。全パッケージを参照してよいのは ADR-0002 で
cmd/mdev だけと決まっている。

日付の扱い: daily とニュースのファイル名は「今日」で決まり、Shell 版は `date` を
直接呼ぶため差し替えられない。生成時の日付を `date.txt` に記録し、Go 側は
同じ日付を返す時計を差し込んで突き合わせている。

改竄検知の確認: `expected.txt` を書き換えるとテストが失敗することを実測した
(比較が素通りしていないことの確認)。
