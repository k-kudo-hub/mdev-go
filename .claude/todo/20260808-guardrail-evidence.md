# ガードレール違反検出の確認記録

各ガードレールが「意図的な違反で実際に fail する」ことをローカル実行で確認した記録。
実行環境: go1.25.5 darwin/arm64 / macOS 15 (Darwin 24.5.0)。

## 1. go-arch-lint(依存方向)

- ツール: `github.com/fe3dback/go-arch-lint@v1.17.0`(Archfile spec version 3)
- 設定: `.go-arch-lint.yml`
- コマンド: `go run github.com/fe3dback/go-arch-lint@v1.17.0 check --output-color=false`

### 正常時

```
module: github.com/k-kudo-hub/mdev-go
OK - No warnings found
exit status 0
```

### 違反 1: domain → infra

一時ファイル `internal/domain/violation_tmp.go`:

```go
package domain

import _ "github.com/k-kudo-hub/mdev-go/internal/infra"
```

出力(exit status 1):

```
Component domain shouldn't depend on github.com/k-kudo-hub/mdev-go/internal/infra in .../internal/domain/violation_tmp.go:3
total notices: 1
```

### 違反 2: cli → tui(レイヤー間の相互参照)/ 違反 3: app → infra

一時ファイル `internal/cli/violation_tmp.go` と `internal/app/violation_tmp.go` に
それぞれ `import _ ".../internal/tui"`、`import _ ".../internal/infra"` を追加。

出力(exit status 1):

```
Component app shouldn't depend on github.com/k-kudo-hub/mdev-go/internal/infra in .../internal/app/violation_tmp.go:3
Component cli shouldn't depend on github.com/k-kudo-hub/mdev-go/internal/tui in .../internal/cli/violation_tmp.go:3
total notices: 2
```

確認後、一時ファイルはすべて削除し `go build ./...` が通ることを確認済み。

### 設定上の注意(実行して判明した挙動)

- `deps` に空のエントリ(`domain: {}`)を書くと spec エラーになる
  (`should have ref in 'mayDependOn'/'canUse' or at least one flag of [...]`)。
  「何にも依存しない」は自コンポーネントのみを `mayDependOn` に書いて表現した。
- 外部テストパッケージ(`package domain_test`)から自パッケージを import すると
  自コンポーネント参照として検出されるため、全コンポーネントに自分自身を
  `mayDependOn` へ明示している。テストファイルを解析対象から除外していないので、
  テストコードのレイヤー違反も検出される。

## 2. golangci-lint(depguard / errcheck / errorlint / gocritic)

- ツール: `github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2`
- 設定: `.golangci.yml`(`version: "2"`、`config verify` で検証済み)
- コマンド: `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run ./...`

### 正常時

```
0 issues.
exit status 0
```

### 違反 1: 許可外ライブラリの import(depguard 全体ルール)

`go get github.com/sirupsen/logrus@v1.9.3` の上で `internal/app/violation_tmp.go` に
`import "github.com/sirupsen/logrus"` を追加。

```
internal/app/violation_tmp.go:3:8: import 'github.com/sirupsen/logrus' is not allowed from list 'all' (depguard)
exit status 1
```

### 違反 2: domain から許可ライブラリを import(depguard domain 限定ルール)

`internal/domain/violation_tmp.go` に `import "github.com/spf13/cobra"` を追加。
cobra は全体ルールでは許可だが、domain では標準ライブラリ以外を禁止しているため落ちる。

```
internal/domain/violation_tmp.go:3:8: import 'github.com/spf13/cobra' is not allowed from list 'domain' (depguard)
exit status 1
```

### 正常系の対照確認: 許可ライブラリは通る

`internal/cli/violation_tmp.go` で cobra を import し `*cobra.Command` を返す関数を定義したが、
depguard の指摘は出なかった(同時に実行した上記 2 件のみが報告された)。許可リストが
「許可すべきものを許可している」ことを確認済み。

### 違反 3〜6: errcheck / errorlint / gocritic

`internal/app/violation_tmp.go` にエラー握り潰し・`==` によるエラー比較・`%v` ラップ・
if-else 連鎖を書いた結果:

