package zellij

import (
	"strings"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// TabController は zellij のタブ・ペイン操作を担当する。
//
// どのメソッドも「この 1 回を諦めるまでの時間」を引数で受ける。劣化した
// zellij サーバでは `zellij action` が返らないことがあり、35 回近い操作を
// 積み上げるタスク作成は、全体の予算から逆算した上限を 1 回ごとに渡さないと
// 数分固まる(現行 task-lib.sh の `_zj_budget_cap` に対応する)。
// 渡された値は commandTimeout(10 秒)で頭打ちにする。
type TabController struct {
	// output はコマンドの標準出力を返す。テストで差し替える。
	output func(timeout time.Duration, name string, args ...string) string
	// run はコマンドを実行する。テストで差し替える。
	run func(timeout time.Duration, name string, args ...string) error
}

var (
	_ app.TabLister = (*TabController)(nil)
	_ app.TabCloser = (*TabController)(nil)
	_ app.TabActor  = (*TabController)(nil)
)

// NewTabController は zellij コマンドを実行する TabController を返す。
//
// どの呼び出しにも実行時間の上限を付ける(commandTimeout を参照)。
// list-tabs はダッシュボードのポーリングが毎周期呼ぶため、返らなくなると
// ポーリングごと止まってしまう。
func NewTabController() *TabController {
	return &TabController{output: commandOutput, run: runCommand}
}

// ListTabs は `zellij action list-tabs` の標準出力をそのまま返す。
//
// 解釈しないのは、タブ名の取り出し(3 列目)と id の解決(先頭 2 列を落とす)で
// 規則が違い、その非対称ごと domain の純粋関数で再現しているためである。
// zellij の外で動いた場合などコマンドが失敗したときは空文字を返す
// (現行版も `2>/dev/null` で握り潰し、タブが 1 つも無い扱いにしている)。
func (c *TabController) ListTabs() string {
	return c.output(commandTimeout, binaryName, "action", "list-tabs")
}

// CloseTabByID は id のタブを閉じる。
//
// 失敗しても何も返さない。既に閉じられている場合などが該当し、いずれも
// 削除フローとしては進んでよい状態である(現行版も `2>/dev/null`)。
func (c *TabController) CloseTabByID(id string) {
	_ = c.run(commandTimeout, binaryName, "action", "close-tab-by-id", id)
}

// CloseActiveTab は今フォーカスしているタブを閉じる。
//
// task-control が id を引けなかったときのフォールバックである。id で閉じるのが
// 本筋で(同期アップロードの間に別のタブへ移っている可能性がある)、こちらは
// 「何も閉じられないよりはまし」という位置づけである。
func (c *TabController) CloseActiveTab() {
	_ = c.run(commandTimeout, binaryName, "action", "close-tab")
}

// QueryTabNames は `zellij action query-tab-names` の出力を 1 行 1 タブ名で返す。
//
// 失敗した場合は空を返す。タブ名の一意化(domain.UniqueTaskName)はこれを
// 「既存のタブが 1 つも無い」と読み、候補をそのまま使う。現行
// ensure_unique_tab_name がコマンド失敗時に元の名前を返すのと同じ結果になる。
func (c *TabController) QueryTabNames(timeout time.Duration) []string {
	out := strings.TrimRight(c.output(capTimeout(timeout), binaryName, "action", "query-tab-names"), "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// FocusTabVerified は名前でタブへフォーカスを移し、実際に移ったかを返す。
//
// `zellij action go-to-tab-name` は存在しないタブ名でも rc=0 で戻る無言の
// no-op で、終了ステータスからは成否が分からない。zellij 0.44.1(macOS)で
// 実測した唯一の差は stdout で、ヒット時のみ移動先タブの index を出力する。
// したがって「stdout 非空 = フォーカス成功」で判定する。
//
// 判定はコマンド置換と同じく末尾の改行を落としてから行う(現行版の
// `out=$(...)` + `[[ -n "$out" ]]`)。
//
// 将来の zellij がこの出力をやめた場合、フォーカスは永久に未確認となり、
// 呼び出し側(CreateTask)はペインを作らずに失敗を返す。Main を壊すより
// 中止を選ぶ方向へ倒れる。
func (c *TabController) FocusTabVerified(timeout time.Duration, name string) bool {
	out := c.output(capTimeout(timeout), binaryName, "action", "go-to-tab-name", name)
	return strings.TrimRight(out, "\n") != ""
}

// NewTab は名前と作業ディレクトリを指定してタブを作る。
//
// command が空でなければ `--` の後ろへ並べる。戻り値は zellij の終了状態で、
// 呼び出し側はこれでタブ作成の成否を判断する(復元処理が依存している)。
func (c *TabController) NewTab(timeout time.Duration, name, cwd string, command []string) error {
	args := append([]string{"action", "new-tab", "-n", name, "--cwd", cwd}, dashPrefixed(command)...)
	return c.run(capTimeout(timeout), binaryName, args...)
}

// NewPane は今のタブへペインを足す。
// command が空の場合は `--` を付けず、素のシェルが起動する。
func (c *TabController) NewPane(timeout time.Duration, direction, cwd string, command []string) error {
	args := append([]string{"action", "new-pane", "--direction", direction, "--cwd", cwd},
		dashPrefixed(command)...)
	return c.run(capTimeout(timeout), binaryName, args...)
}

// MoveFocus は方向を指定してペインのフォーカスを移す。
func (c *TabController) MoveFocus(timeout time.Duration, direction string) error {
	return c.run(capTimeout(timeout), binaryName, "action", "move-focus", direction)
}

// FocusPreviousPane は 1 つ前のペインへフォーカスを戻す。
func (c *TabController) FocusPreviousPane(timeout time.Duration) error {
	return c.run(capTimeout(timeout), binaryName, "action", "focus-previous-pane")
}

// Resize はペインの大きさを変える。
//
// 引数の数が呼び出し元で違う(create_task は `decrease up` の 2 語、
// apply_layout は direction 1 語)ため、可変長で受けてそのまま並べる。
func (c *TabController) Resize(timeout time.Duration, args ...string) error {
	return c.run(capTimeout(timeout), binaryName, append([]string{"action", "resize"}, args...)...)
}

// dashPrefixed は command が空でなければ先頭に `--` を付けて返す。
// 空なら空を返し、呼び出し側は `--` 抜きのコマンド行になる。
func dashPrefixed(command []string) []string {
	if len(command) == 0 {
		return nil
	}
	return append([]string{"--"}, command...)
}

// capTimeout は 1 回の呼び出しの上限を commandTimeout で頭打ちにする。
//
// 0 以下(予算切れ)も commandTimeout に丸める。呼び出し側は予算が尽きたら
// そもそも撃たない約束だが、撃たれた場合に無制限で待つことは無い。
func capTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 || timeout > commandTimeout {
		return commandTimeout
	}
	return timeout
}

// commandOutput は外部コマンドを実行して標準出力を返す。
// 失敗した場合(上限でプロセスグループごと切られた場合を含む)は空文字を返す。
func commandOutput(timeout time.Duration, name string, args ...string) string {
	cmd, cancel := command(timeout, name, args...)
	defer cancel()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}
