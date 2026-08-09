# フェーズ1後半: record-output(daily log・transcript 集計・pricing 計算)の移植

## 概要

タスクタブ削除時に作業サマリを daily log(`$CONDUCTOR_HOME/daily/{session}/{YYYY-MM-DD}.jsonl`)へ 1 行追記する `record-output.sh` を Go に移植する(`mdev record <tab>`)。
これでフェーズ 1(基盤)の移植対象が完了する。Done ペイン(フェーズ 2)と作業ログアップロード(フェーズ 5)がこのレコードを読む。

## 事前調査の結果(初回 Explore 仕様書 セクション 5 より・要点)

- **入力**: 引数 `<tab_name>`(空なら exit 0)。pending ディレクトリから `.tab` 一致の**最初の 1 件**(glob 辞書順)を採用し、無ければ**何も書かず exit 0**。pending は削除しない(削除は呼び出し側)
- **出力レコード**: tab / session / completed_at(`%Y-%m-%dT%H:%M:%S%z`)/ message / summary / markers / 任意フィールド(dir / task_type / claude_session_id / transcript_path / agent、空なら省略)
- **claude 集計**: total_turns(`.type=="user"` 数)、tool_calls / tools_used、model(最初の `.message.model`)、speed(最初の `.usage.speed`、既定 "standard")、トークン 5 種の総和、cost(小数第 6 位丸め)
- **pricing フォールバック**: `$pricing[$model]` → `$pricing["claude-sonnet-4-6"]` → ハードコード既定値。`speed=="fast"` は `fast_multiplier`(既定 6)を乗算
- **markers**: merged(`mcp__github__merge_pull_request` or Bash `gh pr merge` 正規表現)/ slack(`mcp__slack` 系)/ doc(Write・Edit の file_path が `.md|.mdx|.txt|.rst|.adoc`)
- **codex 集計**(`agent=="codex"`): event_msg の user_message 数、`_call$` の response_item、最後の token_count の total_token_usage(input - cached が実 input)、**価格未知なら cost は null**(claude と違いフォールバックしない)、markers.slack / doc は常に false、summary のキーが `cache_write_tokens`(claude は `cache_write_5m/1h`)
- **フォールバック 3 段**: 正常 → parse 失敗(`summary: null`、message 空なら "Parse failed")→ transcript 無し(`summary: null`、message 空なら "No summary available")
- **排他**: daily ファイルへの追記は mkdir ロック(タイムアウト 2 秒、fail-open)。ロック保持は追記 1 行のみ、transcript パースはロック外
- **restored フィールド**: `restore-task.sh` が後から `"restored": true` を付与する(今回は読み書きしないが、レコード型として未知フィールドを壊さないこと)
- **期待値の一次情報**: claude-conductor の `test.sh` セクション 20〜26i

## TODO

### domain(純粋ロジック)

- [x] daily レコード型のテストを作成(必須/任意フィールド、summary null 許容、未知フィールド保持は対象外であることの明記)→ 実装
- [x] claude transcript 集計のテストを作成(turns / tool_calls / tools_used / model / speed / トークン 5 種。test.sh の期待値を移植)→ 実装
- [x] pricing 解決と cost 計算のテストを作成(モデル別 → sonnet フォールバック → ハードコード既定、fast_multiplier、丸め)→ 実装
- [x] markers 判定のテストを作成(merged の正規表現 / slack / doc の拡張子)→ 実装
- [x] codex rollout 集計のテストを作成(user_message 数 / `_call$` / total_token_usage / 価格未知 cost null / キー名差異)→ 実装
- [x] フォールバック 3 段のテストを作成(parse 失敗 / transcript 無しの message 既定値)→ 実装

### app / infra / cli

- [x] app: `RecordOutput` ユースケースのテストを作成(tab 空で no-op / pending 不在で no-op / 最初の 1 件選択 / pending を削除しない)→ port(`PendingFinder` / `DailyAppender` / `TranscriptReader`)と共に実装
- [x] infra: pending の tab 検索(glob 辞書順で最初の 1 件)のテストを作成 → PendingStore を拡張
- [x] infra: daily ストアのテストを作成(append 1 行 / mkdir ロック 2 秒 fail-open / ディレクトリ作成)→ 既存 lock を使って実装
- [ ] cli: `mdev record <tab>` サブコマンドを実装(config から pricing 読み込み。壊れ config の扱いは現行挙動 = 空 pricing で続行に合わせるか evidence に記録して判断)
- [ ] ゴールデンテスト: 現行 record-output.sh を隔離環境で実行して fixture を生成(claude 正常 / fast / codex / parse 失敗 / transcript 無しの各ケース)し、Go 版出力と JSON 等価比較
- [ ] `make check` 緑を確認

## 完了条件

- 同じ pending / transcript / config に対し、現行 `record-output.sh` と同じ daily レコードを追記する(ゴールデンテストで証明)
- cost の数値が現行の jq 計算と一致する(丸め含む)
- `make check` 緑

## 備考

- PR #2 の持ち越し事項: config の壊れ JSON をエラーにした Go 版の判断は、本タスクで「現行挙動(空 pricing で続行)に合わせる」方向で再検討し、結論を evidence に記録する
- ユーザーテストの契機(前回指示): 本タスク完了時点でも Go 版単体のユーザー確認可能な機能はまだ成立しない(呼び出し側のダッシュボードが Shell 版のため)。**次タスクの「hooks.json 切り替え + `mdev record` への差し替え(実環境組み込み)」が最初のユーザーテストポイント**になる見込み
- `restored` の付与(restore-task.sh)はフェーズ 4 の範囲。今回は追記のみ
