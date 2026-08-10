# codex rollout 新形式(v2)対応の調査記録(evidence)

移植元: `claude-conductor` の `worktree/fix-codex-rollout-v2`。仕様の正本は
`scripts/codex-rollout-lib.sh`(HEAD `fe4b184`)で、`record-output.sh` と
`upload-log.sh` が共用する jq 定義がすべての判断を持っている。期待値は
`test.sh` セクション 26i1b。

実行環境は macOS 15 / `jq-1.7.1-apple` / Go 1.25。Shell 版の挙動はすべて隔離
サンドボックス(`env -i` で `HOME` と `CONDUCTOR_HOME` を一時ディレクトリへ
向けたもの)での実測に基づく。実環境のファイルには触れていない。

## 1. 採用規則(合算しない理由)

新旧は同じ活動の別表現である。合算すると同じターンや同じコマンド実行を 2 回
数えてしまうため、Shell 版はどちらか一方だけを採る。

| 値 | v1(旧) | v2(新) | 採用 |
| --- | --- | --- | --- |
| turns | `event_msg` の `payload.type=="user_message"` | `item_completed` の `item.type=="UserMessage"` | v2 が 1 件以上なら v2、0 件なら v1 |
| tool calls | `response_item` の `payload.type` が `_call$` | `item_completed` の `item.type` が CommandExecution / McpToolCall / FileChange / Extension | v1 が 1 件以上なら v1、0 件なら v2 |
| 表示名 | `.name // .tool // .kind // .type` | 同左 | 採用ビューに対してのみ計算 |
| merged | `.input // .arguments` と `.name` | CommandExecution の command と McpToolCall の `.tool` | 両方を OR で走査 |
| model / usage | `turn_context` / 最後の `token_count` | 同左(変化なし) | - |

turns と tools で優先する向きが逆になっているのは Shell 側の実測に合わせたもので、
理由も向きごとに違う。

- turns は v2 を優先する。新形式の rollout には `user_message` イベントが 1 つも
  無いため、v1 が 0 になるのは「旧イベントが無い」ことの証拠になる
- tools は v1 を優先する。lib のコメントにある実測(08/10 の rollout)では、
  `custom_tool_call`(name="exec")9 件の input が tools.web__run 4 / exec_command 1 /
  内省 1 / mcp__node 1 / exec_command 2 と分かれ、それぞれ Extension /
  CommandExecution / McpToolCall item として描画されていた。つまり両ビューは
  同じ活動を別の側から見たもので、カテゴリ別の union であっても二重計上になる。
  相関キーも無い(v1 は `call_id "call_..."` と `id "ctc_..."`、v2 は
  `id "exec-<uuid>"`)。item ビューは `response_item` を 1 つも持たない rollout
  (2026-08-07T22:17 の 0 対 8)のための受け皿である

Go 側の対応は `internal/domain/transcript_codex.go`。`ParseCodexTranscript` が
両ビューを集め、読み終えてから採用する側を決める(`CodexTranscript.Tools` が採用ビュー、
`CodexTranscript.ItemTools` が item ビュー)。採用されなかった item ビューを捨てないのは、
merged 判定だけが両ビューを見るためである。

## 2. merged 判定が走査するもの / 走査しないもの

走査するのは「何を起動したか」だけで、出力も引数の本文も見ない。

| 走査する | 走査しない |
| --- | --- |
| v1: `.input // .arguments`(`gh pr merge`) | CommandExecution の `stdout` / `aggregated_output` |
| v1: `.name`(`merge_pull_request`) | McpToolCall の `arguments` 本文 |
| v2: CommandExecution の command(`gh pr merge`) | FileChange / Extension の中身 |
| v2: McpToolCall の `.tool`(`merge_pull_request`) | |

除外の根拠はどちらも誤検知の実例である。

- `stdout`: `gh pr merge` と書かれたファイルを `cat` しただけの CommandExecution
  item が実データに存在し、item 全体を走査すると未マージのタスクが merged になる
- MCP の `arguments`: Slack 投稿がツール名やコマンドを引用しただけで立ってしまう。
  だから MCP のマージは引数ではなくツール名(`merge_pull_request`)で見分ける

Go 側は `CodexToolCall` に走査対象を 2 つ持たせてこの制限を型で表現している。
`MergeCommand` が `gh pr merge` を探す対象、`MergeTool` が `merge_pull_request` を
探す対象で、走査対象にならない値はそもそも入らない。`merge_pull_request` の照合が
部分一致なのは、呼び出しビューの `mcp__github__merge_pull_request` と item ビューの
`merge_pull_request` を 1 つの式で拾うためである(claude 側の `ClaudeMarkers` は
完全一致で見ており、そちらは変えていない)。

## 3. jq のエッジケース実測(Go 版の分岐の根拠)

### 3.1 `codex_command_text`(command の型ガード)

```jq
if (.command | type) == "array" then (.command | map(tostring) | join(" "))
else ((.command // "") | tostring) end
```

