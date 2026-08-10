# exec タイムアウトのプロセスグループ kill: 検証記録(evidence)

「タイムアウトの kill が孫プロセスに届いていない」ことの再現と、修正でそれが
消えることの実測。憶測は書かず、実行したコマンドと出力だけを残す。

## 1. 壊れていた仕組み

`exec.CommandContext` が context の打ち切りで実行する既定の Cancel は
`cmd.Process.Kill()`、すなわち**直接の子 1 個への SIGKILL** である。

mdev が起こす子は bash スクリプトで、スクリプトはさらに `zellij action ...` を
起こす。したがって上限で切っても止まるのは bash だけで、孫の `zellij action` は
そのまま残る。実環境ではこれが 200 個超まで蓄積し、うち 2 個が 100% CPU で
空転してマシン全体を劣化させた。zellij サーバの劣化は `new-tab` の暗黙フォーカス
切替の遅延・喪失を悪化させるため、「遅い → 上限で切る → 孫が残る → もっと遅い」
という増幅ループになっていた。

なお本作業の時点では残存プロセスは既に掃除済みで 0 件だった
(`ps -axo command | grep -c '[z]ellij action'` → `0`)。200 個超は本 PR より前の
実測値である。

## 2. 修正前(赤)

### 2.1 孫プロセスの残存

テストは「SIGTERM を無視して回り続ける孫を spawn し、自分も回り続ける bash」を
上限 1 秒で実行し、上限後に孫の PID が消えているかを `kill -0`(シグナル 0 の
kill = 生存確認)で 5 秒間ポーリングする。

```
$ go test ./internal/infra/shell/... ./internal/infra/zellij/... -run Grandchild -v
--- FAIL: TestRunCommandKillsGrandchildren (6.02s)         # internal/infra/shell
    runner_test.go:338: 孫プロセス(pid 55337)が生き残っている: 5 秒待っても消えなかった
--- FAIL: TestRunCommandKillsGrandchildren (6.02s)         # internal/infra/zellij
    tabs_test.go:145: 孫プロセス(pid 55412)が生き残っている: 5 秒待っても消えなかった
--- FAIL: TestCommandOutputKillsGrandchildren (6.02s)      # internal/infra/zellij
    tabs_test.go:159: 孫プロセス(pid 55413)が生き残っている: 5 秒待っても消えなかった
FAIL
```

上限を持つ 3 つの入口すべてで孫が生き残る。`6.02s` の内訳は
「上限 1 秒 + 生存確認の 5 秒」で、5 秒待ってなお `kill -0` が成功し続けた。

### 2.2 孫がパイプを握ると呼び出しが返らない

孫の標準出力を親から切り離さない場合(実際の `zellij action` はこの形)、
直接の子を切っても Go は標準出力の写し取りが終わるまで `Wait` から返れない。
修正前の `exec.CommandContext` を素で使う使い捨てプログラムで実測した。

```go
ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
_, err := exec.CommandContext(ctx, "bash", "-c", "bash -c 'sleep 30' &\nsleep 30").Output()
```

```
elapsed=30.01s err=signal: killed
```

**上限 300 ミリ秒に対して 30.01 秒**、つまり孫が終わるまで待たされた。上限は
「返らない呼び出しでポーリングのチェーンを止めない」ために入れたものなので、
この状態では上限がその役目を果たしていない。

## 3. 修正

`internal/infra/proc` に共通ヘルパを置き、上限を持つ全経路をここに通した。

| 手当て | 内容 |
|--------|------|
| `SysProcAttr.Setpgid = true` | 子を新しいプロセスグループのリーダーにする。子が spawn した孫は既定でこのグループを引き継ぐ |
| `cmd.Cancel` の差し替え | `syscall.Kill(-pid, syscall.SIGKILL)`。負の PID はプロセスグループ全体への送信を意味する(kill(2)) |
| `cmd.WaitDelay = 2s` | グループから逃げた孫(setsid など)がパイプを握った場合の保険。Go がパイプを強制的に閉じて Wait を返す |

`ESRCH`(グループに 1 つも残っていない)は `os.ErrProcessDone` に変換する。
変換しないと Go は「打ち切りに失敗した」とみなして別のエラーを被せる。

適用先:

| 経路 | 上限 | 実装 |
|------|------|------|
| `ScreenDetectTick` | 15 秒 | `shell.runCommand` → `proc.Command` |
| `RestoreSession` | 60 秒 | 同上 |
| `ListTabs` | 10 秒 | `zellij.commandOutput` → `proc.Command` |
| `FocusTab` / `CloseTabByID` | 10 秒 | `zellij.runCommand` → `proc.Command` |
| `UploadLog` / `RestoreTask` / `FetchNews` | なし | 同じヘルパを通すが挙動は不変(4 節) |

