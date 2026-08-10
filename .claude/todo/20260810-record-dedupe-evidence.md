# daily 追記の冪等化(置換)の調査記録(evidence)

移植元: `claude-conductor` の `scripts/record-output.sh` 末尾ブロックと `test.sh` セクション 26i2。
実行環境は macOS 15 / `jq-1.7.1-apple` / Go 1.25。
現行 Shell 版の挙動はすべて隔離サンドボックス(`env -i` で `HOME` と `CONDUCTOR_HOME` を
一時ディレクトリへ向けたもの)での実測に基づく。実環境のファイルには触れていない。

## 1. Shell 版の削除条件と Go 実装の対応

現行版の削除は次の 1 行に集約されている。

```sh
jq -c --arg tab "$TAB_NAME" --arg sid "$CLAUDE_SESSION_ID" \
    'select(((.tab == $tab) and ((.claude_session_id // "") == $sid) and ((.restored // false) != true)) | not)'
```

Go 側は `internal/infra/store/daily.go` の `filterSupersededDaily` がこれに対応する。
判定を `map[string]any` で行い、値の型が想定と違っても解析を止めない作りにした。理由は
jq の比較セマンティクスに合わせるためである。

### 型が違う行の扱い(実測)

`tab` が数値の行を混ぜて現行版を走らせた結果:

```console
$ printf '%s\n' '{"tab":123,"claude_session_id":"sid","message":"weird"}' \
                '{"tab":"t","claude_session_id":"sid","message":"old"}' > $DAILY
$ record-output.sh t
$ cat $DAILY
{"tab":123,"claude_session_id":"sid","message":"weird"}
{"tab":"t","session":"s","completed_at":"...","message":"new",...,"claude_session_id":"sid"}
```

`tab` が数値の行は「一致しない行」として残り、文字列で一致した行だけが消えた。
Go 側で `DailyRecord` のような構造体へ Unmarshal すると型不一致で解析エラーになり、
フェイルセーフ(削除見送り)へ落ちて挙動が変わってしまう。`map[string]any` +
文字列アサーション(`dailyString`)にしたのはこのためである。

## 2. フェイルセーフ(解析できない daily)

現行版は jq が失敗した場合に一時ファイルを捨て、削除せず追記だけを行う。実測:

```console
$ printf '%s\n' '{"tab":"t","claude_session_id":"sid","message":"old"}' 'これは JSON ではない' > $DAILY
$ record-output.sh t
$ cat $DAILY
{"tab":"t","claude_session_id":"sid","message":"old"}
これは JSON ではない
{"tab":"t","session":"s","completed_at":"...","message":"new",...}
```

既存の 2 行はそのまま残り、末尾に 1 行増える。重複した記録は後から消せるが、
切り詰めた記録は取り戻せないため、この非対称性をそのまま移植した。
Go 側は `filterSupersededDaily` が 1 行でも解析に失敗したら `removed=false` を返し、
書き直しそのものを見送る(`TestDailyStoreAppendKeepsBrokenDailyIntact`)。

## 3. 空行の扱い

jq は入力の空行を値の区切りとして読み飛ばすため、置換が起きたファイルからは空行が消える。実測:

```console
$ printf '%s\n' '{"tab":"t","claude_session_id":"sid",...}' '' '{"tab":"x","claude_session_id":"other",...}' > $DAILY
$ record-output.sh t
$ wc -l < $DAILY
2
```

Go 側も `strings.TrimSpace(line) == ""` の行を落とすため一致する。
なお削除対象が 1 行も無い場合は書き直し自体を行わないので、空行はそのまま残る
(現行版も `CLAUDE_SESSION_ID` が空、または daily が無い場合は jq を通さない)。

## 4. 残す行を再整形しない判断

現行版は `jq -c` で全行を読み直して書き戻すため、残る行も再整形される。Go 側は
読んだ行の文字列をそのまま書き戻す。ゴールデンテストは JSON としての等価で比較するため
この差は表に出ず、mdev が知らないフィールドや表記(数値の書き方など)を壊さない分だけ安全である。

## 5. dedupe キーから `screen-` 前置きを外した理由

conductor 側の code-review で確定した仕様変更。スクリーン検出は
`scripts/screen-detect-lib.sh:97` で

```sh
--arg claude_session_id "screen-$slug"
```

として pending を作る。`$slug` は `_screen_tab_slug`(mdev-go では `domain.ScreenTabSlug`)が
返すタブ名の純関数であり、同じ名前のタブなら別のタスクでも同じ値になる。

