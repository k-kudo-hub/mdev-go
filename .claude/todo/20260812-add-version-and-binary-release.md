# 統合 6-1: バイナリ配布と自己更新(ADR-0004 D2/D3/一部 D4)

## 概要

ADR-0004 の実施フェーズ 1。mdev バイナリの版の可視化・GitHub Release へのバイナリ添付・`mdev update` の自己更新を実装し、「バイナリだけ古い」問題を根治する。mdev-go のみで完結する(conductor 資産の更新は現行の tarball+install.sh 経路を temporary に共存させ、6-3 で一本化)。

## TODO

### A. `mdev version`(ADR D3-1)

- [x] cmd/mdev に version 変数(ldflags `-X` 注入、既定 "dev")と `mdev version` サブコマンドのテスト+実装
- [x] Makefile: build / install ターゲットに `-trimpath -ldflags "-s -w -X ...=$(git describe)"` を追加

### B. Release へのバイナリ添付(ADR D2)

- [x] tag.yml 拡張: macos-latest で darwin/arm64 + darwin/amd64 をビルド(CGO_ENABLED=0、`-X main.version=<tag>`)、checksums.txt 生成、`gh release create <tag> dist/*` で**原子的に**添付
- [x] tag.yml にアセット添付の検証ステップ(`gh release view --json assets` で 3 件確認、ADR D9-3)
- [x] ランナー変更に伴う実行時間・権限(permissions)の確認

### C. `mdev update` の自己更新(ADR D4-2 の前半)

- [x] domain: 自己更新の判定のテスト+実装(埋め込み版数 vs mdev-go リポジトリの最新タグ。"dev" ビルドは自己更新スキップ)
- [x] infra: バイナリアセットの DL(arch 判定 → `mdev_darwin_<arch>` を取得、checksums.txt で SHA-256 検証、60s/100MB 上限は既存 release パッケージの流儀)
- [x] infra: 自己置換のテスト+実装(`bin/mdev.new` へ書き込み → chmod +x → rename で原子的置換。実行中バイナリの置換は rename なら安全であることを実機確認)
- [x] app+cli: `mdev update` に組み込み(順序: ①自バイナリの更新 → ②conductor 資産の更新(現行フロー維持)。①で置換した場合は新バイナリで②を exec するか、次回に回すかを設計して報告)
- [x] `mdev check-update` の案内に mdev-go の新版も含める(conductor と別行で)

### D. 検証・仕上げ

- [x] make check(カバレッジ閾値維持)
- [x] マージ後の実地検証手順の整理: 次リリース(v0.11.0)にアセットが付くこと → その次のリリースで `mdev update` の自己更新が実際に動くこと(2 リリースにまたがる検証である旨を明記)

## マージ後の実地検証手順(2 リリースにまたがる)

この機能は **1 回のリリースでは検証しきれない**。アセットを添付する側と、
それを取りに行く側が別のリリースになるためである。

### 第 1 段: 次のリリース(この PR のマージで作られる版)

tag.yml の変更が初めて動く。確認するのは次の 3 点。

1. ジョブが macos-latest で緑になること(ランナー変更の初回実行)
2. リリースにアセットが 3 件付くこと(`mdev_darwin_arm64` / `mdev_darwin_amd64` /
   `checksums.txt`)。検証ステップがあるので、欠ければ job が落ちる
3. 添付されたバイナリが版を名乗ること
   `curl -L -o /tmp/mdev <URL> && chmod +x /tmp/mdev && /tmp/mdev version`

この時点では **自己更新はまだ動かない**。今インストールされている
バイナリの版が古く、その版のリリースにアセットが無いためではなく、
「取りに行く先」が第 1 段のリリースになるので、実際に動くのは次からである。

### 第 2 段: そのさらに次のリリース

第 1 段のバイナリを入れた状態で `mdev update` を実行し、自己置換が実際に
起きることを確認する。

1. `mdev version` で第 1 段の版が出ること
2. `mdev update` が「mdev 自身を <旧> -> <新> に更新します...」と出し、
   置き換え後に実行し直しの案内が出ること
3. `mdev version` が新しい版を名乗ること
4. もう一度 `mdev update` を実行すると、今度は conductor 資産の更新へ進むこと

### 現時点で確認済みのこと

- 実バイナリでの自己置換(v0.0.1 → v9.9.9、file:// の配布物で)
- 実行中バイナリへの rename が安全であること(macOS で実測)
- 版を焼いたビルドが実際に mdev-go の最新タグを引き、アセットが無い
  現行リリースに対しては **バイナリを壊さず中止する**こと(404 で error)

## 完了条件

- `mdev version` が埋め込み版数を表示する(make install 経由でも)
- リリースにバイナリ 2 種 + checksums が原子的に添付される
- `mdev update` が自バイナリを検証つきで自己置換できる(旧: Go ツールチェーン必須 → 新: 不要)
- 全テスト・lint・カバレッジ通過

## 備考

- mdev-go リポジトリの最新タグ取得は既存 RemoteTagLister(git ls-remote)を流用。取得先 URL は当面ハードコードせず、埋め込みのリポジトリ slug 定数(ldflags 注入 or 定数)とする — 6-3 で REPO_URL 一本化の際に整理
- VERSION ファイル(conductor の版)はこのフェーズでは触らない(D3-2 の一致検証は 6-3 の mdev install で)
- ユーザーテスト 6-1: リリース 2 本を跨いだ自己更新の実演(私が段取りし、確認だけお願いする形を想定)
