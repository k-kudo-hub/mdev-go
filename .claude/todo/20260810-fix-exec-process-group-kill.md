# exec のタイムアウトをプロセスグループごと kill に強化する

## 概要

PR #6 で外部コマンドに `exec.CommandContext` の実行時間上限を付けたが、
タイムアウト時の kill が**直接の子(bash)にしか届かない**。bash が spawn した
孫プロセス(`zellij action ...`)は生き残る。

実環境ではハングした `zellij action` が 200 個超蓄積し、うち 2 個が 100% CPU で
空転してマシン全体を劣化させた。zellij サーバの劣化はタブ遷移レースを
悪化させるため、これは増幅ループになっている。

## 調査で確定した事実

- タイムアウトを持つ呼び出しは 4 経路
  - `internal/infra/shell/runner.go` の `ScreenDetectTick`(15 秒)/ `RestoreSession`(60 秒)
  - `internal/infra/zellij/tabs.go` の `ListTabs`(10 秒、`commandOutput`)
  - `internal/infra/zellij/focuser.go` の `FocusTab` / `CloseTabByID`(10 秒、`runCommand`)
- タイムアウトを持たない呼び出しは 3 経路(`UploadLog` / `RestoreTask` / `FetchNews`)
- `exec.CommandContext` の既定 Cancel は `cmd.Process.Kill()`、すなわち直接の子 1 個への
  SIGKILL のみ。孫には届かない
- Go 1.25 の `os/exec` は `(c.Cancel != nil || c.WaitDelay != 0) && c.ctx != nil && c.ctx.Done() != nil`
  のときだけ watchCtx を起動する(`$(go env GOROOT)/src/os/exec/exec.go:772`)。
  上限なし経路が渡す `context.Background()` は `Done() == nil` なので、
  Cancel も WaitDelay も一切効かない = 挙動は現状維持になる

## TODO

- [x] 孫プロセスの生存を検証する赤テストを作成(TERM を無視して sleep する孫を
      spawn するスクリプトを短い上限で実行 → 上限後に孫が存在しないこと)
- [x] 修正前にこのテストが赤(孫が生き残る)であることを実測して記録
- [x] `internal/infra/proc` に共通ヘルパを作成(Setpgid / プロセスグループへの
      SIGKILL / WaitDelay)
- [x] `internal/infra/shell/runner.go` の `runCommand` をヘルパ経由にする
- [x] `internal/infra/zellij` の `runCommand` / `commandOutput` をヘルパ経由にする
- [x] 上限なし経路(UploadLog / RestoreTask / FetchNews)の挙動が変わらないことを
      テストで固定する
- [x] lint(depguard)で `syscall` が弾かれないことを確認し、使う理由をコメントに残す
- [x] ゴールデン 23 ケースが不変であることを確認
- [x] evidence(`.claude/todo/20260810-pgkill-evidence.md`)に赤→緑の記録を残す
- [x] `make check` 緑

## コードレビュー指摘への対応(4 件)

- [x] 指摘 1: Cancel の PID 使い回しガード(生の kill の前に os.Process 越しの
      シグナル 0 を通す。根拠は os/exec_unix.go の pidWait のコメント)
- [x] 指摘 2: Setpgid を上限のある経路に限定し、上限なし経路は素の exec.Command に
      戻す(端末が閉じたときの SIGHUP 連鎖を保つ)。方針を proc のドキュメントに明記
- [x] 指摘 3: runOpen に 10 秒の上限を付けて proc.Command へ移す
- [x] 指摘 4: テストヘルパの三重複を解消(実プロセスの証明は proc に集約し、
      shell / zellij は proc.Command を通していることの軽量な確認に置き換える)
- [x] evidence に対応記録と REFUTED 3 件の要旨を追記

## 完了条件

- タイムアウトを持つ全経路で、孫プロセスがタイムアウト後に消えることがテストで固定されている
- 上限なし経路の挙動が変わっていない
- ゴールデン 23 全通過・`make check` 緑

## 備考

- 対象は macOS(darwin)。CI も macos-latest
- `exec.Cmd.SysProcAttr` の型が `*syscall.SysProcAttr` のため `golang.org/x/sys/unix` では
  代替できない。`syscall` は depguard の `$gostd` に含まれる
