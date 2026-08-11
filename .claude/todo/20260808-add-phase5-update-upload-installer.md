# フェーズ5(最終): upload-log / fetch-news / update 系の Go 化 + install 巻き戻し根治

## 概要

Shell 版に残る upload-log / fetch-news / update 系を Go 化し、ShellRunner を消滅させて機能パリティを達成する。install.sh 本体の Go 化は「配布モデルの再設計」(mdev-go のバージョン記録・バイナリ配布形態・2 リポジトリの調停が未決)が前提となるため後続の統合作業に回し、代わりに conductor 側 install.sh へ「Go 版採用フラグ」を入れて **sed 再適用問題と hooks 巻き戻し問題を根治**する(両者は install.sh の無条件上書きという同根)。

## スコープ判断(調査結果に基づく提案)

- **入れる**: upload-log(secrets マスク含む)/ fetch-news / check-update / `mdev update` コマンド / conductor 側 FLAVOR 分岐
- **後続に回す**: install.sh・uninstall.sh 本体の Go 化、init.zsh の `mdev session` サブコマンド化(POSIX cksum 互換の実装が前提。互換を切ると既存セッション名に attach 不能になる)

## TODO

### A. upload-log の Go 化(dd フロー完全 Go 化)

- [x] domain: SecretFilter のテストを作成(test.sh 3613-3670 の全ケースを移植: PEM 状態機械・base64 行・7 パターン ERE・未終端 PEM の over-mask)
- [x] domain: SecretFilter を実装(awk 相当の行単位状態機械 + sed 7 パターンを順序保存で適用)
- [x] domain: 要約用会話抽出のテスト+実装(claude/codex v1/v2 両対応、既存 transcript_* を拡張。Reasoning/response_item 除外)
- [x] domain: BuildLogPath / BuildMarkdown のテスト+実装(固定オフセット日付切り出し・taskname サニタイズ・11 フィールド・.message 非包含)
- [x] domain+store: daily 横断レコード選択のテスト+実装(全ファイル走査・最後の一致・プレースホルダ合成)
- [x] infra/git: LogRepository のテスト+実装(upload-cache 永続 clone・fetch 成否で checkout -B 分岐・同一内容 no-op・chore: add work log メッセージ・-c user.email/name)
- [x] infra/shell: SummaryGenerator を実装(claude -p を stdin 渡し・タイムアウト無し・モデル指定なし)
- [x] app: LogUploader usecase のテスト+実装(スキップ=("",nil) / 失敗=err の契約、マスク 2 回適用の不変条件)
- [ ] app: TaskDeleter.Prepare を LogUploader に接続し ShellRunner.UploadLog を削除

### B. fetch-news の Go 化(ShellRunner 消滅)

- [ ] fetch-news.sh の仕様確認+domain のテスト+実装(RSS パース)
- [ ] infra+app: NewsFetcher を実装し ShellRunner を完全削除

### C. update 系の Go 化

- [ ] domain: バージョン比較 / repo slug / latest tag 選択のテスト+実装(uc_* 互換。空 VERSION は v0.0.0 に正規化 = Shell 版の算術エラーを修正)
- [ ] app+infra: check-update のテスト+実装(1 日 1 回キャッシュ `.update-check`・全失敗無言 fail-open・enabled==false 明示判定)
- [ ] app+infra: `mdev update` のテスト+実装(git ls-remote タイムアウト・tarball DL curl 相当 60s・install.sh 検証つき実行・env 注入)
- [ ] cli: `mdev update` / `mdev check-update` サブコマンド配線

### D. 検証・仕上げ

- [ ] golden テスト: gen-golden-upload.sh(Shell filter_secrets / build_markdown と Go 出力の差分テスト)
- [ ] 型チェック・Lint・カバレッジ確認(domain/app 90%)

### E. conductor 側 PR(別リポジトリ・小変更)

- [ ] install.sh に FLAVOR 分岐のテスト+実装($CONDUCTOR_HOME/FLAVOR が go なら layouts を mdev pane 版に書き換え、hooks マージを mdev hook 版で実施、bin/ 温存を明文化)

## 完了条件

- dd / d+番号 のアップロードが Go 実装で成功し、失敗時はタブ削除が中止される(現行 exit 契約の維持)
- 秘密マスクが Shell 版と同一出力(golden 差分テストで機械検証)
- `mdev update` 実行後も Go 版レイアウト・hooks が巻き戻らない(FLAVOR=go)
- ShellRunner(infra/shell の bash 依存)が UploadLog / FetchNews とも消滅
- 全テスト・lint・カバレッジ(domain/app 90% / 全体 70%)通過

## 備考

- 絶対に落とせない不変条件 3 つ: (1) アップロード失敗時は何も消さない (2) exit 0 は成功とスキップの両義で出力の有無が区別子 (3) マスクは要約送信前と最終出力の 2 回適用
- 要約生成は claude CLI 呼び出しのまま(API 直叩きは認証・モデル選択の新設計になるため)。git 操作も git バイナリ exec(go-git は credential helper 互換を壊す)
- 後続の統合作業(パリティ達成後に別途 ADR): 配布モデル(バイナリ配布 or ソースビルド)、install/uninstall の Go 化、init.zsh の session サブコマンド化、mdev-test の 2 worktree 問題
