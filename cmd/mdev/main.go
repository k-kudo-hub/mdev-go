// Command mdev は Zellij 上のコーディングエージェントセッションを統括する CLI である。
//
// このパッケージは依存の組み立て(DI)とエントリポイントのみを持ち、
// 業務ロジックは internal 以下の各パッケージに置く(ADR-0002)。
package main

import (
	"fmt"
	"os"
)

func main() {
	// フェーズ 1 のサブコマンド実装まではプレースホルダとして振る舞う。
	if _, err := fmt.Fprintln(os.Stdout, "mdev: not implemented yet"); err != nil {
		os.Exit(1)
	}
}
