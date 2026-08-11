package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// タスク作成の時間の設計値。現行 task-lib.sh の環境変数と同じ既定値である
// (CONDUCTOR_ZELLIJ_TIMEOUT / CONDUCTOR_TAB_READY_MS /
// CONDUCTOR_TASK_SETUP_BUDGET / _ZJ_POLL_MS)。
const (
	// ZellijCallTimeout は zellij 1 回の呼び出しを諦めるまでの時間。
	ZellijCallTimeout = 10 * time.Second
	// TabReadyBudget はタブ登録待ちとフォーカス検証の**それぞれ**の予算。
	TabReadyBudget = 10 * time.Second
	// TaskSetupBudget は new-tab からレイアウト適用までの全体の予算。
	TaskSetupBudget = 30 * time.Second
	// tabPollInterval は登録待ち・フォーカス検証のポーリング間隔。
	tabPollInterval = 100 * time.Millisecond
	// layoutSettleDelay はレイアウトを当てる前の待ち(現行版の sleep 0.3)。
	layoutSettleDelay = 300 * time.Millisecond
	// taskControlMinTimeout は予算が尽きていても task-control ペインに
	// 与える時間。このペインが無いとタスクタブから m / w / dd が使えず、
	// タスクとして成り立たないため、見た目の調整とは別扱いにする。
	taskControlMinTimeout = time.Second
	// taskResizeCount はタスクタブを作った直後に縮める回数。
	taskResizeCount = 30
)

// タスク作成が「ペインを 1 枚も作らずに諦めた」ことを表すエラー。
//
// 現行 create_task の rc=3 に対応する。フォーカスが Main に残ったまま
// new-pane を撃つと Main タブを割ってしまうため、確認が取れないうちは
// 何も作らずに失敗を返す(Main を壊すより中止を選ぶ)。
var (
	// ErrTabNotRegistered はタブが期限内に一覧へ現れなかったことを表す。
	ErrTabNotRegistered = errors.New("タブが期限内に登録されませんでした")
	// ErrFocusNotConfirmed はフォーカスが移ったことを確認できなかったことを表す。
	ErrFocusNotConfirmed = errors.New("タブへのフォーカスを確認できませんでした")
)

// Sleeper は待ち時間を作る。ポーリングの間隔とレイアウトの落ち着き待ちに使う。
type Sleeper interface {
	Sleep(d time.Duration)
}

// TaskControlLauncher はタスクタブ下部の操作バーを起動するコマンドを返す。
//
// Go 版では `mdev pane task-control <tab>` を呼ぶ。現行版が
// `bash $CONDUCTOR_HOME/scripts/task-control.sh <tab>` を呼んでいた位置に
// 相当する。実際のパスの解決は infra が持つ。
type TaskControlLauncher interface {
	TaskControlCommand(tab string) []string
}

// TaskSpec は作るタスクの指定である。
type TaskSpec struct {
	// Dir はタブの作業ディレクトリ。
	Dir string
	// Type は task_types のキー。TASK_TYPE として渡り、レイアウトも決める。
	Type string
	// Name はタブ名。TASK_TAB_NAME として渡る。
	Name string
	// Resume は再開するエージェントのセッション ID。空なら新規起動。
	Resume string
	// Agent は名前付きエージェント(.agents のキー)。空なら旧来の
	// 単一エージェント経路で、TASK_AGENT も渡さない。
	Agent string
}

// TaskCreateResult はタスク作成の結果である。
type TaskCreateResult struct {
	// Warning は予算切れでレイアウトを諦めたときの説明。空なら最後まで通った。
	// タブとエージェントは既に動いているため、これは失敗ではない。
	Warning string
}

// TaskCreator はタスクタブを作るユースケースである(現行 create_task 相当)。
type TaskCreator struct {
	Tabs        TabActor
	ScreenState ScreenStateRemover
	Config      ConfigLoader
	Clock       Clock
	Sleeper     Sleeper
	Launcher    TaskControlLauncher
}

