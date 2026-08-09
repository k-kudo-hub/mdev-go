# 実環境組み込み: hooks を Go 版へ切り替える(最初のユーザーテストポイント)

## 概要

`~/.claude/settings.json` の hooks を Shell スクリプトから `mdev hook` サブコマンドへ安全に切り替える仕組みを作り、実環境で Go 版 hook を動かす。
現行の Shell 製ダッシュボードはデータ互換の pending を読むため、**hooks だけ Go に差し替えても全機能が動く**(ADR-0001 の段階移行)。これが Go 版が実環境で動く最初のユーザーテストとなる。

## 前提(確定済みの事実)

- 現行 hooks(`hooks.json` → install.sh が `~/.claude/settings.json` へマージ):
  - `Notification`: terminal-notifier のインラインコマンド + `pending-notify.sh`
  - `Stop`: 同上(文言違い) + `pending-notify.sh`
  - `PostToolUse`: `pending-post-tool.sh`
  - `UserPromptSubmit`: `pending-resolve.sh`
- 置き換え対象は **スクリプト呼び出しの 4 箇所のみ**。terminal-notifier のインラインコマンドは通知の責務で hook 処理と独立しており、今回は触らない
- `mdev hook notify` / `post-tool` / `resolve` は PR #2 でゴールデンテスト済み。`mdev record` も PR #3 で完了しているが、呼び出し元(task-control.sh / dashboard-loop.sh)が Shell のため**自動組み込みは今回スコープ外**(手動実行での確認のみ)

## TODO

- [x] domain: settings.json の hooks 書き換えロジックのテストを作成(4 コマンドの置換 / mdev 以外の hook・他キーの保全 / 冪等性 = 2 回実行しても同じ / 復元)→ 純粋関数(JSON in/out)として実装
- [x] app: `SwitchHooks` / `RestoreHooks` ユースケースのテストを作成(バックアップファイル作成 → 書き換え → 検証、バックアップからの復元)→ 実装
- [x] infra: settings ファイルストアのテストを作成(原子書き込み / バックアップ命名 `settings.json.mdev-backup-{timestamp}` / settings.json 不在時のエラー)→ 実装
- [ ] cli: `mdev hooks switch` / `mdev hooks restore` を実装(`--dry-run` で差分表示のみ。実行時は変更前後の hook 設定を表示)
- [ ] hook 失敗時の終了コード方針を決めて evidence に記録(PR #3 持ち越し: 現状 exit 1。Claude Code の hook 仕様では 0/2 以外は非ブロッキングであることをドキュメントで確認して確定する)
- [ ] `make install` ターゲットを追加(`go build` して `~/.claude-conductor/bin/mdev` へ配置。hooks の書き換え先コマンドはこの絶対パスを使う)
- [ ] ユーザーテスト手順書を作成(`docs/user-test-01-hooks.md`: 切り替え → 確認項目チェックリスト → 問題時の復元手順)
- [ ] `make check` 緑を確認

## ユーザーテストの確認項目(手順書に含める)

1. `make install` + `mdev hooks switch` 後、通常のタスクフローが Shell 版ダッシュボードで動くこと:
   - 権限プロンプトで Dashboard に赤 ■ が出る(Notification)
   - 承認するとタブから Main へ自動で戻り、表示が消える(PostToolUse)
   - ターン完了で緑 ■ done が出る(Stop)
   - プロンプト送信で pending が消えて Main へ戻る(UserPromptSubmit)
   - `w` での Waiting 切り替えが Go 版 hook と共存して動く(waiting-toggle.sh は Shell のまま)
   - タブ削除で daily log に記録される(record-output.sh は Shell のまま)
2. `mdev record <tab>` の手動実行で daily log に Go 版のレコードが追記されること
3. `mdev hooks restore` で完全に元へ戻ること

## 完了条件

- `mdev hooks switch` → ユーザーテスト全項目 OK → `mdev hooks restore` の往復が安全に行える
- 切り替え・復元が settings.json の hooks 以外(permissions 等)を一切変更しない
- `make check` 緑

## 備考

- **ユーザーテストの契機**(記録済みの方針): この PR のマージ後、次タスクへ進む前にユーザーテストを実施する
- settings.json は Claude Code 全体の設定ファイルのため、書き換えは必ずバックアップ + 原子書き込みで行い、対象は `.hooks` の 4 イベントの該当コマンドに限定する
- claude-conductor リポジトリは変更しない(インストール済み環境の settings.json のみが対象)
