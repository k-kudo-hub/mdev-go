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
