# フェーズ1コア基盤: 移植時の確認記録

現行 Shell 版(claude-conductor)の挙動を Go に移すにあたり、仕様が不明だった点を
実行して確認した記録と、意図的に挙動を変えた点の一覧である。

実行環境: macOS 15 (Darwin 24.5.0) / bash 3.2.57 / jq 1.7.1-apple / go 1.25.5 darwin/arm64。
確認はすべて `HOME` と `CONDUCTOR_HOME` を一時ディレクトリへ向けて行い、実環境の
ファイルには触れていない。

## 1. jq の `//` 演算子は空文字を既定値に置き換えない

`pending-notify.sh` は `jq -r '.message // "Needs attention"'` で既定値を与えている。
jq の `//` が偽と見なすのは **null と false だけ**で、空文字は値として扱われる。

```
$ echo '{"message":""}'     | jq -r '.message // "Needs attention"'
                                    <- 空文字がそのまま出る
$ echo '{"message":null}'   | jq -r '.message // "Needs attention"'
Needs attention
$ echo '{}'                 | jq -r '.message // "Needs attention"'
Needs attention
$ echo '{"message":false}'  | jq -r '.message // "Needs attention"'
Needs attention
```

そのため `message` が空文字の入力では pending にも空文字が書かれる。Go 側で
「空なら既定値」と実装すると挙動が変わるため、既定値の適用は jq 式と同じ
「抽出の時点」に置いた(`domain.ParseHookInput`)。ゴールデンテストの
`notify-empty-message` / `notify-null-message` がこの差を固定している。

## 2. jq -r は文字列以外もそのまま出力する

```
$ echo '{"session_id":123}'  | jq -r '.session_id // empty'
123
$ echo '{"session_id":true}' | jq -r '.session_id // empty'
true
```

Claude Code はこれらのキーを常に文字列で渡すため実害は無いが、Go 側で
`string` に決め打ちで unmarshal すると入力全体の解釈が失敗して挙動が変わる。
`map[string]json.RawMessage` で受けて、文字列以外は JSON 表記を返すようにした。
なお jq が行う数値の正規化(`1.50` → `1.5`)とオブジェクトの圧縮までは
再現していない(コード中にコメントとして明記済み)。

## 3. `basename ""` は空文字を返す(Go の filepath.Base は "." を返す)

タブ名のフォールバックは
`TAB_NAME="${TASK_TAB_NAME:-$(basename "$(... .cwd // empty ...)")}"` の後に
`[ -z "$TAB_NAME" ] && TAB_NAME="unknown"` である。

```
$ printf '[%s]\n' "$(basename "")"           -> []
$ printf '[%s]\n' "$(basename "/")"          -> [/]
$ printf '[%s]\n' "$(basename "/tmp/myapp/")"-> [myapp]
$ printf '[%s]\n' "$(basename ".")"          -> [.]
```

`filepath.Base("")` は `"."` を返すため、そのまま使うとタブ名が `unknown` では
なく `.` になる。空文字だけ特別扱いする `shellBasename` を置いた。
`/` と `.` は basename と一致するのでそのまま通している。

## 4. jq の `group_by` / `max_by` の順序

`restore-session.sh:69` の
`group_by(.tab) | map(max_by(.updated_at // "")) | .[]` を実測した。

```
$ echo '[{"tab":"a","u":"1","id":1},{"tab":"a","u":"1","id":2},{"tab":"a","u":"2","id":3}]' \
    | jq -c 'group_by(.tab) | map(max_by(.u)) | .[]'
{"tab":"a","u":"2","id":3}

$ echo '[{"tab":"a","u":"1","id":1},{"tab":"a","u":"1","id":2}]' \
    | jq -c 'group_by(.tab) | map(max_by(.u)) | .[]'
{"tab":"a","u":"1","id":2}          <- 同値は入力順で「最後」が残る

$ echo '[{"tab":"z","u":"1"},{"tab":"a","u":"1"},{"tab":"m","u":"1"}]' \
    | jq -c 'group_by(.tab) | map(max_by(.u)) | .[]'
{"tab":"a","u":"1"} / {"tab":"m","u":"1"} / {"tab":"z","u":"1"}   <- tab 昇順
```

