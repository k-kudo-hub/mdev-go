# 統合 6-3: インストーラと init の Go 化(ADR-0004 D4/D6/D7/D8)

## 概要

ADR-0004 実施フェーズ 3(最難関)。`mdev install/uninstall/init/test` を実装して install.sh 本体と init.zsh の実質を Go へ移し、実環境を Go 完結の形に切り替える(REPO_URL の mdev-go 化・FLAVOR 廃止・scripts/ 削除)。conductor リポジトリのクローズは 6-4。

## TODO

### A. `mdev install`(ADR D4-1/D4-5/D8-3)

- [ ] app: インストール手順の usecase 設計とテスト(依存チェック(zellij/jq(移行期のみ)/claude)→ 資産配置(embed から。**不在時のみ書く**、実ファイルは上書きしない。multi.kdl は引用符付きペイン呼び出しで書き出す)→ config.json のキー単位マージ(install.sh:128-159 の jq 相当を Go で)→ VERSION/REPO_URL 書き込み(**REPO_URL は mdev-go へ書き換え = 移行の要**)→ hooks 配線 → codex notify 差し替え → zshrc 確認)
- [ ] hooks 配線: settings.json に conductor hooks が無ければ embed の hooks.json から作成、Shell 版 hooks(`/scripts/pending-`)を検出したら Go 版へ書き換え(現 hookswitch のロジックを install に内包)。**FLAVOR ファイルは廃止**(存在すれば削除、hooks switch/restore コマンドは 6-4 で除去予定として deprecated 化)
- [ ] codex notify 差し替え: `~/.codex/config.toml` の notify を `["<CONDUCTOR_HOME>/bin/mdev","codex","notify"]` へ。**Codex Computer Use ラッパーの入れ子(`--previous-notify` の JSON 文字列)を解いて差し替える**処理込み(6-2 申し送り 2)。他ツールの notify は現行 install.sh と同じく触らず案内
- [ ] 移行処理: 既存インストールの `~/.claude-conductor/scripts/` を削除(D5-4。実行前に一覧表示)。`init.zsh` を**シムに置き換え**(下記 B)
- [ ] `mdev uninstall`: 現 uninstall.sh 相当(hooks 除去・codex notify 除去・CONDUCTOR_HOME 削除の案内と実行、自分自身の削除を含む)
- [ ] レイアウト移行(裁定 1): 資産配置は「不在時のみ書く」を維持しつつ、既存ファイル内の既知の Shell 参照だけを外科的に書き換える(`/scripts/*-loop.sh` → `bin/mdev pane *`、`/scripts/agent-launch.sh` → `bin/mdev agent launch`。いずれも引用符付き)。書き換えたら install の出力に 1 行報告
- [ ] `mdev update` を ADR D4-2 の完成形へ(裁定 2): 自己置換の後は **自分自身の `mdev install`(冪等再適用)** で資産を更新する。conductor tarball + bash install.sh のフローは削除。REPO_URL を mdev-go へ切り替えた時点で旧フローは成立しないため同じ PR で行う
- [ ] VERSION と check-update の単一化(ADR D3-2、裁定 2): `mdev install` が VERSION へ **バイナリ自身の版**を書く。REPO_URL が mdev-go を指す場合、check-update は conductor 行を出さず mdev 1 本の比較に収束させる

### B. `mdev init zsh` とシム(ADR D6)

- [ ] cli: `mdev init zsh` — zj 系エイリアスと補完(あれば)を出力。init.zsh は「PATH 追加 + eval "$(mdev init zsh)"」のシムに縮退した内容を embed し、install が書き出す(.zshrc は無変更)
- [ ] `mdev`(attach-or-create)/ `dev` / `zs` / `pending-clear` の Go サブコマンド化: セッション名生成は既存 posixCksum を転用(**cksum 値の「文字列としての下 4 桁」をバイト単位で再現**、既存セッション名互換のテスト必須)。attach/new-session は exec で zellij を前景起動。fetch-news / check-update / sessions clean --auto の呼び出しも Go 側の起動フローに内包
- [ ] シムの `mdev` はバイナリへ委譲(関数名衝突の解消: シムでは関数を定義せず PATH の bin/mdev が受ける)

### C. `mdev test <worktree>`(ADR D7)

- [ ] worktree の `go build` → 隔離データディレクトリ(CONDUCTOR_HOME)→ レイアウトを一時生成(os.Executable ベース)→ テストセッション起動(Warp/iTerm/Terminal の分岐は現 mdev-test 相当)。dry-run 対応
- [ ] task-control 起動の os.Executable() 化(D7-2。hooks は env 展開形を維持 = D7-3)

### D. ブートストラップ install.sh(ADR D4-1)

- [ ] mdev-go リポジトリに新設: 依存チェック → 最新リリース判定 → アセット DL(checksums 検証)→ bin/mdev 配置 → `mdev install` を exec する薄い curl スクリプト(bash 3.2 互換)。README にインストール手順(ブラウザ DL 時の xattr 注意も)

### E. 検証・仕上げ

- [ ] make check(カバレッジ閾値維持)+ 統合検証(隔離環境で: 新規インストール / 既存 Shell 環境からの移行(hooks 書き換え・scripts 削除・REPO_URL 書き換え)/ mdev test の worktree 起動)
- [ ] ユーザーテスト 6-3 の手順書(実環境での install 実行・新シムでの mdev 起動・dev/zs の動作)

## 完了条件

- 隔離環境で「ブートストラップ install.sh 1 発 → mdev 起動 → タスク作成 → dd」が Shell 資産ゼロで通る
- 既存環境の移行(`mdev install`)で hooks/codex/REPO_URL/scripts が正しく切り替わり、ユーザーデータ(config.json/daily/tasks/news)が無傷
- 既存セッション名との互換(24 文字切り詰め)がテストで保証される
- 全テスト・lint・カバレッジ通過

## 備考

- **実環境への適用(mdev install の実行)はユーザーテスト 6-3 として実施**(実装フェーズでは隔離検証まで)
- mdev-test の旧関数は 6-4 で init.zsh から消える。移行期間中はシムに残さない(mdev test へ完全移行)
- jq 依存: config.json マージを Go 化するため原理的には不要になるが、移行期の Shell 資産(なし)…依存チェックから jq を外せるか実装時に判断し報告
