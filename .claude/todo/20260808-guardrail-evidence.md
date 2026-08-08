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