したがって `screen-<slug>` を置換キーに使うと、同名タブで過去に完了した別タスクの記録まで
削除条件に一致してしまう。**キーとして使えるのは `claude_session_id` が非空かつ
`screen-` で始まらない場合だけ**とし、それ以外は従来どおり無条件追記にした
(`domain.DailyRecord.HasDedupeKey`)。重複が残るのは Done ペインの見た目の問題だが、
履歴の誤削除は復旧できないため、安全側へ倒している。

## 6. ロックを取れないとき(fail-open)は置換しない

daily ファイルのロックは 2 秒で諦めて処理を続ける(fail-open)。この状態で置換を行うと、
ファイル全体の書き直しが並行する `restore-task` の結果(`restored: true` の付与)を
巻き戻しかねない。追記は `O_APPEND` なので競合しても行を失わないが、全体書き直しは失う。

そのため `DailyStore.Append` は**ロックを取得できたときだけ**削除フィルタを走らせ、
取れなかった場合は追記のみ行う(`TestDailyStoreAppendSkipsReplacementWhenLockUnavailable`)。
ロック無しでの重複は次回の(ロックを取れた)実行で解消される。

## 7. ゴールデン fixture の再生成手順

```console
$ bash scripts/gen-golden-record.sh /Users/kazuto/projects/claude-conductor/.worktree/fix-upload-codex-record-dedupe
30 件の fixture を .../golden-record に生成しました
$ go test ./internal/infra/store/ -run TestGoldenRecord -v | grep -c -- '--- PASS'
30
```

`cases.json` に `"runs": N` を足し、`gen-golden-record.sh` は同じ sandbox のまま
`record-output.sh` を N 回続けて走らせるようにした。Go 側のゴールデンテストも
`runCount()` 回だけ `Execute` を呼ぶ。Shell 版は実行ごとに `date` を呼ぶため 1 回目と
2 回目の `completed_at` は異なりうるが、置換で残るのは最後の 1 件だけなので、
Go 側の固定時刻(fixture の最終行から復元)と一致する。

### 既存 fixture を変更しないための確認

再生成すると全ケースのファイル名の日付と `completed_at` が今日の値へ変わる。
`completed_at` を定数へ潰し、ファイル名の日付を無視して旧 fixture(`git show HEAD:<path>`)と
突き合わせた結果、**内容が変わったケースは 0 件**だった(新規の `retry-replaces-entry` のみ追加)。
そのうえで既存 29 ケースは `git checkout` と生成物の削除で元へ戻し、新規ケースだけを追加した。

### 新規ケース `retry-replaces-entry`

同一 pending で `record-output.sh` を 2 回走らせる。既存 daily には
(a) 同じ tab + sid で `restored: true`、(b) 同じ tab + sid で未 restore、
(c) 別 tab + 別 sid、の 3 行を置いてある。Shell 版の出力は:

```
{"tab":"retry-tab",...,"message":"restored-history",...,"restored":true}
{"tab":"other-tab",...,"message":"other-task",...}
{"tab":"retry-tab",...,"message":"latest attempt",...,"claude_session_id":"sess-retry"}
```

(b) だけが消え、(a) と (c) は位置ごと残り、新しい記録が末尾に来る。1 ケースで
「restored は対象外」「別キーは対象外」「未変更行の相対順序」「置換行の末尾配置」
「2 回実行で 1 行」のすべてを押さえている。

## 8. 未解決 / 判断を保留した点

- **conductor 側の再確認待ち**: `screen-` 前置きの除外とロック fail-open 時の置換スキップは
  conductor 側でも反映中である。この fixture は反映前の Shell 版から生成したが、
  ケースの sid は `sess-retry`(`screen-` ではない)でロックも空いているため、
  どちらの変更にも影響されない経路しか通っていない。conductor 側の確定後に再生成して
  同一であることを確認する必要がある。
- **`screen-` sid のゴールデンケース**: conductor 側の確定後に追加を検討する
  (`runs: 2` + `screen-<slug>` の pending で 2 行になること)。現時点では Go 側の
  ユニットテスト `TestDailyStoreAppendKeepsScreenSessionEntries` で固定している。
- **app 層のテストは赤を先に出せていない**: `RecordOutput` 自体の振る舞いは変わらない
  (dedupe キーは pending から素通しで渡るだけ)ため、
  `TestRecordOutputRepeatsTheSameDedupeKeyOnRetry` は退行防止の characterization テストである。
  赤から始めたのは domain と store のテストである。