```
internal/app/violation_tmp.go:13:11: Error return value of `os.Remove` is not checked (errcheck)
internal/app/violation_tmp.go:18:9: comparing with == will fail on wrapped errors. Use errors.Is to check for a specific error (errorlint)
internal/app/violation_tmp.go:23:35: non-wrapping format verb for fmt.Errorf. Use `%w` to format errors (errorlint)
internal/app/violation_tmp.go:28:2: ifElseChain: rewrite if-else to switch statement (gocritic)
internal/app/violation_tmp.go:38:37: ST1008: error should be returned as the last argument (staticcheck)
exit status 1
```

errcheck / errorlint / gocritic に加え、既定の staticcheck も併せて発火することを確認した。

確認後、一時ファイルを削除し `go.mod` / `go.sum` を元に戻して `0 issues.` に戻ることを確認済み。

### 設定上の注意(実行して判明した挙動)

- depguard の `files` グロブは `**/internal/domain/**/*.go` では**マッチしなかった**
  (domain の違反が検出されなかった)。`**/internal/domain/**` に変更して検出されるようになった。
- depguard は `list-mode: strict` で「許可リスト以外はすべて拒否」となる。自モジュール
  (`github.com/k-kudo-hub/mdev-go`)も許可リストに明示しないと自パッケージの import が落ちる。
- golangci-lint v2 の既定 (`default: standard`) は errcheck / govet / ineffassign /
  staticcheck / unused。errcheck は既定に含まれるが、ADR-0003 の意図を明示するため
  `enable` にも列挙している。

## 3. go-test-coverage(カバレッジ閾値)

- ツール: `github.com/vladopajic/go-test-coverage/v2@v2.19.0`
- 設定: `.testcoverage.yml`(全体 70%、`internal/domain` と `internal/app` は 90%)
- コマンド:
  ```
  go test -race -covermode=atomic -coverprofile=cover.out ./...
  go run github.com/vladopajic/go-test-coverage/v2@v2.19.0 --config=.testcoverage.yml
  ```

### 正常時

```
Package coverage threshold (0%) satisfied:	PASS
Total coverage threshold (70%) satisfied:	PASS
Total test coverage: 80.0% (8/10)
exit status 0
```

### 違反 1: domain のカバレッジが 90% を下回る

`internal/domain/violation_tmp.go` にテストのない 1 文の関数を追加(domain 8/9 = 88.9%)。

```
Package coverage threshold (0%) satisfied:	FAIL
  below threshold:				coverage:	threshold:
  internal/domain				88.9% (8/9)	90%

Total coverage threshold (70%) satisfied:	PASS
Total test coverage: 72.7% (8/11)
exit status 1
```

全体は 72.7% で PASS のまま domain だけが落ちており、層別の上書きルールが効いている。

### 違反 2: app のカバレッジが 90% を下回る

`internal/app/violation_tmp.go` にテストのない 1 文の関数を追加(app 0/1 = 0.0%)。

```
Package coverage threshold (0%) satisfied:	FAIL
  below threshold:				coverage:	threshold:
  internal/app					 0.0% (0/1)	90%
exit status 1
```

### 違反 3: 全体が 70% を下回る

`internal/cli/violation_tmp.go` にテストのない 5 文の関数を追加。

```
Package coverage threshold (0%) satisfied:	PASS
Total coverage threshold (70%) satisfied:	FAIL
Total test coverage: 53.3% (8/15)
exit status 1
```

domain の上書きルール外(cli)の未テストコードは全体閾値で捕まる。

確認後、一時ファイルをすべて削除し、再実行して PASS(80.0%)に戻ることを確認済み。

### テストなし・空パッケージの扱い(実行して確認)

- **doc.go のみで実行文を 1 つも持たないパッケージ**(現状の `internal/app`、`internal/cli`、
  `internal/tui`、`internal/infra`)は、カバレッジプロファイルに 1 行も出力されないため
  go-test-coverage の統計に現れず、**閾値判定の対象外**になる。
  `--breakdown-file-name` の出力も `cmd/mdev/main.go;2;0` と
  `internal/domain/taskname.go;8;8` の 2 行のみで、空パッケージは含まれない
  (go-test-coverage 側にも「statements を持たないファイルは含めない」実装がある)。
