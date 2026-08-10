// Package proc は外部コマンドの起動方法を 1 か所に集める。
// internal/infra のサブパッケージであり、app / domain には依存しない(ADR-0002)。
//
// ここが解く問題は 1 つだけである。exec.CommandContext の既定の打ち切りは
// 直接の子プロセス 1 個への SIGKILL でしかなく、その子が spawn した孫には
// 届かない。mdev が呼ぶのは bash スクリプトで、スクリプトはさらに
// `zellij action ...` を起こすため、上限で切っても孫がそのまま残る。
// 実環境ではハングした `zellij action` が 200 個超まで蓄積し、うち 2 個が
// 100% CPU で空転してマシン全体を劣化させた。zellij サーバの劣化は
// タブ遷移の取りこぼしを悪化させるため、これは増幅ループになる。
package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	// syscall を使うのは、exec.Cmd.SysProcAttr の型が *syscall.SysProcAttr で
	// 決め打ちされており golang.org/x/sys/unix では代替できないためである。
	// depguard の許可リストでは $gostd に含まれる。
	"syscall"
	"time"
)

// waitDelay は打ち切り後の後始末に与える猶予である。
//
// プロセスグループごと SIGKILL すればパイプは直ちに閉じるので、通常この猶予は
// 使われない。効くのは孫が setsid などで自分のグループを作りグループ外へ
// 逃げた場合で、Go はここで標準出力のパイプを強制的に閉じて Wait を返す。
// 猶予が無い(0)と Wait は逃げた孫が終わるまで永久に返らず、ポーリングの
// チェーンがそこで止まる。上限のうち最も短いもの(zellij CLI の 10 秒)に対して
// 十分小さい値を選んだ。
const waitDelay = 2 * time.Second

// Command は ctx の終了でプロセスグループごと止まる exec.Cmd を返す。
//
// 打ち切りの範囲を「子 1 個」から「子とその子孫すべて」へ広げるために、
// 子を新しいプロセスグループのリーダーにしたうえで(Setpgid)、打ち切りの
// シグナルをそのグループ全体へ送る。
//
// ctx が打ち切られない context(context.Background() など)の場合、Go の
// os/exec は Cancel も WaitDelay も参照しない(Start が watchCtx を起動する
// 条件が `ctx.Done() != nil` であるため)。上限を設けない呼び出しの挙動は
// Setpgid を除いて従来と変わらない。
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	// Setpgid は子を新しいプロセスグループのリーダーにする。子が spawn した
	// 孫は既定でこのグループを引き継ぐため、グループを 1 つ潰せば子孫ごと消える。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process) }
	cmd.WaitDelay = waitDelay
	return cmd
}

// killGroup は p が率いるプロセスグループ全体に SIGKILL を送る。
//
// 負の PID を渡す kill は「その絶対値を ID に持つプロセスグループ全体」への
// 送信を意味する(kill(2))。Command が Setpgid を付けているためグループ ID は
// 子の PID と等しい。
//
// SIGTERM ではなく SIGKILL を使うのは、止めたい相手が既にハングしていて
// シグナルハンドラが動かない前提だからである(既定の打ち切りも SIGKILL)。
func killGroup(p *os.Process) error {
	// Cancel が呼ばれるのは起動に成功した後だけなので、通常ここは nil にならない。
	if p == nil {
		return os.ErrProcessDone
	}

	err := syscall.Kill(-p.Pid, syscall.SIGKILL)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.ESRCH):
		// グループに 1 つもプロセスが残っていない = 既に終わっている。
		// os.ErrProcessDone を返すと Go は「打ち切りではなく自然終了」として扱う。
		return os.ErrProcessDone
	default:
		// グループへ送れない場合(権限など)でも、せめて直接の子だけは止める。
		return p.Kill()
	}
}