`domain.LatestPerTab` はこの 2 点(同値は後勝ち・結果は tab 昇順)を再現している。

## 5. `kill -0` は権限が無いプロセスも「居ない」と判定する

`lock-lib.sh` の stale 判定は `kill -0 "$owner"` の成否である。bash の kill 組み込みは
EPERM(他ユーザーのプロセス)でも 1 を返すため、現行版は「所有者が消えた」と
みなしてロックを奪う。Go 側も `process.Signal(syscall.Signal(0)) == nil` を
生存条件にして同じ判定にした(ESRCH と EPERM を区別していない)。

## 6. pending ディレクトリの読み手は事前作成に依存していない

`pending-notify.sh` は `mkdir -p "$PENDING_DIR"` を **session_id の検証より前**に
実行するため、session_id の無い入力でも空ディレクトリだけが残る。
これに依存する読み手が無いことを確認した。

- `dashboard-loop.sh:10` / `waiting-loop.sh:8` / `codex-notify.sh:68` は自分で `mkdir -p` する
- `record-output.sh:42` / `waiting-toggle.sh:23` / `task-control.sh` は
  `for f in "$PENDING_DIR"/*.json` + `[ -f "$f" ]` の形で、ディレクトリが無くても動く

したがって Go 版は「書き込み時にだけディレクトリを作る」方式にした(下記の差異 1)。

## 7. 現行仕様との差異

### 差異 1(意図的): session_id が無い入力で pending ディレクトリを作らない

現行版は `mkdir -p` を検証より前に置いているため空ディレクトリが残る。Go 版は
書き込み時にのみ作る。上記 6 のとおり依存する読み手が無いため観測されない。
ゴールデンテストはファイルの集合のみを比較し、空ディレクトリの有無は見ない。

### 差異 2(意図的・堅牢化): 一時ファイルを書き込み先と同じディレクトリに作る

現行 `registry-lib.sh` は `mktemp`(= `$TMPDIR`)に書いてから `mv` している。
`$TMPDIR` が別ファイルシステムにある場合 `mv` はコピーになり原子的でない。
Go 版は必ず書き込み先と同じディレクトリに一時ファイルを作る。生成されるファイルの
内容は変わらないため互換性には影響しない(TODO の「備考」で合意済みの変更)。

### 差異 3(意図的): 設定 JSON が壊れている場合はエラーにする

現行版は `jq ... 2>/dev/null` で握り潰し、単価表が空のまま料金計算を続ける。
Go 版の `store.LoadConfig` は解釈に失敗したらエラーを返す。料金が静かに 0 になる
より、利用者に直してもらうほうが妥当と判断した。ファイルが 1 つも無い場合は
現行版と同じくゼロ値を返す。

### 差異 4(明示化): hook の失敗時に終了コード 1 を返す

現行版は jq の失敗時にその終了コードで終わる(それ以外は 0)。Go 版は
ユースケースがエラーを返したときに標準エラーへ出力して 1 を返す。Claude Code の
hook では 2 以外の非ゼロは非ブロッキング扱いで、会話を止めずにユーザーへ内容が
見える。

### 発見した暗黙仕様(現行版に合わせて再現したもの)

- 壊れた JSON と空ファイルの pending は「event が空」として扱われる。Stop で
  上書きされ、PostToolUse では削除されない
- `pending-notify.sh` はレジストリの更新を pending の上書き判定より **前** に行う。
  Stop が Notification を上書きしない場合でもレジストリだけは最新化される
- `pending-resolve.sh` のレジストリ更新は `TASK_TAB_NAME` を直接使い、
  `pending-notify.sh` のような cwd の basename へのフォールバックを持たない