- したがって「`internal/app` に 90% を課しているのに app にテストが無い」状態でも現時点では
  緑になる。実行文が 1 つでも追加された瞬間に 90% 判定の対象となり、違反 2 のとおり落ちる。
- **テストファイルが 1 つも無いパッケージ**であっても、実行文があればプロファイルに 0% として
  出力され判定対象になる(違反 2 と違反 3 がその実例)。

### 設定上の注意(実行して判明した挙動)

- `override.path` の正規表現が照合されるのは**モジュールパスを取り除いたリポジトリ相対パス**
  (例: `internal/domain`、`internal/domain/taskname.go`)である。
  当初 `^github\.com/k-kudo-hub/mdev-go/internal/domain$` と書いていたが**一切マッチせず**、
  domain のカバレッジが 88.9% でも PASS してしまった。`^internal/domain$` に修正して検出された。
- `threshold.package` を 0(無効)にしていても `override` は機能する。レポートの見出しは
  `Package coverage threshold (0%)` のままだが、上書きルールに一致したパッケージには
  上書き後の閾値が適用される。
- `override.path` が `.go` / `.go$` で終わるかどうかでファイル単位かパッケージ単位かが
  判定される。パッケージ単位にしたい場合は `.go` で終わらせないこと。
- カバレッジプロファイルが古い(削除済みファイルを含む)と
  `could not find file [...]` で失敗する。`go test` の後に必ず実行すること。

## 4. ツールバージョン固定方法の検証(go.mod tool ディレクティブ)

ADR-0003 は「`go.mod` の tool ディレクティブ等でリポジトリに固定する」としているため、
tool ディレクティブを第一候補として実際に検証した。結論として **tool ディレクティブは採用せず**、
`Makefile` + バージョン固定 `go run <module>@<version>` を採用する。

### 検証内容と結果

| 組み合わせ | `go get -tool` | `go mod tidy` | 結果 |
|------------|----------------|---------------|------|
| golangci-lint v2.12.2 のみ | 成功 | 成功 | `go tool golangci-lint --version` 動作。`go` ディレクティブは `1.25.0` |
| go-arch-lint v1.17.0 のみ | 成功 | 成功 | `go tool go-arch-lint check` 動作。`go` ディレクティブは `1.25.0` |
| + go-test-coverage v2.19.0 | 成功 | 成功 | `go` ディレクティブが **`1.26` に引き上げられる** |
| 3 ツールすべて | 成功 | **失敗** | ambiguous import で `go mod tidy` が exit 1 |

### 不採用の決め手 1: 3 ツール同居で `go mod tidy` が失敗する

```
go: github.com/fe3dback/go-arch-lint imports
	...
	oss.terrastruct.com/d2/d2layouts/d2dagrelayout imports
	cdr.dev/slog tested by
	cdr.dev/slog.test imports
	cdr.dev/slog/sloggers/slogstackdriver imports
	google.golang.org/genproto/googleapis/logging/v2 imports
	google.golang.org/genproto/googleapis/rpc/status: ambiguous import: found package google.golang.org/genproto/googleapis/rpc/status in multiple modules:
	google.golang.org/genproto v0.0.0-20220519153652-3a47de7e79bd
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217
(cloud.google.com/go/compute/metadata でも同種の ambiguous import が発生)
TIDY=1
```

go-arch-lint が依存する `oss.terrastruct.com/d2` → `cdr.dev/slog` の古い
`google.golang.org/genproto` と、golangci-lint 側が引き込む分割後の
`google.golang.org/genproto/googleapis/rpc` が衝突する。`go mod tidy` が通らない状態は
日常の依存追加(cobra / bubbletea を入れるフェーズ 1〜2)で確実に支障になる。

### 不採用の決め手 2: `go` ディレクティブが 1.26 に引き上げられる

go-test-coverage は v2.18.4 以降 `go 1.26` を要求する(v2.18.3 までは `go 1.24`)。
tool ディレクティブに加えると本体の `go.mod` が `go 1.26` に書き換わり、
ADR-0001/0002 で前提としている `go 1.25` を満たさなくなる。

