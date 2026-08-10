# codex rollout 新形式(v2)対応の調査記録(evidence)

移植元: `claude-conductor` の `worktree/fix-codex-rollout-v2`(コミット `0587cac`)。
`scripts/record-output.sh` の jq と `test.sh` セクション 26i1b が仕様の一次情報である。
実行環境は macOS 15 / `jq-1.7.1-apple` / Go 1.25。
Shell 版の挙動はすべて隔離サンドボックス(`env -i` で `HOME` と `CONDUCTOR_HOME` を
一時ディレクトリへ向けたもの)での実測に基づく。実環境のファイルには触れていない。

## 1. 採用規則(合算しない理由)

新旧は同じ活動の別表現である。合算すると同じターンや同じコマンド実行を 2 回
数えてしまうため、Shell 版はどちらか一方だけを採る。

| 値 | v1(旧) | v2(新) | 採用 |
| --- | --- | --- | --- |
| turns | `event_msg` の `payload.type=="user_message"` | `item_completed` の `item.type=="UserMessage"` | v2 が 1 件以上なら v2、0 件なら v1 |
| tool calls | `response_item` の `payload.type` が `_call$` | `item_completed` の `item.type` が CommandExecution / McpToolCall / FileChange / Extension | v1 が 1 件以上なら v1、0 件なら v2 |
| merged | `.input // .arguments` | `.command` と `.arguments` のみ | 両方を OR で走査 |
| model / usage | `turn_context` / 最後の `token_count` | 同左(変化なし) | - |

turns と tools で優先する向きが逆になっているのは Shell 側の実測に合わせたもので、
理由も向きごとに違う。

- turns は v2 を優先する。新形式の rollout には `user_message` イベントが 1 つも
  無いため、v1 が 0 になるのは「旧イベントが無い」ことの証拠になる
- tools は v1 を優先する。実機の v2 rollout は `response_item` の `_call` と
  `item_completed` の両方を持つことがあり、そのときは従来と同じ数え方(ツール名も
  従来どおり `exec` などの実名)になってほしい。item ビューは
  `response_item` を 1 つも持たない rollout のための受け皿である

Go 側の対応は `internal/domain/transcript_codex.go`。`ParseCodexTranscript` が
両ビューを集め、読み終えてから採用する側を決める(`CodexTranscript.Tools` が採用ビュー、
`CodexTranscript.ItemTools` が item ビュー)。採用されなかった item ビューを捨てないのは、
merged 判定だけが両ビューを見るためである。

## 2. merged マーカーが item の stdout を見ない理由

`CommandExecution` item は実行したコマンド(`command`)だけでなく `stdout` と
`aggregated_output` も持つ。item 全体を走査すると、`gh pr merge` と書かれた
ファイルを `cat` しただけのタスクが merged と判定される。conductor 側で実データを
確認済みで、`test.sh` セクション 26i1b の「merged marker ignores command output」が
この回帰を固定している。

Go 側は `CodexToolCall.Input` に「実行したコマンドだけ」を入れることで同じ制限を
表現している(item ビューでは `command` の join と `arguments` のみ)。
`internal/domain/transcript_codex_test.go` の `TestCodexMarkersIgnoresCommandOutput` が
stdout に `gh pr merge` を含む item で false になることを固定する。

## 3. jq のエッジケース実測(Go 版の分岐の根拠)

item ビューの merged 走査は次の式である。

```jq
(((.command // []) | join(" ")) + " " + ((.arguments // "") | tostring))
```

`jq` に直接与えて確かめた結果:

```console
$ echo '{"command":"ls -l"}' | jq -c '((.command // []) | join(" "))'
jq: error (at <stdin>:1): Cannot iterate over string ("ls -l")

$ echo '{"command":["a",1,null,true]}' | jq -c '((.command // []) | join(" ")) + " " + ((.arguments // "") | tostring)'
"a 1  true "

$ echo '{"command":[{"x":1}]}' | jq -c '((.command // []) | join(" "))'
jq: error (at <stdin>:1): string ("") and object ({"x":1}) cannot be added

$ echo '{"arguments":{"code":"x"}}' | jq -c '((.command // []) | join(" ")) + " " + ((.arguments // "") | tostring)'
" {\"code\":\"x\"}"

$ echo '{"command":null,"arguments":false}' | jq -c '((.command // []) | join(" ")) + " " + ((.arguments // "") | tostring)'
" "
```