## 4. 上限なし経路の挙動が変わらない根拠

Go 1.25.5 の `os/exec` は watchCtx を起動する条件を次のように書いている。

```
$(go env GOROOT)/src/os/exec/exec.go:772
	if (c.Cancel != nil || c.WaitDelay != 0) && c.ctx != nil && c.ctx.Done() != nil {
```

上限なしの経路が渡すのは `context.Background()` で、その `Done()` は `nil` を返す。
したがって `Cancel` も `WaitDelay` も**一度も参照されない**。上限なし経路で
実際に変わるのは `Setpgid` だけである。

`Setpgid` は子が端末のフォアグラウンドプロセスグループに入らなくなることを
意味するが、mdev が起こす子は stdin が `/dev/null`(`cmd.Stdin` が nil)で
端末を読み書きしないため、`SIGTTIN` / `SIGTTOU` で止まる経路は無い。

挙動の不変はテストでも固定した。

- `shell.TestRunCommandWithoutTimeoutRuns` — 上限 0 で通常どおり実行できる
- `shell.TestRunnerAppliesTimeoutPerScript` — 3 経路の上限が 0 のまま
- `proc.TestCommandRunsNormally` / `TestCommandReturnsExitError` — 標準出力と非 0 終了

## 5. 修正後(緑)

### 5.1 孫プロセスの消滅

```
$ go test ./internal/infra/shell/... ./internal/infra/zellij/... -run Grandchild -v
--- PASS: TestRunCommandKillsGrandchildren (1.00s)         # internal/infra/shell
--- PASS: TestRunCommandKillsGrandchildren (1.00s)         # internal/infra/zellij
--- PASS: TestCommandOutputKillsGrandchildren (1.00s)      # internal/infra/zellij
```

所要時間が 6.02 秒から 1.00 秒(= 上限そのもの)へ落ちた。生存確認の
ポーリングが 1 周目で終わった、つまり**上限で切った時点で孫は既に消えている**。

### 5.2 パイプを握った孫で待たされない

```
$ go test -race ./internal/infra/proc/... -v
--- PASS: TestCommandDoesNotWaitForGrandchildHoldingStdout (0.30s)
```

2.2 節と同じ形(上限 300 ミリ秒 / 孫は 30 秒)で **30.01 秒 → 0.30 秒**。

### 5.3 proc パッケージ全体

```
--- PASS: TestKillGroupOnMissingGroup (0.00s)
--- PASS: TestKillGroupWithoutProcess (0.00s)
--- PASS: TestCommandSetsProcessGroupAndWaitDelay (0.00s)
--- PASS: TestCommandRunsNormally (0.00s)
--- PASS: TestCommandReturnsExitError (0.00s)
--- PASS: TestCommandDoesNotWaitForGrandchildHoldingStdout (0.30s)
--- PASS: TestCommandKillsGrandchildren (1.00s)
ok  	github.com/k-kudo-hub/mdev-go/internal/infra/proc	2.280s
```

## 6. ガードレール

```
$ make check
gofmt: no diff
golangci-lint: 0 issues.
go-arch-lint: OK - No warnings found
go test -race: 全パッケージ ok(internal/infra/proc 92.3%)
Total coverage threshold (70%) satisfied: PASS   # 実測 90.3%
go build -o bin/mdev ./cmd/mdev
```

ゴールデンは 23 ケース全通過(`TestGoldenPanesMatchShellVersion` の
dashboard 7 / waiting 4 / done 6 / news 6)。外部コマンドの止め方の変更なので
表示には影響しないが、実依存グラフを組み立てる唯一のテストなので毎回確認した。

### depguard

`syscall` は許可リストの `$gostd` に含まれるため弾かれない(`0 issues.`)。
`golang.org/x/sys/unix` へ寄せられないのは `exec.Cmd.SysProcAttr` の型が
`*syscall.SysProcAttr` で決め打ちされているためで、その旨を import に
コメントとして残した。

### 依存方向(ADR-0002)

`internal/infra/proc` は `internal/infra` のサブパッケージで、`.go-arch-lint.yml` の
`infra: { in: [ internal/infra, internal/infra/** ] }` に自動的に含まれる。
標準ライブラリ以外に依存しないため、app / domain への逆流も起きない。

## 7. この修正で防げないこと

- **孫が自分でプロセスグループを抜けた場合**(`setsid`)。グループへの SIGKILL は
  届かない。`WaitDelay` があるので呼び出し側が待たされることは無いが、逃げた
  プロセス自体は残る。現行のスクリプトに `setsid` は無い
- **既にハングしている zellij サーバ**。本 PR が消すのは残留する CLI クライアントで、
  サーバ側の劣化そのものを治すものではない