- `zellij action go-to-tab-name "Main"` は PostToolUse では「Notification の
  pending を実際に削除したとき」にだけ実行され、UserPromptSubmit では
  pending の有無にかかわらず実行される
- `Notification` は `Waiting` を無条件に上書きし、その際 `prev_event` は消える
  (jq がフィールド一覧を作り直すため)

## 8. ゴールデンテストの fixture

- 入力定義: `internal/infra/store/testdata/golden/cases.json`(手書き・22 件)
- 生成コマンド: `scripts/gen-golden.sh [claude-conductor のパス]`
  - `HOME` と `CONDUCTOR_HOME` を `mktemp -d` 配下へ向け、`env -i` で他の環境変数を
    遮断したうえで現行スクリプトを実行する
  - `zellij` は呼び出しを記録するスタブを PATH の先頭に置く(実 zellij は起動しない)
  - 生成物(`pending/`、`tasks/`、`zellij-calls.txt`)を
    `testdata/golden/<case>/expected/` に保存する
  - Shell 版は `date` を 2 回呼ぶため、pending の `time` とレジストリの
    `updated_at` が秒をまたぐことがある。またいだ場合は自動で再実行する
- 検証: `internal/infra/store/golden_test.go`。fixture が持つ時刻を
  固定 Clock として Go 版へ与え、生成されるファイル集合・各ファイルの JSON 内容
  (キー順は問わない)・zellij 呼び出しの並びを突き合わせる

### ゴールデンテストが実際に差分を検出することの確認

意図的な変異を入れて fail することを確認した(確認後はすべて `git checkout` で復帰)。

| 変異 | 結果 |
|------|------|
| `DefaultPendingMessage` を別の文言に変更 | 2 件で「内容が違う」 |
| `ShouldOverwritePending` から Notification の保護を外す | `notify-stop-keeps-notification` が fail |
| PostToolUse が Stop の pending も削除するようにする | `post-tool-keeps-stop` が「生成されていない」と zellij 呼び出しの差で fail |
| タブ名の cwd フォールバックを無効化 | `notify-tab-fallback-from-cwd` が fail |
| `session_id` 空の early return を削除 | `notify-no-session-id` / `notify-broken-stdin` が「Shell 版が作らないファイルが生成された」で fail |

## 9. `make check` の結果

```
gofmt: no diff
golangci-lint run ./...          -> 0 issues.
go-arch-lint check               -> OK - No warnings found
go test -race -covermode=atomic  -> 全パッケージ ok
  internal/app          100.0%
  internal/cli           90.5%
  internal/domain       100.0%
  internal/infra        100.0%
  internal/infra/store   87.6%
  internal/infra/zellij 100.0%
go-test-coverage                 -> Total 90.8% (276/304) PASS
go build -o bin/mdev ./cmd/mdev  -> 成功
CHECK_EXIT=0
```

### 層別閾値(domain / app 90%)が実際に効いていることの確認

`internal/app` に未テストの 22 文を持つ一時ファイルを置いて実行した。

```
below threshold:                        coverage:       threshold:
internal/app                            68.2% (45/66)   90%

Total coverage threshold (70%) satisfied:       PASS
Total test coverage: 84.9% (276/325)
exit status 1
```

全体は 84.9% で PASS のまま `internal/app` だけが落ちており、前タスクで設定した
層別の上書きルールが、実行文を持つようになったこの PR から実際に機能している。
確認後、一時ファイルを削除して PASS に戻ることを確認済み。

### 未カバーの箇所(意図的に残したもの)

- `cmd/mdev/main.go`: DI の組み立てのみ。実プロセスの起動でしか通らない
- `internal/infra/store/atomic.go` / `registry.go` / `lock.go` の一部エラー分岐:
  一時ファイルの chmod / rename 失敗など、ファイルシステムを壊さないと再現
  できない経路。閾値を下げずに済む範囲なので、無理な再現テストは書いていない
