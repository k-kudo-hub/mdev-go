# ADR-0004: 配布モデルと最終統合(リポジトリ統合・バイナリ配布・Shell 全廃)

- Status: Accepted
- Date: 2026-08-12
- 関連: ADR-0001(Go リライトとデータ互換)、ADR-0002(ports & adapters)、ADR-0003(ガードレール)

## Context

フェーズ 1〜5 の完了により機能パリティを達成した(mdev-go v0.10.0 / claude-conductor v0.9.0、FLAVOR=go で共存中)。残るは install.sh 本体と init.zsh だが、これは移植ではなく配布モデルの再設計を要する。調査(2026-08-12)で確定した中心問題:

1. **mdev バイナリの版を知る手段が無い**(`mdev --version` は unknown flag。ldflags 注入も version コマンドも無い)
2. **`mdev update` はバイナリを永久に更新しない**(install.sh は `bin/` に意図的に触れず、バイナリ更新手段は `git pull && make install` = Go ツールチェーン必須のみ)
3. 両リポジトリの GitHub Release には**アセットが 1 つも無い**(ソース tarball のみ)
4. 2 リポジトリでバージョン体系・リリースフローが独立しており、「バイナリだけ古い」状態が構造的に発生する
5. Go 版で実際に生きている Shell は 8 ファイルまで減っており、うち load-bearing なのは fetch-news.sh(News の起動時取得)・codex-notify.sh・agent-launch.sh の 3 経路。Go 側に不足するのは CLI 配線程度(news の Fetcher は実装済み、posixCksum も実装済み)
6. init.zsh の全関数(mdev / dev / zs / pending-clear)は**シェル状態を変更していない**ため、シェル関数である必要が無い(業界定石: starship / zoxide / fnm はいずれも「バイナリ + rc に 1 行」)

## Decision

### D1. リポジトリ体制

- **mdev-go を正本とし続ける**(当初方針 = ADR-0001 の「claude-conductor には直接コミットしない」を維持)。claude-conductor は最終統合の完了をもって**凍結し、アーカイブ**する
- 移行は `mdev install` が `REPO_URL` を mdev-go へ書き換える処理で完結する(現在の利用者は移行を主導する本人のみであり、conductor 経由の update 経路を温存する理由は無い)。conductor 側には最終リリースを 1 本置き、README で移転先を告知する
- タグ線は mdev-go の v0.10.0 の続き(v0.11.0〜)。CI・bump-version.sh は mdev-go 版が既に正
- ADR は mdev-go の `docs/adr/` のまま。conductor に必要な資産(layouts / config.default.json / hooks.json / codex-notify の仕様)は**統合フェーズで mdev-go 側へ移設**する(embed 化と同時)

### D2. バイナリ配布

- GitHub Release に **darwin/arm64・darwin/amd64 のバイナリ + checksums.txt を添付**する。ソースビルド配布(Go ツールチェーンをユーザー要件にする案)は却下
- goreleaser は**不採用**(bump ラベル駆動の既存タグ生成と役割が衝突)。tag.yml に setup-go + ビルドループ + `gh release create <tag> <files...>` を追加(約 15 行)
- **リリース作成とアセット添付は 1 コマンドで原子的に**行う(分離すると update が失敗する窓が空く)
- ビルドは **macos-latest ランナー**(Go リンカの ad-hoc 署名が確実。ci.yml と同一)
- **コード署名・Notarization は行わない**。install は curl 経由で quarantine が付かないため。README に「ブラウザで DL した場合は `xattr -d com.apple.quarantine`」を明記
- ビルドフラグ: `-trimpath -ldflags "-s -w -X main.version=<tag>"`
- Homebrew tap は今回採用せず、将来の選択肢として記録

### D3. バージョンの単一化

- `mdev version` サブコマンドを新設(ldflags で焼いた版を表示)
- `~/.claude-conductor/VERSION` を唯一の版とし、install がバイナリの自己申告と VERSION の一致を検証
- REPO_URL / .update-check の形式は現行維持

### D4. インストーラの Go 化と自己更新

- install.sh は**ブートストラップ専用の薄い curl スクリプト**に縮退(依存チェック → 最新版判定 → バイナリ DL → `mdev install` を exec)。config マージ・hooks 配線・codex notify・zshrc 追記の本体ロジックは **`mdev install`** へ
- `mdev update` は「新バイナリを DL → rename で自己置換 → 新バイナリの `mdev install`」に変更(**バイナリ自身が更新可能になる** = 中心問題 2 の根治)
- `mdev uninstall` を新設(自分自身の削除を含む)
- `layouts/*.kdl`・`config.default.json`・`hooks.json` を **`go:embed`**。`~/.claude-conductor` は**データと状態ファイルのみ**に。ただし `CONDUCTOR_HOME/layouts/*.kdl` が存在する場合はそちらを優先(カスタマイズの退避路)
- **FLAVOR ファイルと `mdev hooks switch/restore` は廃止**。`mdev install` が settings.json 内の Shell 版 hooks(`/scripts/pending-` を含むコマンド)を検出して Go 版へ書き換える処理を内包