// UniqueName は既存のタブと重ならないタスク名を返す。
//
// 現行 task-create-loop.sh の ensure_unique_tab_name に対応する。
// zellij の外や呼び出しの失敗では一覧が空になり、base がそのまま通る
// (現行版も `2>/dev/null` で同じ結果になる)。失敗を区別しないのは、
// ここで止めるより同名のタブを 1 つ作るほうが害が小さいためである。
func (c *TaskCreator) UniqueName(base string) string {
	names, _ := c.Tabs.QueryTabNames(ZellijCallTimeout)
	return domain.UniqueTaskName(base, names)
}

// Execute はタスクタブを作る。
//
// 順序は現行 create_task(task-lib.sh v0.7.4)と同じである。
//
//	screen-state 削除
//	  → new-tab                     (失敗ならその error を返す。ペインは 1 枚も作らない)
//	  → query-tab-names ポーリング   (期限切れなら ErrTabNotRegistered)
//	  → go-to-tab-name ポーリング    (期限切れなら ErrFocusNotConfirmed)
//	  → new-pane(task-control)      (予算切れでも最低 1 秒は試す)
//	  → resize decrease up × 30      (予算切れで打ち切り、成功として返す)
//	  → focus-previous-pane          (同上)
//	  → apply_layout(残り予算)      (結果は見ない。見た目の調整であるため)
//
// 最も重要な契約は「登録待ちかフォーカス検証に失敗したらペインを 1 枚も
// 作らない」ことである。この 2 つを飛ばして new-pane を撃つと、フォーカスが
// Main に残っている場合に Main タブを割ってしまう。
func (c *TaskCreator) Execute(env PaneEnv, spec TaskSpec) (TaskCreateResult, error) {
	config, _ := c.Config.Load()

	// 同じ名前で作り直したタブが前のタスクのスクリーン検出状態を引き継がない
	// ようにする。古い "working" は最初のポーリングで即 Stop に見え、古い
	// "blocked" は Main への不要なジャンプを起こす。
	if err := c.ScreenState.Remove(env.Session(), domain.ScreenTabSlug(spec.Name)); err != nil {
		return TaskCreateResult{}, fmt.Errorf("スクリーン検出の状態の削除に失敗しました: %w", err)
	}

	start := c.Clock.Now()
	if err := c.Tabs.NewTab(ZellijCallTimeout, spec.Name, spec.Dir,
		taskLaunchCommand(config, spec)); err != nil {
		// 復元処理がこの戻り値でタブ作成の成否を判断する。
		return TaskCreateResult{}, fmt.Errorf("タブ %s の作成に失敗しました: %w", spec.Name, err)
	}

	// new-tab の rc=0 は「サーバが受理した」であって「タブが在る」ではない
	// (zellij 0.44.1 の action はサーバ応答を 1 秒しか待たない)。名前で指す
	// コマンドはタブが一覧に出るまで撃っても無言で外れる。
	if !c.waitTabRegistered(spec.Name) {
		return TaskCreateResult{}, fmt.Errorf("%w: %s", ErrTabNotRegistered, spec.Name)
	}
	if !c.waitFocused(spec.Name) {
		return TaskCreateResult{}, fmt.Errorf("%w: %s", ErrFocusNotConfirmed, spec.Name)
	}

	// task-control ペインはタスクの中核なので、予算が尽きていても最低 1 秒は試す。
	limit := callCap(c.remaining(start))
	if limit <= 0 {
		limit = taskControlMinTimeout
	}
	_ = c.Tabs.NewPane(limit, "down", spec.Dir, c.Launcher.TaskControlCommand(spec.Name))

	// ここから下は見た目の調整で、タブとしては既に機能している。
	// 予算切れなら諦めて成功として返す。
	//
	// 残り予算は 1 ステップにつき 1 回だけ測り、判定と実際に渡す上限で同じ値を
	// 使う。2 回測ると、その間に時間が進んで「判定は通ったのに渡した上限は
	// 0 以下」という食い違いが起こりうる。
	for range taskResizeCount {
		step := callCap(c.remaining(start))
		if step <= 0 {
			return TaskCreateResult{Warning: setupBudgetWarning(spec.Name)}, nil
		}
		_ = c.Tabs.Resize(step, "decrease", "up")
	}
	if limit = callCap(c.remaining(start)); limit <= 0 {
		return TaskCreateResult{Warning: setupBudgetWarning(spec.Name)}, nil
	}
	_ = c.Tabs.FocusPreviousPane(limit)

	// 残り予算を使い切っていたらレイアウトには入らない。
	//
	// ApplyLayout の「0 以下 = 無制限」は**呼び出し側が予算を指定しなかった**
	// ことを表す約束であって、「使い切った」ではない。使い切ったあとの残り
	// (0 以下)をそのまま渡すと意味が反転し、1 回あたり最大 10 秒かかりうる
	// レイアウト操作が警告も無しに最後まで走ってしまう。ここから渡すのは
	// 必ず正の値に限る。
	remaining := c.remaining(start)
	if remaining <= 0 {
		return TaskCreateResult{Warning: setupBudgetWarning(spec.Name)}, nil
	}
	// レイアウトの成否はタブ作成の成否を上書きしない。
	warning := c.ApplyLayout(config, spec.Dir, spec.Type, remaining)
	return TaskCreateResult{Warning: warning}, nil
}

