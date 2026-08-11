# フェーズ4 evidence(スクリーン検出 + セッション/タスク復元)

移植元は claude-conductor の `scripts/screen-detect-lib.sh` / `restore-session.sh` /
`restore-task.sh` / `task-lib.sh`。期待値の一次資料は claude-conductor の `test.sh`
(分類 17b5 / ライフサイクル 17b6 / tick 17b7 / 復元 26e-26j)。

ここには「一次情報のどこを読んで何を確定したか」と「Shell 版との意図的な差異」を
書き足していく。

## 1. 分類(screen_classify)

### 1-1. 窓の作り方

`screen-detect-lib.sh:38`

```bash
tail_buf=$(printf '%s\n' "$text" | grep -v '^[[:space:]]*$' | tail -n "$SCREEN_TAIL_LINES")
```

- 空白のみの行を先に落としてから末尾 20 行を取る。dump-screen はビューポート高まで
  空行で埋めるため、生の tail ではパディングしか見えない(同ファイルの :25-28 のコメント)。
- `SCREEN_TAIL_LINES=20`(:28)。

### 1-2. 優先順位

neutral → blocked → working → idle(:43-68)。neutral が最優先なのは、ビューアが
承認文言を映しているだけの画面を承認待ちと誤認しないため(test.sh の
"neutral wins over blocked")。

### 1-3. blocked のメッセージ

`screen-detect-lib.sh:51-58`

```bash
line=$(printf '%s\n' "$tail_buf" | grep -E -m 1 -- "$pattern" 2>/dev/null || true)
if [[ -n "$line" ]]; then
    printf 'blocked\t%s\n' "$(printf '%s' "$line" | sed 's/^[[:space:]]*//')"
```

- **パターン主導**。config の配列順に見て、最初にヒットしたパターンの、
  最初のヒット行を返す(行主導ではない)。
- 返す文字列は行全体から先頭の空白を除いたもの。TAB 区切りで state と繋がる。
- `[[ -n "$line" ]]` があるため、**空行にしか一致しないパターンは blocked にならない**
  (コマンド置換が空文字になる)。neutral / working は `grep -q` なので一致すれば成立する
  という非対称がある。Go 版でも「一致行が空文字なら blocked にしない」で再現した。

### 1-4. 窓が空のとき

`printf '%s\n' "$tail_buf"` は `tail_buf` が空でも改行 1 個(= 空行 1 行)を grep へ渡す。
したがって「空の画面」に対しても、空文字に一致するパターン(`^ *$` など)は
neutral / working として成立する。Go 版は窓が空のとき `[]string{""}` を照合対象にして
これを再現した(`ClassifyScreen` のコメント)。

### 1-5. 正規表現

`grep -E` = POSIX ERE。Go は `regexp.CompilePOSIX` を使い、**行ごとにループ**して
`MatchString` する(grep と同じ「行単位の一致」にするため)。コンパイルに失敗した
パターンは不一致として飛ばす(grep -E も不正な正規表現で非ゼロ終了し、
`2>/dev/null` と `|| true` で不一致に潰れる)。

`config.default.json` の codex パターンは
`^ *Would you like to run the following command\? *$` のように `\?` `\(` を含むが、
Go の POSIX 構文でも句読点のエスケープはリテラルとして解釈されるため一致する。

### 1-6. パターンの取り出し(agent_patterns)

`task-lib.sh:233-238`

```bash
jq -r --arg a "$agent" --arg s "$state" '.agents[$a].patterns[$s] // [] | .[]' ...
```

- agent が空なら即 return(パターン無し)。
- `.patterns` が配列でない/オブジェクトでない場合、jq はエラー終了して
  `2>/dev/null` で空になる。Go 側は `ScreenPatterns.UnmarshalJSON` がキー単位で
  読み、読めないキーだけを空にする(Config の per-entry 許容の流儀)。
- 空文字のパターンは `[[ -z "$pattern" ]] && continue` で飛ばす。

## 2. 状態機械(screen_update_pending)

### 2-1. 実行順序そのものが仕様

`screen-detect-lib.sh:117-248` の順序:

1. `state == neutral` なら**即 return**。`mkdir -p` すら通らない(:122-124)
2. pending ディレクトリと `.screen-state` を作る(:126-134)
3. 前回状態を読む(`prev` / `prev_at`)(:138-141)
4. idle の確定判定をして `effective` を決め、**状態ファイルへ書く**(:151-165)
5. **その後で** Waiting ガード(:169-175)。Waiting があれば副作用なしで return
6. case 別の副作用(:177-246)

