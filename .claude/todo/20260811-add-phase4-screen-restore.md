# フェーズ4: スクリーン検出 + セッション/タスク復元の Go 化

## 概要

codex の状態検出(`screen_detect_tick`)と復元(`restore-session.sh` / `restore-task.sh`)を Go 内製化し、ShellRunner の該当 3 メソッドを撤去する。
状態機械は ADR-0002 の設計通り「観測 + 前回状態 + 現在時刻 → 次状態 + 副作用リスト」の domain 純粋関数にする。

## 調査で確定した要点(仕様書 = 調査報告全文が一次資料)

- **screen_classify**: 空行除去 → tail 20 の窓、neutral > blocked > working > idle、パターン主導のマッチ行抽出(blocked)、不正 regex は不一致扱い、POSIX ERE(行ごとループで grep と一致)
- **状態機械**: neutral は完全 no-op(状態ファイルすら書かない)/ 状態書き込みは Waiting ガードより**前**(Waiting 中も内部状態は進む)/ idle は実時間 1 秒(整数 epoch 秒差)の idle_pending 経由で確定 / working の Main 帰還は prev ∈ {blocked, idle} のみ(初回・idle_pending は除外)/ Stop 確定は「タブに pending が 1 件も無い」が条件 / blocked は既存 Notification の時刻保持
- **競合規則**: 調査 §6.4 の完全表(notify Stop への譲歩・収束、Waiting 保護)
- **restore-session**: registry 1 件ずつ検証 → LatestPerTab(移植済み)→ 既存タブスキップ → dir 検証と削除 → resume 3 条件 → rc 0/3 は成功カウント → Main 帰還 → 常に成功
- **restore-task**: (tab, completed_at, restored!=true) の最初の 1 件、exit 0-5 の契約、lock + read-modify-rewrite で restored:true
- **発見バグ**: screen 由来 sid(`screen-<slug>`)が restore-task の resume 判定を通過し `codex resume screen-...` が実行される → Go 版では resume 判定に「sid が `screen-` 前置でない」を追加(修正・evidence 記録)

## TODO

### domain(状態機械の純粋関数化)

- [x] `ScreenTailWindow`(空行除去 → tail N)と `ClassifyScreen`(優先順位・パターン主導 blocked 行抽出・不正 regex 無視。regexp.CompilePOSIX)のテスト → 実装(test.sh の分類 13 ケースをテーブル移植)
- [x] `.screen-state` の 1 行形式(`state [epoch]`)のパース/フォーマットのテスト → 実装(非数値 epoch は即確定側に倒す)
- [x] `DecideScreen`(観測 + 前回 + pending スナップショット + epoch 秒 → 副作用リスト)のテスト → 実装。遷移表(調査 §2.5)全行 + ライフサイクルケース(test.sh 1240-1466)をテーブル移植。**neutral = 空リスト / 状態書き込みが常に先頭 / Waiting 時は状態書き込みのみ** を固定
- [x] `domain.AgentConfig` に `Patterns`(neutral/blocked/working)を追加(Config の unmarshalTaskKeys へ配線、per-entry 許容の流儀維持)

### app / infra(検出の内製化)

- [x] port 追加のテスト → 実装: `PaneLister`(list-panes -t -c -j の JSON 解析: is_plugin=false・terminal_command の `TASK_AGENT=([^ ]+)` 抽出)/ `ScreenDumper`(dump-screen -p terminal_<id>、失敗は空)/ ScreenState の Read/Write 拡張 / registry の **mtime 最新** lookup(`_screen_registry_lookup` 互換。updated_at ではない — restore との非対称は現行仕様として維持・記録)
- [x] `ScreenDetector` ユースケースのテスト → 実装(ペイン列挙 → detection==screen のみ → dump → classify → DecideScreen → 副作用実行。pending 借用 3 キー(dir/task_type/transcript)含む)。`DashboardPane` の Shell ラッパ呼び出しを差し替え(HasScreenDetectionAgent ゲートと fail-open は維持)

### app / infra(復元の内製化)

- [x] `SessionRestorer` のテスト → 実装(既存 CreateTask を再利用。ErrTabNotRegistered / ErrFocusNotConfirmed = rc=3 相当は成功カウント + 警告。QueryTabNames・Main 帰還はガード付き = Shell 版の未ガードからの自動改善として記録)。`DashboardPane.Startup` を差し替え
- [x] `TaskRestorer` のテスト → 実装(exit 0-5 相当の sentinel エラー、daily の `MarkRestored`(lock 2 秒 fail-open + 同一ディレクトリ temp)、resume 3 条件 + **screen- 前置 sid の除外**)。`DonePane.Restore` を差し替え(戻り値は現行同様无視から開始)
- [x] ShellRunner から ScreenDetectTick / RestoreSession / RestoreTask を削除(runner・port・fake の掃除)

### 互換検証

- [ ] ゴールデン: restore-task 相当(stub zellij + fixture daily/registry で Shell を隔離実行し、daily の restored マーキングと zellij 呼び出し列を比較)。screen 検出は domain テーブルテストが一次(test.sh のケース網羅)+ 実 rollout ではなく fixture ダンプでの分類一致確認
- [ ] 既存ゴールデン 101 ケース不変・`make check` 緑
- [ ] ユーザーテスト手順書 `docs/user-test-04-screen-restore.md`(codex タスクの blocked/done 検出、再起動復元(mdev 再起動で --resume が効く)、Done の r 復元、チェックリストと復元手順)

## 完了条件

- codex タスクの検出(blocked 即時 / done は 1 秒遅延確定 / neutral 無視 / Waiting 保護)が Shell 版と同一のファイル状態遷移を生む(遷移表全行をテストで固定)
- 復元 2 経路が Shell 版と同一の判定(rc 契約・resume 3 条件)で動き、screen- sid バグは Go 側で修正済み
- ShellRunner の 3 メソッドが消え、毎 tick の bash/jq プロセス起動がゼロになる
- `make check` 緑・手順書完成(ユーザーテスト 04 はマージ後)

## 備考

- **ユーザーテスト契機**: 完了時 = ユーザーテスト 04(codex 検出と復元の実地確認)
- 意図的な差異(evidence 記録): idle 確定は整数 epoch 秒差のまま(パリティ優先)/ restore 系 zellij 呼び出しにガードが付く(改善)/ screen- sid の resume 除外(バグ修正)。conductor 側の同バグは別途小 PR 候補として記録
- mtime vs updated_at の選択キー非対称は現行仕様どおり各経路で維持