// remaining はタスク作成全体の予算の残りを返す。使い切っていれば 0 以下になる。
func (c *TaskCreator) remaining(start time.Time) time.Duration {
	return TaskSetupBudget - c.elapsedSince(start)
}

// callCap は残り予算を 1 回の呼び出しの上限へ丸める。
// 残りが尽きていれば 0 以下がそのまま返り、呼び出し側はコマンドを撃たない。
func callCap(remaining time.Duration) time.Duration {
	if remaining > ZellijCallTimeout {
		return ZellijCallTimeout
	}
	return remaining
}

// ApplyLayout は task_types.<type>.layout の操作を順に当てる。
//
// budget が正のときは、その時間を超えた時点で残りを打ち切って警告を返す
// (劣化サーバで 1 コマンドずつ上限ぶん待たされ、数分固まるのを防ぐ)。
// 0 以下なら無制限で、現行版が第 3 引数を省略した場合に対応する。
//
// 戻り値は打ち切ったときの説明である。レイアウトは見た目の調整なので、
// 途中で諦めても呼び出し側は失敗として扱わない。
func (c *TaskCreator) ApplyLayout(config domain.Config, dir, taskType string, budget time.Duration) string {
	steps := config.Layout(taskType)
	if len(steps) == 0 {
		return ""
	}

	start := c.Clock.Now()
	// ペインが並び終わる前に次の操作を撃つと当たり所がずれる。
	c.Sleeper.Sleep(layoutSettleDelay)

	for _, step := range steps {
		// resize だけは amount 回まわるため、繰り返しの中でも予算を見る。
		repeat := 1
		if step.Action == layoutActionResize {
			repeat = step.Amount
		}
		for range repeat {
			limit := budgetCap(c.elapsedSince(start), budget)
			if limit <= 0 {
				return layoutBudgetWarning(taskType)
			}
			c.applyStep(limit, dir, step)
		}
	}
	return ""
}

// レイアウトの action 名。現行 apply_layout の case 文に対応する。
// これ以外の値は黙って読み飛ばされる。
const (
	layoutActionNewPane           = "new-pane"
	layoutActionMoveFocus         = "move-focus"
	layoutActionFocusPreviousPane = "focus-previous-pane"
	layoutActionResize            = "resize"
)

// applyStep は 1 ステップぶんの操作を撃つ。
// 戻り値は見ない(現行版も各アクションの rc を無視している)。
func (c *TaskCreator) applyStep(limit time.Duration, dir string, step domain.LayoutStep) {
	switch step.Action {
	case layoutActionNewPane:
		var command []string
		if step.Command != "" {
			command = []string{step.Command}
		}
		_ = c.Tabs.NewPane(limit, step.Direction, dir, command)
	case layoutActionMoveFocus:
		_ = c.Tabs.MoveFocus(limit, step.Direction)
	case layoutActionFocusPreviousPane:
		_ = c.Tabs.FocusPreviousPane(limit)
	case layoutActionResize:
		_ = c.Tabs.Resize(limit, step.Direction)
	}
}