つまり「Waiting 中でも内部状態だけは進む」(test.sh "Waiting tab keeps tracking state")。
Go 版の `DecideScreen` は、この順序を副作用リストの順序としてそのまま持つ
(neutral = 空リスト / 先頭は必ず write-state / Waiting なら write-state だけ)。

### 2-2. idle の確定(idle_pending)

`screen-detect-lib.sh:151-164`

```bash
now=$(date +%s)
if [[ "$state" == "idle" ]]; then
    if [[ "$prev" == "working" ]]; then
        effective="idle_pending $now"
    elif [[ "$prev" == "idle_pending" ]]; then
        if [[ "$prev_at" =~ ^[0-9]+$ ]] && [[ $((now - prev_at)) -lt 1 ]]; then
            effective="idle_pending $prev_at"
        else
            confirm_idle=1
        fi
    fi
fi
```

- 差は**整数 epoch 秒**。`date +%s` の値そのままの引き算なので、実測の経過時間が
  0.99 秒でも秒境界をまたげば 1 になる。パリティ優先でこのまま移植した(意図的)。
- `prev_at` が数値でなければ即確定側(`confirm_idle=1`)。
- `prev` が `blocked` / `idle` / 空のときは idle_pending に入らない = 何度観測しても
  done にならない(test.sh "blocked->idle never becomes done")。

### 2-3. working

- タブに属する pending を**全部**消す(:189-194)。notify 由来の Stop も含む。
- `prev` が `blocked` または `idle` のときだけ Main へ帰る(:205-210)。
  空(初回観測)と `idle_pending` と `working` は除外。

### 2-4. blocked

既存の screen pending が `Notification` ならそのまま(時刻を保持)。無い、または
別の event なら書き直す(:181-184)。message が空なら `Approval required`。

### 2-5. idle(3 段)

1. screen pending が `Notification` なら消す(:214-216)
2. screen pending が `Stop` で、**同じタブに他の pending がある**なら消す(:221-229)
3. `confirm_idle == 1` かつ**タブに pending が 1 件も無い**ときだけ `Stop` を書く(:235-244)

1 の削除は 2・3 の判定より先に実際にファイルを消すので、Go 版も削除を
ローカルのスナップショットへ反映してから次の判定に進む。

### 2-6. screen pending の 3 キー借用

`_screen_registry_lookup`(:75-88)は registry から `.tab` 一致のうち
**mtime が最大**のファイルを選び、`dir` / `task_type` / `transcript_path` を返す。
空の値はキーごと省略される(`_screen_write_pending` の jq、:105-108)。

restore-session が `updated_at` で最新を選ぶ(`LatestPerTab`)のとは選択キーが違う。
現行仕様どおり各経路で非対称のまま維持する。

実測(隔離実行)で確認: 同じタブに 2 件のエントリを置き、**古いほうの
`updated_at` を 2099 年に書き換えたうえで mtime を 2020 年に落とす**と、
screen pending が借りるのは新しいほう(mtime 最大)の dir / task_type /
transcript_path だった。選択キーが mtime であることの直接の裏付けである。

### 2-7. 状態機械の差分検証(隔離実行)

Shell 版 `screen_update_pending` を隔離環境で観測列ごと動かし、Go の
`DecideScreen` を同じ列で回した結果と突き合わせた。一致を確認した列:

| 観測列 | Shell の最終状態 |
| --- | --- |
| Waiting あり: blocked → working → idle → (1 秒後)idle | state が blocked→working→idle_pending→idle と進み、pending は park.json のみ |
| working → (外から Notification 追加)→ neutral | state=working のまま、Notification も残る |
| working → idle → (1 秒後)idle → notify Stop 追加 → idle | screen Stop を書いたあと、notify Stop 着弾後の idle で自ら消える |
| 別タブの Waiting がある状態で working → idle → (1 秒後)idle | 影響を受けず screen Stop を書く |
| blocked → idle → idle → (1 秒後)idle | 一度も Stop を書かない |
| blocked(message 空) | `Approval required` |
| working →(notify Stop 追加)→ idle → (1 秒後)idle | 重複 Stop を書かない |

`claude_session_id` は `screen-<slug>`、ファイル名は `screen-<slug>.json`
(`_screen_tab_slug` は移植済みの `domain.ScreenTabSlug`)。

