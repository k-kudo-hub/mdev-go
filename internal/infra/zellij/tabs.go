package zellij

import (
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/infra/proc"
)

// TabController は zellij のタブ一覧の取得とタブの終了を担当する。
type TabController struct {
	// output はコマンドの標準出力を返す。テストで差し替える。
	output func(name string, args ...string) string
	// run はコマンドを実行する。テストで差し替える。
	run func(name string, args ...string) error
}

var (
	_ app.TabLister = (*TabController)(nil)
	_ app.TabCloser = (*TabController)(nil)
)

// NewTabController は zellij コマンドを実行する TabController を返す。
//
// どちらの呼び出しにも実行時間の上限を付ける(commandTimeout を参照)。
// list-tabs はダッシュボードのポーリングが毎周期呼ぶため、返らなくなると
// ポーリングごと止まってしまう。
func NewTabController() *TabController {
	return &TabController{
		output: outputWithTimeout(commandTimeout),
		run:    withTimeout(commandTimeout),
	}
}

// ListTabs は `zellij action list-tabs` の標準出力をそのまま返す。
//
// 解釈しないのは、タブ名の取り出し(3 列目)と id の解決(先頭 2 列を落とす)で
// 規則が違い、その非対称ごと domain の純粋関数で再現しているためである。
// zellij の外で動いた場合などコマンドが失敗したときは空文字を返す
// (現行版も `2>/dev/null` で握り潰し、タブが 1 つも無い扱いにしている)。
func (c *TabController) ListTabs() string {
	return c.output(binaryName, "action", "list-tabs")
}

// CloseTabByID は id のタブを閉じる。
//
// 失敗しても何も返さない。既に閉じられている場合などが該当し、いずれも
// 削除フローとしては進んでよい状態である(現行版も `2>/dev/null`)。
func (c *TabController) CloseTabByID(id string) {
	_ = c.run(binaryName, "action", "close-tab-by-id", id)
}

// outputWithTimeout は上限付きでコマンドを実行し標準出力を返す関数を返す。
func outputWithTimeout(timeout time.Duration) func(name string, args ...string) string {
	return func(name string, args ...string) string { return commandOutput(timeout, name, args...) }
}

// commandOutput は外部コマンドを実行して標準出力を返す。
// 失敗した場合(上限でプロセスグループごと切られた場合を含む)は空文字を返す。
func commandOutput(timeout time.Duration, name string, args ...string) string {
	ctx, cancel := commandContext(timeout)
	defer cancel()
	out, err := proc.Command(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}
