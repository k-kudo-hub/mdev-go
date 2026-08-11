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
//
// # 使いどころ
//
// **実行時間の上限を持つ呼び出しだけがこのパッケージを使う。**
//
// 上限を持たない呼び出し(要約の生成・ニュースの取得・git の clone や push・
// install.sh の実行)は素の exec.Command のままにする。プロセスグループを分けると、mdev を抱えている
// 端末が閉じたときにカーネルが送る SIGHUP が子へ連鎖しなくなるためである
// (SIGHUP はフォアグラウンドのプロセスグループへ届く)。上限を持つ呼び出しは
// 高々その上限で自分から片付くので分けてよいが、上限の無い呼び出しは
// 連鎖が切れると端末が消えた後も残り続ける。
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
// ctx は打ち切られるものを渡すこと(パッケージのコメントの「使いどころ」を
// 参照)。打ち切られない context を渡した場合、Go の os/exec は Cancel も
// WaitDelay も参照しないため(Start が watchCtx を起動する条件が
// `ctx.Done() != nil` である)、プロセスグループを分ける副作用だけが残る。
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

	// 生の kill を撃つ前に、os.Process 越しにシグナル 0(送信はせず生存確認だけを
	// する印)を通す。reap 済みの PID を使い回した無関係のプロセスグループを
	// 撃たないための手当てである。
	//
	// Go の os/exec は Wait4 を呼ぶ前に「済み」の印を立てる。理由はソースに
	// 書かれている(os/exec_unix.go の pidWait: "Mark the process done now,
	// before the call to Wait4, so that Process.pidSignal will not send a
	// signal.")。os.Process.Signal はその印を見て ErrProcessDone を返し、印を
	// 立てる側は sigMu の書き込みロックで送信中の Signal の完了を待つ。つまり
	// os.Process 越しなら reap 済みの相手へシグナルが飛ぶことはない。
	// syscall.Kill を直に呼ぶとこの印が見えない。
	if err := p.Signal(syscall.Signal(0)); err != nil {
		// 相手が居ない場合の ESRCH もここで os.ErrProcessDone に変換済みである
		// (os/exec_unix.go の convertESRCH)。
		return err
	}

	// 以下の kill は sigMu の外にあるため、確認と送信の間に相手が終わって PID が
	// 使い回される窓が理屈上は残る。実害は無視してよい。macOS の PID は順次
	// 割り当てで、一周(既定の上限は 99998)しない限り同じ番号は戻ってこない。
	// さらに撃つ相手はプロセスグループなので、被害が出るのは「使い回された PID の
	// プロセスがグループリーダーでもある」場合に限られる。
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
