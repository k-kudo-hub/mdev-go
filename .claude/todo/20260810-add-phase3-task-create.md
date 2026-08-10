# フェーズ3: タスク作成ペイン + task-control の Go 化

## 概要

TaskCreate ペイン(`n` フロー)と task-control ペイン(m / w / dd)を `mdev pane task-create` / `mdev pane task-control <tab>` として移植し、`create_task` / `apply_layout` / `waiting-toggle` を Go 化する。
zellij 駆動には確立済みの防御(登録待ち・stdout 検証フォーカス・予算・プロセスグループ kill)を最初から組み込む。

## 主要な設計判断(レビューで確認したい点)

1. **fzf / fd を Go 内製で置換**(外部依存 2 つを削減):
   - ディレクトリ探索: `search_depth=1` 運用ではドット始まりディレクトリ除外だけで実用十分(fd の .gitignore 解釈は深さ 1 では効かない)。内製 WalkDir + ドット除外
   - 選択 UI: 自前の簡易フィルタリスト(部分列マッチ = fzf の基本動作相当、↑↓ / Ctrl-P/N / 入力で絞り込み / Enter / ESC)。**UX 変更点**: alt-screen を使わずペイン内描画、`FZF_DEFAULT_OPTS` は反映されなくなる。depguard の制約(外部 fuzzy ライブラリ不可)に適合
2. **restore-\*.sh は変更しない**: Shell の create_task を呼び続ける(conductor はバグ修正のみ枠)。Go の n フローは Go 版 create_task を使う。両実装はデータ互換で併存し、フェーズ 4(restore Go 化)で一本化
3. **task-control の dd は既存の PrepareDelete / CommitDelete に統合**: Dashboard との非対称のうち `close-tab` フォールバックを CommitDelete に追加し、`.screen-state` 削除は統合により付与(Shell 版の欠落バグの修正 = 改善方向の差異として evidence 記録)
4. **意図的な改善(evidence 記録)**: タスク名入力は Go の textinput 相当でプリフィル編集可(bash4 体験に統一)/ create_task 失敗時にエラーを 2 秒表示(Shell は無言)/ 矢印キーのチラつき解消 / task-create にも Ctrl-C 終了

## TODO

### domain

- [x] Config 拡張のテスト → 実装(`search_dirs` / `search_depth`(既定 1)/ `skip_task_name_input` / `task_types`(description + layout steps)/ `agents.command`・`resume_args`(word-split は strings.Fields 相当))
- [x] デフォルト名・名前解決のテスト → 実装(`generate_default_name` = basename+type / `resolve_name` = 空なら default。末尾スラッシュの basename 挙動含む)
- [x] 選択リストのフィルタ(部分列マッチ)のテスト → 実装(純粋関数)
- [x] waiting-toggle の event 遷移のテスト → 実装(prev_event 退避 / 復元(欠落時 Notification)/ time 更新。純粋変換)
- [ ] task-control バー描画のテスト → 実装(通常 / WAITING、ANSI は Shell と同一文字列。ONCE 互換)

### app / infra(zellij 駆動の防御込み)

- [ ] TabController 拡張のテスト → 実装(`QueryTabNames` / `FocusTabVerified`(stdout 非空 = 成功)/ `NewTab` / `NewPane` / `Resize` / `MoveFocus` / `FocusPreviousPane` / `CloseActiveTab`。全て proc.Command + 10 秒上限)
- [ ] `CreateTask` ユースケースのテスト → 実装(Shell 版 v0.7.4 と同一シーケンス: screen-state 削除 → new-tab → 登録ポーリング待ち → 検証フォーカス + リトライ → 失敗なら pane 構築せず rc=3 相当のエラー種別 → task-control ペイン(予算切れでも最低 1 秒)→ resize×30 → focus-previous-pane → apply_layout(rc 無視)。全体予算 30 秒、途中打ち切りは成功扱い + 警告)
- [ ] `ApplyLayout` のテスト → 実装(new-pane / move-focus / focus-previous-pane / resize(amount 回)、command 省略時の分岐)
- [ ] `ToggleWaiting` ユースケースのテスト → 実装(FindByTab 最初の 1 件 / 無ければ no-op / 原子書き込み)
- [ ] `UniqueTaskName` の existing 取得を QueryTabNames で結線(zellij 失敗時は base をそのまま = Shell 互換)
- [ ] task-control 用の削除ユースケース統合のテスト → 実装(CommitDelete に close-tab フォールバック追加。2 打鍵タイムアウトは 2 秒の別定数)

### tui

- [ ] task-create Model のテスト → 実装(メニュー(キー待ちのみ・ポーリング無し)→ dir 選択 → type 選択(記述順)→ agent 選択(0 件スキップ / 1 件即決 / 複数で選択)→ 名前入力(プリフィル・skip 設定対応)→ 一意化 → 作成実行(進行表示 + 失敗表示)→ 新タブへ遷移。各ステップの ESC キャンセルでメニューへ)
- [ ] task-control Model のテスト → 実装(2 秒ポーリングで WAITING 追従 / m / w / d+d(2 秒)/ dd の削除フロー(upload 失敗で何も消さない契約・URL 2 秒表示)/ 削除完了で終了)
- [ ] `--once` 対応(task-control は Shell の ONCE と出力一致)

### cli / 互換検証

- [ ] `mdev pane task-create` / `mdev pane task-control <tab>` を登録
- [ ] ゴールデン: task-control バーの ONCE 出力を Shell と比較(通常 / WAITING)。waiting-toggle は同一 pending 入力での出力ファイル比較(Shell を隔離実行)
- [ ] ユーザーテスト手順書 `docs/user-test-03-task-create.md`(multi.kdl の TaskCreate 差し替え、n フロー全経路・キャンセル・w・dd のチェックリスト、復元手順)
- [ ] `make check` 緑

## 完了条件

- n フローで作成したタスクが Shell 版と同一の環境変数・タブ構成・レイアウトになる(dev / k8s / レイアウト無しの 3 パターン)
- dd / w が Shell 版 task-control と同一のファイル状態遷移を生む(ゴールデン + ユニット)
- 劣化サーバでも各操作が予算内で脱出する(既存防御の適用をテストで固定)
- `make check` 緑・手順書完成(ユーザーテスト 03 はマージ後)

## 備考

- **ユーザーテスト契機**: 完了時 = ユーザーテスト 03(タスク作成フローの実地確認)
- fd / fzf への依存が Go ペイン経路から消える(Shell 版は現状維持)
- Dashboard の「スペース入りタブ名が表示されない」既知バグは本タスクでは触らない(表示系の互換維持)
- エッジケース表(調査 §8 の 28 項目)を実装時の一次チェックリストとして使用
