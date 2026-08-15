# 統合 6-4(最終): 仕上げと conductor のクローズ(ADR-0004 D8/6-4)

## 概要

ADR-0004 の最終フェーズ。mdev-go 側の残務(deprecated 除去・ドキュメント整合・README)を片付け、claude-conductor に最終リリースと移転告知を置いてアーカイブする。完了で Go リライトプロジェクト全体がクローズする。

## TODO

### A. mdev-go 側の仕上げ

- [x] `mdev hooks switch/restore` コマンドの除去(6-3 で deprecated 化済み。FLAVOR 廃止に伴い存在理由が消滅。hookswitch の変換ロジックのうち install が使う部分は install 側に残す)
- [x] 手順書 §8 の不整合修正: user-test-6-3-install.md の update フロー記述を実挙動(v0.13.1 で実証済みの「自己置換 → 再実行で install」)に合わせ、未使用の RenderUpdateApplying を削除
- [x] CONDUCTOR_HOME 残骸の整理: `hooks.json`(ディスク上は不使用 — install は embed を参照)を install の撤去対象に追加。`config.default.json` は**残す**(store の読み込みフォールバックが現役で参照。理由をコメントに)
- [x] golden 生成スクリプト(gen-golden-*)に「conductor アーカイブ後は再生成不可。fixture がコミット済みのためテストは継続動作する」旨のヘッダ注記
- [x] README 更新: ブートストラップ 1 発のインストール手順(ブラウザ DL 時の xattr 注意込み)・アーキテクチャ概要・claude-conductor からの移転である旨
- [x] ADR-0004 に追記: Shell 版最終リリースのタグ(D8-1)と、**ADR-0001 のデータ形式凍結の解除**(D8-4)を Accepted の決定として記録

### B. conductor 側のクローズ

- [ ] README に移転告知を追加(「開発は k-kudo-hub/mdev-go へ移行。本リポジトリは Shell 版の最終形としてアーカイブ。ロールバック手順 = 最終タグの tarball → ./install.sh」)+ 最終リリース(bump:patch)
- [ ] issue #65(flaky テスト)へ「アーカイブに伴いクローズ(Shell 版テストは凍結)」のコメントを残してクローズ
- [ ] リポジトリのアーカイブ(gh repo archive)— **マージ・リリース・issue 処理がすべて済んでから最後に実行**

### C. 検証・クロージング

- [ ] make check + 実環境での最終サニティ(`mdev update`(最新確認)/ version / sessions clean --dry-run)
- [ ] メモリ更新(プロジェクト完了の記録)と、ローカル ~/projects/claude-conductor の扱いの確認(削除はユーザー判断)

## 完了条件

- mdev-go 単体で「インストール → 日常利用 → 更新」の全ライフサイクルが完結し、ドキュメントもそれだけで読める
- claude-conductor がアーカイブ済み(read-only)で、訪問者に移転先が明示される
- 全テスト・lint・カバレッジ通過

## 備考

- conductor のアーカイブは不可逆に近い操作(解除は可能だが)のため、**B の各手順は実行前にユーザーの最終確認を取る**
- ローカルの ~/projects/claude-conductor ディレクトリと Warp の旧 launch configurations の掃除は任意(ユーザー判断)