### D5. 残存 Shell の全廃(前提実装)

- `mdev news fetch` 新設(Fetcher 実装済み・配線のみ)+ init 経路から fetch-news.sh を除去
- `mdev codex notify` 新設 + `~/.codex/config.toml` の notify を書き換え(既存の `["bash", ".../codex-notify.sh"]` の移行処理込み)
- `mdev agent launch` 新設(dev.kdl の Agent ペイン用)
- 上記 3 つが揃った時点で `scripts/` を配布物から削除。既存インストールの `~/.claude-conductor/scripts/` は `mdev install` が**削除**する(残すと「どちらが動いているか」が不明になるため)

### D6. init.zsh の縮退

- `mdev`(attach-or-create)/ `dev` / `zs` / `pending-clear` を Go サブコマンドへ移す(シェル状態を変更していないため技術的障壁なし)
- セッション名の 24 文字切り詰めは実装済みの `domain.posixCksum` を転用。**cksum 値の「文字列としての下 4 桁」という現仕様をバイト単位で再現**する(互換を壊すと既存セッション名に attach 不能)
- `.zshrc` の既存行 `source ~/.claude-conductor/init.zsh` は**変更しない**。init.zsh を「PATH 追加 + `eval "$(mdev init zsh)"`」の 2〜3 行のシムに縮退させる(既存ユーザーの .zshrc 書き換え不要)
- `zj` 系エイリアスは `mdev init zsh` の出力で維持する

### D7. 開発時の隔離モデル(mdev-test の後継)

- `mdev test <worktree>` を新設: worktree のソースを `go build` → 隔離データディレクトリを CONDUCTOR_HOME に → レイアウトを一時生成してテストセッションを起動(**2 worktree 問題の解消**)
- task-control 等セッション寿命のプロセス起動は `os.Executable()` ベースへ(今動いているバイナリが自分の子を起動する)
- **hooks のコマンド文字列は `${CONDUCTOR_HOME:-...}` の env 展開形を維持**(worktree の絶対パスを settings.json に焼かない)
- レイアウトは実行時生成(install 時 sed の廃止)。CONDUCTOR_HOME は「データの所在」のみに再定義

### D8. ロールバックと移行

- Shell 版最終リリースのタグ(統合直前の conductor 最新)を ADR 追記で確定し、ロールバック手順(旧タグ tarball → ./install.sh)を README に 1 行で残す
- FLAVOR による切り替えは統合リリースで廃止(戻す道は旧タグ経由のみ)
- `mdev install` の移行処理: (i) Shell hooks → Go hooks (ii) codex notify 書き換え (iii) `scripts/` 削除 (iv) layouts 再生成 (v) .zshrc 無変更 (vi) config.json はキー単位マージを Go で再現
- **ADR-0001 のデータ形式凍結は統合完了をもって解除**する

### D9. テストとガードレール

- conductor test.sh は Shell 実装の消滅に伴い縮退。install/uninstall/移行の検証は Go テスト + 隔離 E2E(gen-golden 方式の隔離 HOME)へ移す
- install/update の新コードにも ADR-0002 の port 化と ADR-0003 のカバレッジ閾値を適用
- tag.yml に「アセットが添付されたことの検証」ステップを追加

## 実施計画(各フェーズ末にユーザーテスト)

1. **統合 6-1: バイナリ配布と自己更新** — mdev version / Release アセット(tag.yml 拡張)/ `mdev update` の自己置換(mdev-go のみで完結)
2. **統合 6-2: Shell 全廃の前提実装** — news fetch / codex notify / agent launch + conductor の資産(layouts / config.default.json / hooks.json)を mdev-go へ移設して embed
3. **統合 6-3: インストーラと init** — `mdev install/uninstall/init/test`、ブートストラップ install.sh(mdev-go 側に新設)、REPO_URL の書き換え、FLAVOR 廃止
4. **統合 6-4: conductor のクローズ** — 実環境の `scripts/` 削除・最終リリース・README 告知 → claude-conductor をアーカイブ

## Consequences

- ✅ 利用者の依存が curl + zellij(+ 移行期 jq)に減り、更新は `mdev update` 1 コマンドで原子的に完結する
- ✅ 「バイナリだけ古い」事故が構造的に消える。リリースは PR 1 本 → タグ 1 本
- ✅ 配布物から Shell が消え、bash 3.2 互換制約・二重実装(fetch-news の awk/Go 等)が終わる
- ⚠️ リポジトリ移設中は CI ガードレールの一時的な断絶リスクがある(6-1 で移設完了を最優先し、他の変更を混ぜない)
- ⚠️ Shell 版へのロールバックは旧タグ経由のみになる(FLAVOR 切り替えの廃止)
- ⚠️ ブラウザから手動 DL したバイナリは quarantine で起動を拒否される(README で xattr 手順を案内)
- 🔍 検証項目として残す: クロスコンパイル時の ad-hoc 署名の実測(macos-latest 採用で論点消滅の見込み)、`-s -w` のサイズ削減幅
