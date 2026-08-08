# フェーズ1コア基盤: config / pending ストア / task registry / hook サブコマンド

## 概要

ADR-0001 フェーズ 1 のうち、hook イベント処理系(`pending-notify.sh` / `pending-post-tool.sh` / `pending-resolve.sh` / `registry-lib.sh` / `lock-lib.sh` / config 読み込み)を Go に移植する。
データ形式は現行 Shell 版と完全互換とし(ADR-0001)、hook だけを Go 版に差し替える段階移行を成立させる。

## 事前調査の結果(Explore による現行実装の仕様確定・要点)

- **pending**: `~/.claude-pending/{ZELLIJ_SESSION_NAME:-unknown}/{session_id}.json`。パスは `CONDUCTOR_HOME` 非依存で固定。全フィールド string 型。`transcript_path` / `dir` / `task_type` / `prev_event` は空ならキー省略
- **event の値**: `Notification` / `Stop` / `Waiting` / `unknown`(hook_event_name 欠落時)
- **上書き規則**: Notification は無条件上書き(Waiting も潰す)。Stop は既存が Notification / Waiting なら書かない(`pending-notify.sh:43-48`)
- **registry**: `$CONDUCTOR_HOME/tasks/{session}/{sid}.json`。upsert は `ZELLIJ_SESSION_NAME` と `TASK_TAB_NAME` が**両方非空のときだけ**(これがタスクタブ判定)。`updated_at` は `%Y-%m-%dT%H:%M:%S%z`。mktemp+mv で原子書き込み、jq 失敗時は既存保持
- **tab 名фォールバック**: `TASK_TAB_NAME` → `basename(.cwd)` → `"unknown"`(notify のみ。resolve は `TASK_TAB_NAME` 直接)
- **PostToolUse**: `session_id` のみ使用。既存 pending の event が `Notification` のときだけ削除 → Main へフォーカス。registry には触れない
- **UserPromptSubmit**: pending を event 問わず無条件削除(Waiting 解除の実体)→ registry upsert → pending が無くても Main へフォーカス
- **壊れ/空 JSON**: jq が空文字に潰すため「event 空」として扱われる(Stop に上書きされる / post-tool では削除されない)。registry 読みは 1 ファイルずつ検証
- **ロック**(`lock-lib.sh`): mkdir ベース + pid ファイル。stale は `kill -0` 判定 → `mv` 退避後削除。実利用タイムアウト 2 秒、タイムアウト時は**警告して続行(fail-open)**
- **config**: 実行時マージなし。`config.json` があればそれ、無ければ `config.default.json`(ファイル単位フォールバック)。本サブシステムが読むのは `.pricing` のみだが、以後のフェーズ共通のローダとして実装
- **Zellij 副作用**: `zellij action go-to-tab-name "Main"`(post-tool / resolve、`ZELLIJ_SESSION_NAME` 非空時のみ)

## TODO

### domain(純粋ロジック)

- [x] pending レコード型のテストを作成(全 string / 空フィールドのキー省略 / JSON round-trip)→ 実装
- [x] event 上書き規則のテストを作成(Notification 無条件上書き / Stop は Notification・Waiting を潰さない / 壊れ JSON = event 空は上書きされる)→ 純粋関数として実装
- [x] tab 名フォールバック連鎖のテストを作成(`TASK_TAB_NAME` → `basename(cwd)` → `"unknown"`、空 cwd の挙動含む)→ 実装
- [x] registry レコード型と「タブごと `updated_at` 最新 1 件選択」のテストを作成 → 実装

### app(ユースケースと port)

- [x] port を定義(`PendingStore` / `RegistryStore` / `Focuser`(Zellij)/ `Clock`)し、fake 実装をテスト用に用意
- [ ] `HandleNotify`(pending-notify.sh 相当)のテストを作成(session_id 空で no-op / registry の AND ガード / 上書き規則の適用)→ 実装
- [ ] `HandlePostTool` のテストを作成(Notification のみ削除 / Main フォーカス / registry 不触)→ 実装
- [ ] `HandleResolve` のテストを作成(無条件削除 / upsert / pending 無しでもフォーカス)→ 実装

### infra(adapter)

- [ ] pending ファイルストアのテストを作成(パス規約 / 同一ディレクトリ temp+rename での原子書き込み / 壊れ JSON を「event 空」として返す)→ 実装
- [ ] registry ファイルストアのテストを作成(パス規約 / 原子書き込み / 壊れファイルを 1 件単位でスキップ)→ 実装
- [ ] config ローダのテストを作成(`config.json` → `config.default.json` のファイル単位フォールバック / `.pricing` の型)→ 実装
- [ ] mkdir ロックのテストを作成(獲得・解放 / stale 検出 / タイムアウト 2 秒 / fail-open)→ 実装
- [ ] Zellij Focuser adapter を実装(`zellij action go-to-tab-name`、エラーは無視 = 現行挙動)

### cli(結線)

- [ ] `mdev hook notify` / `mdev hook post-tool` / `mdev hook resolve` サブコマンドを cobra で実装(stdin JSON → app 呼び出し。テストは cli 薄皮の引数処理のみ)

### 互換性検証

- [ ] ゴールデンテスト: 現行 Shell 版に同じ stdin / 環境変数を与えて生成させた pending / registry 実ファイルを fixture 化し、Go 版の出力と JSON 等価(フィールド集合と値)で比較
- [ ] `make check` を実行し、カバレッジ層別閾値(domain / app 90%)を含む全ガードレールが緑であることを確認

## 完了条件

- 現行 hooks.json の 3 スクリプトと同じ入力(stdin JSON + 環境変数)に対し、`mdev hook` サブコマンドが同じファイル状態・同じ Zellij 副作用を生じる
- ゴールデンテストが Shell 版出力との互換を証明している
- `make check` 緑(domain / app の 90% 閾値がこの PR から実質有効になる)

## 備考

- **含めない**(フェーズ 1 の残りとして次タスク): `record-output.sh`(daily log・transcript 集計・pricing 計算)、`codex-notify.sh`、screen 検出、hooks.json の実切り替え(インストーラ連携)
- 壊れ JSON の扱いは Shell 版と同一挙動を再現する(並行運用中の互換性のため)。改善(厳格化)は移行完了後に ADR を立てて行う
- 原子書き込みは mktemp の位置を「同一ディレクトリ」に統一する(Shell 版は $TMPDIR で別 FS だと非原子。挙動互換の範囲内の堅牢化)
- Zellij 副作用はコマンド実行の有無まで(実際のフォーカス移動は実環境確認で担保)
