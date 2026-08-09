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

## 5. Shell 版との意図的な差異

(実装しながら追記する)