```console
$ echo '{"command":["a",1,null,true,{"x":1}]}' | jq -c 'if (.command|type)=="array" then (.command|map(tostring)|join(" ")) else ((.command // "")|tostring) end'
"a 1 null true {\"x\":1}"

$ echo '{"command":"gh pr merge 12"}' | jq -c '...同上...'
"gh pr merge 12"

$ echo '{"command":{"a":1}}' | jq -c '...同上...'
"{\"a\":1}"

$ echo '{"command":false}' | jq -c '...同上...'
""

$ echo '{}' | jq -c '...同上...'
""
```

型で分岐しているのは、形の揺れで jq を落とさないためである。落ちると daily
レコードが丸ごと `summary: null` へ退避してしまう。**この型ガードは 0587cac
時点の実装(`join`)からの変更点で、挙動が 2 つ変わっている**。

- 配列以外の command(文字列やオブジェクト)は、以前は jq が
  「Cannot iterate over string」で落ちていたが、いまは tostring されて通る
- 配列要素の `null` は、`join` では空文字だったが `map(tostring)` では `"null"`

Go 側は `codexCommandText` がこの分岐をそのまま写している。`jsonToString` に
null の分岐を足したのは、Go の `json.Unmarshal` が JSON の null を文字列へ
入れてもエラーにせず空文字を残すためで、jq の `null | tostring` は `"null"` である。

### 3.2 `codex_tool_name`(表示名)

```console
$ echo '{"type":"McpToolCall","server":"node_repl","tool":"js"}' | jq -c '.name // .tool // .kind // .type'
"js"
$ echo '{"type":"Extension","kind":"web.search"}' | jq -c '.name // .tool // .kind // .type'
"web.search"
$ echo '{"type":"FileChange"}' | jq -c '.name // .tool // .kind // .type'
"FileChange"
```

実データの item は `McpToolCall={server,tool}` / `Extension={kind}` という形で、
どちらもツール名のフィールド名が違う。`.type` は最後の受け皿である。

### 3.3 `item` への添字

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

`codex_tool_name` が最初に拾う値(`.name` / `.tool` / `.kind`)が文字列でない場合、
jq はその値をそのまま `unique` に流すので `tools_used` に数値が入った summary が
出る。Go 版は `ToolsUsed` を `[]string` で表すため同じ JSON を作れず、値の型を
偽らずにフォールバック(`summary: null`)へ落とす。呼び出しビューに対して既にある
差異(`TestParseCodexTranscriptRejectsNonStringToolName`)を item ビューと
`.tool` / `.kind` へ広げたものである。実在の rollout ではどれも文字列である。

## 5. ゴールデンによる Shell 版との一致確認

`scripts/gen-golden-record.sh` に conductor worktree
(`/Users/kazuto/projects/claude-conductor/.worktree/fix-codex-rollout-v2`、HEAD `fe4b184`)
を渡して生成した。gen スクリプトは `$CONDUCTOR_SRC/scripts` を丸ごとサンドボックスへ
コピーするため、`record-output.sh` が source する `codex-rollout-lib.sh` も
そのまま使われる。生成は隔離サンドボックスで行われ、実環境の `~/.claude-conductor`
や `~/.codex` には書き込んでいない。

codex 新形式のケースと、Shell 版が実際に書いた daily の中身:

| ケース | turns | calls | tools_used | merged |
| --- | --- | --- | --- | --- |
| codex-v2-item-completed | 2 | 5 | CommandExecution, FileChange, js, web.search | true |
| codex-v2-call-view-preferred | 2 | 1 | exec | true |
| codex-v2-command-output-not-merged | 1 | 1 | CommandExecution | false |
| codex-v2-command-type-guard | 1 | 3 | CommandExecution | true |
| codex-v2-mcp-merge | 1 | 1 | merge_pull_request | true |
| codex-v2-mcp-arguments-not-merged | 1 | 1 | send_message | false |
| codex-v1-mcp-merge | 1 | 1 | mcp__github__merge_pull_request | true |

それぞれが固定している回帰:

- `codex-v2-item-completed`: item ビューの数え方と、`.tool` / `.kind` による命名
- `codex-v2-call-view-preferred`: 新形式の rollout に `response_item` を 1 件足した
  もの。item ビューの 5 件ではなく呼び出しビューの 1 件が採られる(二重計上しない)
- `codex-v2-command-output-not-merged`: stdout に `gh pr merge` を含む `cat` だけの item
- `codex-v2-command-type-guard`: 文字列の command / 配列内の object / command 無しが
  混ざっても `summary` が null にならず、文字列の command からマージを検出する
- `codex-v2-mcp-merge` と `codex-v2-mcp-arguments-not-merged`: MCP のマージは
  ツール名で拾い、引数に引用された `gh pr merge` では立たない
- `codex-v1-mcp-merge`: 呼び出しビューの `.name` でも MCP のマージを拾う

### 既存ケースの不変確認

生成は全ケースを上書きするため、既存 32 ケース(daily を 1 行も書かない
`no-pending` を除く)について、旧 fixture(`git show HEAD:...`)と新 fixture を
`completed_at` を除いた JSON として比較した。**内容差分 0 件**。
既存ケースは `git checkout` で元へ戻し、内容が変わる 2 ケースと新規 4 ケース
だけを更新・追加した。

```console
$ go test ./internal/infra/store/ -run GoldenRecord -v 2>&1 | grep -c '^    --- PASS'
39
$ make check
...
Total test coverage: 90.8% (1730/1906)
```
