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
| `Open`(URL を開く) | 10 秒 | `shell.runOpen` → `proc.Command`(8 節の指摘 3 で追加) |
| `UploadLog` / `RestoreTask` / `FetchNews` | なし | 素の `exec.Command` のまま(4 節) |

## 4. 上限なし経路を分けている理由

上限を持たない 3 経路(UploadLog / RestoreTask / FetchNews)は proc を通さず、
素の `exec.Command` のままにしている。プロセスグループを分けると、端末が
閉じたときにカーネルが送る **SIGHUP がフォアグラウンドのプロセスグループにしか
届かず、子へ連鎖しなくなる**ためである。上限のある呼び出しは高々その上限で
自分から片付くので分けてよいが、上限の無い呼び出しは連鎖が切れると端末が
消えた後も残り続ける。

なお仮に同じヘルパを通しても、打ち切り自体は起きない。Go 1.25.5 の `os/exec` は
watchCtx を起動する条件を次のように書いており、

```
$(go env GOROOT)/src/os/exec/exec.go:772
	if (c.Cancel != nil || c.WaitDelay != 0) && c.ctx != nil && c.ctx.Done() != nil {
```

上限なしの経路が渡す `context.Background()` の `Done()` は `nil` を返すため
`Cancel` も `WaitDelay` も一度も参照されない。つまり残る差は `Setpgid` だけで、
上の SIGHUP 連鎖がその唯一の実害である。

使い分けはテストで固定した。

- `shell.TestCommandUsesProcessGroupOnlyWithTimeout` — 上限つきだけ Setpgid と Cancel が立つ
- `zellij.TestCommandUsesProcessGroupOnlyWithTimeout` — 同上
- `shell.TestRunCommandWithoutTimeoutRuns` — 上限 0 で通常どおり実行できる
- `shell.TestRunnerAppliesTimeoutPerScript` — 3 経路の上限が 0 のまま

## 5. 修正後(緑)

### 5.1 孫プロセスの消滅

2.1 節と同じ 3 本を、同じ形のまま流した結果。

```
$ go test ./internal/infra/shell/... ./internal/infra/zellij/... -run Grandchild -v
--- PASS: TestRunCommandKillsGrandchildren (1.00s)         # internal/infra/shell
--- PASS: TestRunCommandKillsGrandchildren (1.00s)         # internal/infra/zellij
--- PASS: TestCommandOutputKillsGrandchildren (1.00s)      # internal/infra/zellij
```

所要時間が 6.02 秒から 1.00 秒(= 上限そのもの)へ落ちた。生存確認の
ポーリングが 1 周目で終わった、つまり**上限で切った時点で孫は既に消えている**。

この 3 本はレビュー指摘 4(8 節)で `proc.TestCommandKillsGrandchildren` の
1 本に集約した。同じ形・同じ結果(1.00s で PASS)で、shell / zellij 側には
「上限つきなら proc.Command を通す」ことの確認だけを残している。

### 5.2 パイプを握った孫で待たされない

```
$ go test -race ./internal/infra/proc/... -v
--- PASS: TestCommandDoesNotWaitForGrandchildHoldingStdout (0.30s)
```

2.2 節と同じ形(上限 300 ミリ秒 / 孫は 30 秒)で **30.01 秒 → 0.30 秒**。

### 5.3 proc パッケージ全体

```
--- PASS: TestKillGroupOnMissingGroup (0.00s)
--- PASS: TestKillGroupOnReapedProcess (0.00s)
--- PASS: TestKillGroupWithoutProcess (0.00s)
--- PASS: TestCommandSetsProcessGroupAndWaitDelay (0.00s)
--- PASS: TestCommandRunsNormally (0.00s)
--- PASS: TestCommandReturnsExitError (0.00s)
--- PASS: TestCommandDoesNotWaitForGrandchildHoldingStdout (0.30s)
--- PASS: TestCommandKillsGrandchildren (1.00s)
ok  	github.com/k-kudo-hub/mdev-go/internal/infra/proc	2.738s
```

`-count=3` でも揺れは出なかった(`ok ... 4.195s`)。実プロセスを起こすテストは
失敗しても孫を残さないよう、PID を読んだ時点で `t.Cleanup` に SIGKILL を
登録している。全実行後に `pgrep` で残存 0 を確認した。

## 6. ガードレール

```
$ make check
gofmt: no diff
golangci-lint: 0 issues.
go-arch-lint: OK - No warnings found
go test -race: 全パッケージ ok(internal/infra/proc 86.7% / shell 96.4% / zellij 91.3%)
Total coverage threshold (70%) satisfied: PASS   # 実測 90.4%
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

## 8. コードレビュー指摘への対応(CONFIRMED / PLAUSIBLE 4 件)

### 指摘 1: Cancel の PID 使い回しガード(CONFIRMED)

`cmd.Cancel` は **reap 済みの相手にも呼ばれうる**。`os/exec` の watchCtx は

```go
select {
case resultc <- ctxResult{}:   // Wait が結果を受け取った
	return
case <-c.ctx.Done():           // 先に上限が来た
}
```

の形で、`resultc` の受け手は `Wait()` の中で `p.Wait()`(= reap)が返った
**後**にある。したがって「子が終わって reap された直後に上限が来る」窓では
Cancel が走る。生の `syscall.Kill(-pid, SIGKILL)` はこのとき、使い回された
PID のプロセスグループを撃ちうる。

対策として、生の kill の前に `p.Signal(syscall.Signal(0))` を通す。Go は
Wait4 を呼ぶ前に「済み」の印を立てており、その理由がソースに書かれている。

```
$(go env GOROOT)/src/os/exec_unix.go の pidWait
	// Mark the process done now, before the call to Wait4,
	// so that Process.pidSignal will not send a signal.
	p.doRelease(statusDone)
	// Acquire a write lock on sigMu to wait for any
	// active call to the signal method to complete.