// waitTabRegistered はタブ名が一覧に現れるまで待つ。
// 期限内に現れなければ false を返す。
func (c *TaskCreator) waitTabRegistered(name string) bool {
	return c.pollUntil(func() bool {
		// 問い合わせの失敗は「まだ見えていない」と同じ扱いでよい。期限まで
		// 何度も引き直すため、1 回の失敗で諦める必要が無い。
		names, _ := c.Tabs.QueryTabNames(ZellijCallTimeout)
		for _, existing := range names {
			if existing == name {
				return true
			}
		}
		return false
	})
}

// waitFocused はフォーカスが name のタブへ移るまで再試行する。
//
// go-to-tab-name は存在しないタブ名でも rc=0 の無言 no-op になるため、
// 「移った」ことの確認は TabActor の実装(stdout の有無)に委ねている。
func (c *TaskCreator) waitFocused(name string) bool {
	return c.pollUntil(func() bool {
		return c.Tabs.FocusTabVerified(ZellijCallTimeout, name)
	})
}

// pollUntil は check が真を返すまで TabReadyBudget の間ポーリングする。
//
// 判定は 1 回試してから期限を見る順で行う。健全なサーバでは 1 往復で
// 終わり、追加の待ちも呼び出しも発生しない(test.sh 17b8 が固定している)。
func (c *TaskCreator) pollUntil(check func() bool) bool {
	start := c.Clock.Now()
	for {
		if check() {
			return true
		}
		if c.elapsedSince(start) >= TabReadyBudget {
			return false
		}
		c.Sleeper.Sleep(tabPollInterval)
	}
}

// elapsedSince は start からの経過時間を返す。
func (c *TaskCreator) elapsedSince(start time.Time) time.Duration {
	return c.Clock.Now().Sub(start)
}

// budgetCap は「残り予算」と「1 回の上限」の小さいほうを返す。
//
// 現行 task-lib.sh の `_zj_budget_cap` に対応する。予算切れなら 0 以下を返し、
// 呼び出し側はそれ以上コマンドを撃たない。
//
// budget が 0 以下のときだけは「予算の指定が無い」= 無制限として 1 回の上限を
// そのまま返す。**使い切って 0 以下になった残りをここへ渡してはならない**。
// 意味が反転して打ち切りが効かなくなる。呼び出し側は「使い切った」と
// 「指定が無い」を自分で区別してから渡すこと(Execute は正の値しか渡さない)。
func budgetCap(elapsed, budget time.Duration) time.Duration {
	if budget <= 0 {
		return ZellijCallTimeout
	}
	return callCap(budget - elapsed)
}

// taskLaunchCommand は new-tab に渡すコマンド行を組み立てる。
//
//	env TASK_TAB_NAME=<name> TASK_TYPE=<type> [TASK_AGENT=<agent>] <エージェント> [<resume 引数> <id>]
//
// TASK_AGENT を付けるのは名前付きエージェントのときだけである。旧来の
// 単一エージェント経路のタブは環境変数もそのままにしておき、そこから
// 書かれる pending もエージェント無しのまま(下流は claude として扱う)にする。
func taskLaunchCommand(config domain.Config, spec TaskSpec) []string {
	command := []string{"env", "TASK_TAB_NAME=" + spec.Name, "TASK_TYPE=" + spec.Type}
	if spec.Agent != "" {
		command = append(command, "TASK_AGENT="+spec.Agent)
	}
	command = append(command, config.AgentCommand(spec.Agent)...)
	if spec.Resume != "" {
		command = append(command, config.AgentResumeArgs(spec.Agent)...)
		command = append(command, spec.Resume)
	}
	return command
}

// setupBudgetWarning は予算切れでタブの整形を諦めたときの説明を返す。
func setupBudgetWarning(name string) string {
	return fmt.Sprintf("タスク作成の予算(%s)を使い切ったため、タブ %s の残りのレイアウトを省きました",
		TaskSetupBudget, name)
}

// layoutBudgetWarning は予算切れでレイアウトを諦めたときの説明を返す。
func layoutBudgetWarning(taskType string) string {
	return fmt.Sprintf("予算を使い切ったため、%s のレイアウトの残りを省きました", taskType)
}
