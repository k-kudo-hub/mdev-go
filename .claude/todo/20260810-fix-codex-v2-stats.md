# codex rollout 新形式(v2 / item_completed)の統計を Go 版へ移植する

移植元: `claude-conductor` の `worktree/fix-codex-rollout-v2` ブランチ
(`scripts/record-output.sh` と `test.sh` セクション 26i1b、コミット `0587cac`)。

新しい codex は会話とツール実行を `event_msg` / `payload.type=="item_completed"` の
`item` として記録し、旧 `user_message` イベントを出さなくなった。Go 版の
`ParseCodexTranscript` は旧形式しか見ていないため、新形式の rollout では
`total_turns` が常に 0 になり、ツール呼び出しも 0 件になる。

## 確定仕様(Shell 側で確定済み)

- turns: v2(`item.type=="UserMessage"` の件数)が 1 以上ならそれを採用、0 なら v1
  (`payload.type=="user_message"` の件数)。**合算しない**(同じターンの別表現)
- tool calls / tools_used: v1(`response_item` の `payload.type` が `_call$`)が
  1 件以上ならそれを採用、0 件なら v2(`item.type` が CommandExecution /
  McpToolCall / FileChange / Extension)。名前は `.name // .type`
- merged マーカー: 両ビューを OR 走査。v2 側の走査対象は
  `((.command // []) | join(" ")) + " " + ((.arguments // "") | tostring)` のみ。
  stdout / aggregated_output は見ない(cat しただけで誤検知する実データがある)
- model(turn_context)と usage(最後の token_count)は不変

## タスク

- [x] 1. Shell 版 jq の新規部分のエッジケースを実測して仕様を固める(evidence)
- [x] 2. 赤テスト: v2 の turns / tools / tools_used / merged と、優先規則の回帰を書く
- [x] 3. `transcript_codex.go` を新形式対応にする(v2 ビューの保持と採用規則)
- [x] 4. `CodexMarkers` を両ビュー走査へ変え、`daily_build.go` の呼び出しを直す
- [x] 5. `make check` を通す
- [x] 6. ゴールデン: v2 の case を `cases.json` に足し、conductor worktree の Shell から生成
- [x] 7. 既存ゴールデンが不変であることを確認する
- [x] 8. evidence をまとめる

## 追記: conductor の code-review 対応(fe4b184)への追随

仕様の正本が `scripts/codex-rollout-lib.sh` に集約され、集計精度が上がった。
ツール集計の pick-one はそのまま(カテゴリ別 union も二重計上になることが
実 rollout の調査で確定したため)。

- [x] 9. tools_used の命名を `.name // .tool // .kind // .type` にする
      (McpToolCall は `.tool`、Extension は `.kind`)
- [x] 10. merged 判定を「起動したもの」だけに絞る。v1 は `.name` の
      `merge_pull_request` 一致を追加、v2 は CommandExecution の command と
      McpToolCall の `.tool` のみ(引数本文と出力は走査しない)
- [x] 11. `.command` の型ガード(配列以外でも落とさず tostring する)
- [x] 12. テストとゴールデンを fe4b184 で更新・追加(McpToolCall のマージ、
      文字列 command、FileChange / Extension の命名)

## 完了条件

- `make check` が緑
- ゴールデンテストが全件通過し、既存 fixture に差分が無い