## 3. tick(screen_detect_tick)

`screen-detect-lib.sh:255-285`

- `zellij action list-panes -t -c -j` の JSON を
  `is_plugin == false` かつ `terminal_command != null` かつ
  `terminal_command` が `TASK_AGENT=` を含む、で絞る。
- agent は `capture("TASK_AGENT=(?<a>[^ ]+)")`(空白まで)。
- `agent_detection(agent) == "screen"` のペインだけを dump する。
- `dump-screen -p terminal_<id>`。失敗・空はそのペインを飛ばす(= neutral 相当)。

## 4. restore-session.sh

- registry を**1 ファイルずつ** `jq -c .` で検証読み(壊れた 1 件で全体が止まらない)。
- `group_by(.tab) | map(max_by(.updated_at // "")) | .[]` = 移植済みの
  `domain.LatestPerTab`。jq の `group_by` はキー昇順なので、タブ名昇順で回る。
- `query-tab-names` に載っているタブは飛ばす(エントリは残す)。
- `dir` が空 or 実在しなければ `registry_remove_by_tab` して飛ばす。
- resume は `sid != "" && transcript != "" && -f transcript` の 3 条件。
- `create_task` の rc: 0 と 3 を「復元した」と数える。rc=3 は
  「タブは出来たがフォーカス未確認でペイン未構築」(task-lib.sh:370-378)で、
  Go では `ErrTabNotRegistered` / `ErrFocusNotConfirmed` に対応する。
- 1 件でも復元したら `go-to-tab-name Main`。常に exit 0。

### 意図的差異(改善)

Shell の `zellij action query-tab-names` と `go-to-tab-name Main` は
`_zellij_guarded` を通さない素の呼び出しで、劣化サーバでは返ってこない。
Go 版は `TabActor.QueryTabNames` / `Focuser.FocusTab` 経由でどちらも
10 秒の上限つきになる(infra/zellij)。挙動は「失敗したら空 / 何もしない」で
Shell の `2>/dev/null` と同じ側へ倒れるため、互換性は保たれる。

## 5. restore-task.sh

- 対象は daily の `(tab, completed_at, restored != true)` に一致する**最初の 1 件**。
- 終了コード 0-5 の契約(スクリプト冒頭のコメント):
  0 成功(create_task rc=3 込み) / 1 引数不正・未発見 / 2 dir 無記録 /
  3 dir 不在 / 4 タブ作成失敗 / 5 daily 更新失敗。
- resume は restore-session と同じ 3 条件。
- `restored: true` は daily ロック(2 秒 fail-open)を握ったまま read-modify-rewrite。
  `index(true)` で**最初の 1 件だけ**を反転する(test.sh 26g)。

### 5-1. 発見バグ: `screen-` 前置の sid で resume してしまう

スクリーン検出が書く pending の `claude_session_id` は `screen-<slug>`
(`screen-detect-lib.sh:97`)。record-output はこれを daily の
`claude_session_id` にそのまま書くため、Done から復元すると
`codex resume screen-cx_task-1234567890` という**存在しないセッション ID** で
エージェントが起動する。transcript(rollout)は registry から借りて実在するので
3 条件は通ってしまう。

Go 版は resume 判定に「sid が `domain.ScreenSessionIDPrefix` で始まらない」を
追加して修正した。差が出るケースはゴールデンから外し、ユニットテストで固定する
(理由をテストのコメントに明記)。conductor 側の同じバグは別 PR の候補として残す。

### 5-2. 意図的差異: daily の書き直し方

Shell は `jq -s ... | .[]` でファイル全体を再出力するため、**触っていない行まで
jq -c の整形に置き換わる**(数値表記や空白が正規化される)。Go 版は対象行にだけ
`"restored":true` を差し込み、他の行はバイト列のまま残す。未知のキーや表記を
壊さないためで、ゴールデンの比較は行ごとの JSON 等価で行う。

## 6. ゴールデン

`scripts/gen-golden-restore-task.sh` が stub zellij + fixture(daily / registry)で
Shell 版 `restore-task.sh` を隔離実行し、
`cmd/mdev/testdata/golden-restore-task/<case>/` に

- `exit.txt` … 終了コード
- `daily.jsonl` … 実行後の daily(restored の付き方)
- `zellij.log` … stub が記録した zellij 呼び出し列

を保存する。Go 側は同じ入力から `app.TaskRestorer` を動かして突き合わせる。