### 不採用の決め手 3: go.mod / go.sum の肥大化

3 ツールを tool 依存にすると `go.mod` 220 行超・`go.sum` 926 行超まで膨らみ、
本体の依存(cobra / bubbletea のみの予定)がレビューで見えなくなる。
また `go tool golangci-lint` 実行時に `missing go.sum entry for module providing package
github.com/fsnotify/fsnotify` が出るなど、go.sum の整合を別途取る必要があった。

### 採用した代替手段

`Makefile` にバージョンを変数として持ち、`go run <module>@<version>` で実行する。

```make
GOLANGCI_LINT_VERSION    := v2.12.2
GO_ARCH_LINT_VERSION     := v1.17.0
GO_TEST_COVERAGE_VERSION := v2.19.0
```

- `go run <module>@<version>` はカレントモジュールの `go.mod` を無視して解決するため、
  本体の依存グラフを一切汚さない(検証後 `git checkout go.mod && rm go.sum` で
  完全にクリーンな状態に戻ることを確認済み)
- ローカルも CI も `make lint` / `make arch` / `make cover` を呼ぶため、バージョンは常に一致する
- 初回はビルドが走るが、以降はビルドキャッシュが効く(`make check` 全体で約 4 秒)
- 副作用として go-test-coverage v2.19.0 の実行時に
  `requires go >= 1.26; switching to go1.26.5` が出て Go ツールチェーンが自動取得される。
  これは `go run` 側の一時的な解決であり、本体の `go.mod`(`go 1.25`)には影響しない

### `make check` の実行結果(全ガードレール緑)

```
gofmt: no diff
golangci-lint run ./...            -> 0 issues.
go-arch-lint check                 -> OK - No warnings found
go test -race -covermode=atomic    -> ok  internal/domain  coverage: 100.0%
go-test-coverage                   -> Total coverage threshold (70%) satisfied: PASS / 80.0% (8/10)
go build -o bin/mdev ./cmd/mdev    -> 成功
CHECK_EXIT=0
```

## 5. bump ラベルフロー(bump-label-check.yml / tag.yml / scripts/bump-version.sh)

claude-conductor から移植した。移植にあたっての変更点は次の 3 点のみ。

- `actions/checkout` を `v4` から `v7`(2026-08-08 時点の最新メジャー)に更新
- `bump-label-check.yml` に `permissions: contents: read` を明示
- `tag.yml` の `run` 内で `${{ steps.bump.outputs.level }}` を直接展開していた箇所を
  `env: LEVEL:` 経由に変更(`run` へのスクリプト注入を避けるため)

構文検証: `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7` が
`.github/workflows/` の 3 ファイルすべてで exit 0(指摘なし)。

### bump-label-check の判定ロジックをローカルで再現して確認

ワークフロー内の jq 式をそのまま実行した結果:

```
LABELS=[]                   -> FAIL(exit1)
LABELS=["bug"]              -> FAIL(exit1)
LABELS=["bump:patch"]       -> PASS(exit0)
LABELS=["bug","bump:major"] -> PASS(exit0)
```

bump ラベルが無い PR が確実に落ちることを確認した。

### tag.yml の bump レベル決定を確認

```
["bump:patch"]              -> patch
["bump:patch","bump:major"] -> major   (major > minor > patch の優先度)
["bug"]                     -> <skip>  (タグ生成をスキップ)
```

### scripts/bump-version.sh の動作確認

```
bash scripts/bump-version.sh minor ""      -> v0.1.0
bash scripts/bump-version.sh patch v1.2.3  -> v1.2.4
bash scripts/bump-version.sh major 1.2.3   -> v2.0.0
bash scripts/bump-version.sh foo v1.0.0    -> exit 1
    bump-version.sh: invalid bump type: 'foo' (expected major|minor|patch)
```

### bump ラベルの作成

```
gh label create bump:major --color B60205 --description "..." -R k-kudo-hub/mdev-go
gh label create bump:minor --color FBCA04 --description "..." -R k-kudo-hub/mdev-go
gh label create bump:patch --color 0E8A16 --description "..." -R k-kudo-hub/mdev-go
```
