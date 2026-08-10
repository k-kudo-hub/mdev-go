# record-output の daily 追記を冪等化(置換)する

## 概要

claude-conductor 側で確定した「再試行時の daily 追記の置換セマンティクス」を mdev-go の
`RecordOutput` / daily ストアへ移植する。

アップロード失敗で `dd`(タスク削除)が中止されると `record-output` は同じ pending に対して
何度も走る。現状は走るたびに 1 行増えるため、Done ペインに同じタスクのエントリが増殖する。

## 確定仕様(一次情報: claude-conductor の scripts/record-output.sh 末尾 + test.sh セクション 26i2)

- **対象**: 当日の daily ファイルのみ(追記先と同じファイル)
- **dedupe キー**: `tab` + `claude_session_id` の完全一致。ただし `claude_session_id` が
  **空、または `screen-` で始まる場合はキーとして使わず無条件追記**(従来動作)。
  スクリーン検出の合成 ID `screen-<slug>` はタブ名の純関数であり、同名タブの別タスクの
  履歴まで一致してしまうため(conductor 側 code-review で確定)
- **対象条件**: `restored != true` の行のみ削除対象(`restored: true` は復元済みの履歴として残す)
- **置換位置**: 一致行を**すべて削除**してから**末尾へ追記**(削除されなかった行の相対順序は不変)
- **排他**: 既存 lock(2 秒 fail-open)を取れたときだけ削除フィルタ →
  同一ディレクトリ temp → rename → 追記。**ロックを取れなかった場合は削除をあきらめて
  追記のみ**(非ロックの全体書き換えは並行 restore の `restored: true` を巻き戻すため)
- **フェイルセーフ**: 既存 daily の解析に失敗したら削除せずそのまま追記(重複は回復できるが
  切り詰めは回復できない)

## TODO

### 契約とテスト(TDD: 赤を確認してから実装)

- [x] app: `DailyAppender` の契約コメントを「追記」から「置換つき追記」へ更新し、
      `RecordOutput` の doc コメントに dedupe キーを持たせる責務を書く
- [x] app: `RecordOutput` が dedupe キー(tab / claude_session_id)を載せたレコードを
      Append へ渡すことのテスト
- [x] store: 同一 (tab, claude_session_id) の 2 回追記で 1 行のまま内容が更新されるテスト
- [x] store: 別 `claude_session_id` は別行として増えるテスト
- [x] store: `restored: true` の行は削除対象外であるテスト
- [x] store: `claude_session_id` が空なら削除せず追記するテスト
- [x] store: 未変更行の相対順序が保たれ、置換行が末尾に来るテスト
- [x] store: 壊れた daily(JSON として読めない行を含む)ではフィルタせず追記するテスト
- [x] store: 置換後にロックが残らないテスト
- [x] `DailyStore.Append` に置換セマンティクスを実装(`writeFileAtomic` を使用)

### 仕様変更(conductor 側 code-review で確定)への追随

- [x] domain: `DailyRecord.HasDedupeKey`(空 / `screen-` 前置きはキーにしない)のテスト → 実装
- [x] store: `screen-` の sid で 2 回実行すると 2 行になるテスト
- [x] store: ロックを取れないとき(fail-open)は削除フィルタを飛ばして追記のみ行うテスト
- [ ] conductor 側の確定連絡を受けてゴールデンを再生成し、`screen-` sid のケース追加を検討

### ゴールデン(現行 Shell 版との一致)

- [x] `cases.json` に実行回数を表現する項目を足し、`scripts/gen-golden-record.sh` を対応させる
- [x] 再試行置換のゴールデンケースを追加(同一 pending で record を 2 回走らせ、期待 daily が
      1 行になる)
- [x] `gen-golden-record.sh` を conductor worktree のパスで実行し fixture 再生成。
      変更のない既存 fixture は `git checkout` で戻す(completed_at 変化のノイズ回避)
- [x] ゴールデンテスト全件通過を確認

### 仕上げ

- [ ] 各コミット前に `make check` 緑
- [ ] evidence(`.claude/todo/20260810-record-dedupe-evidence.md`)に判断の根拠を記録