分かったこと:

- `command` が配列でなければ join が反復に失敗して jq 全体が落ちる。
  `null` と `false` だけは `// []` で空配列に落ちるので落ちない
- join は `null` を空文字に、数値と真偽値を表記に変える。オブジェクトと配列は
  連結できずに落ちる
- `arguments` は `tostring` なので、オブジェクトは compact JSON の文字列になる。
  `false` は `// ""` で空文字になる
- 連結の区切りに必ず 1 個の空白が入るため、`command` も `arguments` も無い item の
  走査対象は `" "`(空白 1 個)になる

`item` そのものへの添字も確かめた。

```console
$ echo '{"type":"event_msg","payload":{"type":"item_completed","item":5}}' \
    | jq -sc '[.[] | select(.type=="event_msg" and .payload.type=="item_completed" and .payload.item.type=="UserMessage")] | length'
jq: error (at <stdin>:1): Cannot index number with string "type"

$ echo '{"type":"event_msg","payload":{"type":"item_completed","item":["a"]}}' | jq -sc '...同上...'
jq: error (at <stdin>:1): Cannot index array with string "type"

$ echo '{"type":"event_msg","payload":{"type":"item_completed","item":null}}' | jq -sc '...同上...'
0
```

`item` がオブジェクトでも null でもなければ jq が落ち、レコード全体が Parse failed に
なる。Go 側は `codexItemOf` がこの条件で `ok=false` を返し、同じくフォールバック
レコードへ落とす。`item` が null や欠落なら、単に「どの型にも一致しない」だけで
エラーにはならない。

## 4. 現行仕様との意図的な差異(1 件)

item の `name` が文字列でない場合、jq は `.name // .type` の結果をそのまま
`unique` に流すので `tools_used` に数値が入った summary が出る。Go 版は
`ToolsUsed` を `[]string` で表すため同じ JSON を作れず、値の型を偽らずに
フォールバック(summary: null)へ落とす。これは呼び出しビューに対して既にある
差異(`TestParseCodexTranscriptRejectsNonStringToolName`)を item ビューへ広げた
ものである。実在の rollout の item には `name` フィールド自体が無い
(だからこそ jq の `.name // .type` が type 側に落ちてツール名が item の型になる)。

## 5. ゴールデンによる Shell 版との一致確認

`scripts/gen-golden-record.sh` に conductor worktree
(`/Users/kazuto/projects/claude-conductor/.worktree/fix-codex-rollout-v2`)を渡し、
新形式の 3 ケースを追加した。生成は隔離サンドボックスで行われ、実環境の
`~/.claude-conductor` や `~/.codex` には書き込んでいない。

追加したケースと、Shell 版が実際に書いた daily の中身:

| ケース | turns | tool calls | tools_used | merged |
| --- | --- | --- | --- | --- |
| codex-v2-item-completed | 2 | 3 | CommandExecution, McpToolCall | true |
| codex-v2-call-view-preferred | 2 | 1 | exec | true |
| codex-v2-command-output-not-merged | 1 | 1 | CommandExecution | false |

`codex-v2-call-view-preferred` は新形式の rollout に `response_item` の
`custom_tool_call` を 1 件足したもので、item ビューの 3 件ではなく呼び出しビューの
1 件が採られること(二重計上しないこと)を固定する。
`codex-v2-command-output-not-merged` は stdout に `gh pr merge` を含む
`cat` の item だけを持ち、merged が false のままであることを固定する。

### 既存ケースの不変確認

生成は全 35 ケースを上書きするため、既存 31 ケース(daily を 1 行も書かない
`no-pending` を除く)について、旧 fixture(`git show HEAD:...`)と新 fixture を
`completed_at` を除いた JSON として比較した。**内容差分 0 件**。
差が出るのは `completed_at` と、それに連動する daily のファイル名の日付
(`2026-08-09.jsonl` → `2026-08-10.jsonl`)だけだったので、既存ケースは
`git checkout` で元へ戻し、新規 3 ケースだけを追加した。

```console
$ go test ./internal/infra/store/ -run GoldenRecord -v 2>&1 | grep -c '^    --- PASS'
35
$ make check
...
Total test coverage: 90.8% (1734/1909)
```