```

`pidSignal` はこの印を見て `ErrProcessDone` を返す(`os/exec_unix.go:104`)。
darwin は pidfd を持たない(`pidfdFind` が `ENOSYS`)ため、必ずこの PID 経路を
通る。

残る TOCTOU 窓(確認と送信の間)は実害無視でよい。macOS の PID は順次
割り当てで、一周(上限 99998)しない限り同じ番号は戻らない。加えて撃つ相手は
プロセスグループなので、被害が出るのは「使い回された PID のプロセスが
グループリーダーでもある」場合に限られる。

**赤は作れない**。これは競合の窓を狭める手当てで、狙って再現する手段が無い
(PID を指定してプロセスを起こすことはできない)。根拠は上のソース読みで、
テストは契約の固定にとどめた(`proc.TestKillGroupOnReapedProcess` —
reap 済みの `os.Process` に対して `os.ErrProcessDone` を返す)。

**引き換えに受け入れた点**: 上の窓で Cancel が走った場合、グループ kill は
行われない。子が上限ちょうどで終わり、かつ孫を残していた場合には孫が生き残る。
「無関係のプロセスグループを撃つ」ほうが害が大きいと判断した。ハングした
`zellij action` を抱えている本来の事例では、直接の子(bash)は孫を待って
生きているため印は立たず、この窓には入らない。

### 指摘 2: Setpgid を上限のある経路に限定(PLAUSIBLE → 採用)

4 節に書いたとおり、上限なしの 3 経路を素の `exec.Command` に戻した。
方針は proc パッケージのドキュメントコメント(「# 使いどころ」)に明記し、
`shell.TestCommandUsesProcessGroupOnlyWithTimeout` /
`zellij.TestCommandUsesProcessGroupOnlyWithTimeout` で固定した。

### 指摘 3: `runOpen` の整合(移行を採用)

`open` / `xdg-open` に 10 秒の上限を付け、`proc.Command` へ移した。`open` は
LaunchServices へ依頼して即座に返り、ブラウザは launchd が起こすため
このプロセスの子孫にはならない。つまり上限に達したときに道連れになるのは
`open` 自身とその子孫だけで、開いたブラウザは巻き込まれない。

上限が要る理由は、呼び出しがニュース画面の出す `tea.Cmd`(goroutine)の中に
あるためである。画面は固まらないが、返らなければ goroutine と子プロセスが
mdev の終了まで残る。

### 指摘 4: テストヘルパの一本化

`grandchildScript` / `readPID` / `waitProcessGone` の 3 重複を解消した。
実プロセスでの振る舞いの証明は `internal/infra/proc` の 1 本に集約し、
shell / zellij 側は「上限つきなら `proc.Command` を通す・上限なしなら
通さない」の軽量な確認に置き換えた(`Setpgid` と `Cancel` の有無を見る)。

## 9. 否定した指摘(REFUTED 3 件)

### fork と setpgid の間に kill が走ると ESRCH になる窓

**成立しない**。子の中では `setpgid` が execve より先に走る。

```
$(go env GOROOT)/src/syscall/exec_libc2.go:117   // Set process group → setpgid
$(go env GOROOT)/src/syscall/exec_libc2.go:279   // Time to exec.      → execve
```

そして親の `forkExec` は、子が execve を終える(成功なら CLOEXEC で pipe が
閉じる / 失敗ならエラーが流れてくる)まで pipe を読んで待つ。

```
$(go env GOROOT)/src/syscall/exec_unix.go:217
	// Read child error status from pipe.
```

したがって `Start` が返った時点でプロセスグループは既に存在する。
`cmd.Process` が埋まるのも `Start` の後なので、Cancel が走るときに
グループが未作成ということはない。

### `freePGID` が実在のグループと衝突する

**到達しない**。`freePGID` は 90000〜99000 を順に `kill(-pid, 0)` で試し、
`ESRCH` が返った番号(= そのグループが存在しない)だけを返す。使うのは
`killGroup` が `os.ErrProcessDone` を返すことの確認だけで、シグナルは
送っていない(シグナル 0)。仮に確認後にその番号が使われ始めても、テストは
シグナルを撃たないので害が無い。

### `ESRCH` → `os.ErrProcessDone` の変換は誤り

**変換が正しい**。`ESRCH` は「そのグループに 1 つもプロセスが残っていない」、
すなわち止めたい相手が既に居ないことを意味する。Go はこの戻り値を見て
「打ち切りに成功した」か「打ち切りに失敗した」かを決めており、
`os.ErrProcessDone` 以外を返すと `exec: canceling Context: ...` を被せて
本来の終了理由を隠してしまう。標準ライブラリ自身も同じ変換をしている
(`os/exec_unix.go` の `convertESRCH`)。
