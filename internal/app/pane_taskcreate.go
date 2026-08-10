package app

import (
	"fmt"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// DirLister は候補となるディレクトリを列挙する。
//
// 現行 task-create-loop.sh が `fd --type d --max-depth <depth> . <roots...>` で
// 行っていたところに対応する。fd への外部依存を無くすため実装は自前で掘る。
type DirLister interface {
	// ListDirs は roots 配下のディレクトリを深さ depth まで返す。
	// 読めない root は飛ばす(エラーにしない)。
	ListDirs(roots []string, depth int) []string
	// IsDir は path が実在するディレクトリかどうかを返す。
	// 現行版が起点を選ぶときに使う `[[ -d "$expanded" ]]` に対応する。
	IsDir(path string) bool
}

// TaskTypeChoice は選択 UI に並べるタスク種別 1 件である。
//
// tui は domain を参照できない(ADR-0002)ため、表示に要るものだけを
// app の型として渡す。
type TaskTypeChoice struct {
	// Name は task_types のキー。TASK_TYPE として渡る。
	Name string
	// Description は設定の description。
	Description string
}

// TaskCreatePane はタスク作成ペインのユースケースである
// (現行 task-create-loop.sh 相当)。
type TaskCreatePane struct {
	Config  ConfigLoader
	Dirs    DirLister
	Creator *TaskCreator
	// Home は search_dirs の先頭の `~` を展開するホームディレクトリ。
	Home string
}

// Menu は待ち受け画面を返す。
func (p *TaskCreatePane) Menu(env PaneEnv) string {
	return domain.RenderTaskCreateMenu(env.Session())
}

// Directories は候補のディレクトリと、起点が 1 つでもあったかを返す。
//
// search_dirs のうち先頭の `~` を展開して実在するものだけを起点にし、その配下を
// search_depth まで掘る。
//
// 起点の有無と候補の有無は別に返す。現行版もこの 2 つを区別しており、
// 起点が 1 つも無ければ赤字を出して 2 秒待つが、起点はあるのに候補が
// 0 件のときは(fzf が何も選ばずに終わって)黙ってメニューへ戻る。
func (p *TaskCreatePane) Directories() (dirs []string, rootsFound bool) {
	config, _ := p.Config.Load()
	roots := make([]string, 0, len(config.SearchDirs))
	for _, dir := range config.SearchDirs {
		expanded := domain.ExpandHome(dir, p.Home)
		if !p.Dirs.IsDir(expanded) {
			continue
		}
		roots = append(roots, expanded)
	}
	if len(roots) == 0 {
		return nil, false
	}
	return p.Dirs.ListDirs(roots, config.SearchDepth()), true
}

// TaskTypes は選択肢に並べるタスク種別を設定の記述順で返す。
func (p *TaskCreatePane) TaskTypes() []TaskTypeChoice {
	config, _ := p.Config.Load()
	choices := make([]TaskTypeChoice, 0, len(config.TaskTypes))
	for _, t := range config.TaskTypes {
		choices = append(choices, TaskTypeChoice{Name: t.Name, Description: t.Description})
	}
	return choices
}

// Agents は設定済みエージェントの名前を記述順で返す。
//
// 空なら旧来の単一エージェント経路を使う(選択 UI を出さず、TASK_AGENT も
// 渡さない)。1 件なら呼び出し側が選択 UI を出さずに即決する。
func (p *TaskCreatePane) Agents() []string {
	config, _ := p.Config.Load()
	return config.AgentNames()
}

// SkipNameInput はタスク名の入力を省く設定かどうかを返す。
func (p *TaskCreatePane) SkipNameInput() bool {
	config, _ := p.Config.Load()
	return config.SkipTaskNameInput
}

// DefaultName はディレクトリと種別から既定のタスク名を返す。
func (p *TaskCreatePane) DefaultName(dir, taskType string) string {
	return domain.DefaultTaskName(dir, taskType)
}

// ResolveName は入力されたタスク名を解決する(空なら既定名)。
func (p *TaskCreatePane) ResolveName(defaultName, input string) string {
	return domain.ResolveTaskName(defaultName, input)
}

// UniqueName は既存のタブと重ならないタスク名を返す。
func (p *TaskCreatePane) UniqueName(base string) string {
	return p.Creator.UniqueName(base)
}

// Create はタスクタブを作る。
//
// 戻り値の警告は「タブは作れたが予算切れでレイアウトを省いた」ことを表す。
// 失敗(error)のときはペインが 1 枚も作られていない(Main は無傷である)。
func (p *TaskCreatePane) Create(env PaneEnv, dir, taskType, name, agent string) (string, error) {
	result, err := p.Creator.Execute(env, TaskSpec{
		Dir: dir, Type: taskType, Name: name, Agent: agent,
	})
	if err != nil {
		return "", fmt.Errorf("タスクの作成に失敗しました: %w", err)
	}
	return result.Warning, nil
}

// FilterCandidates は選択 UI の絞り込みを行う。
//
// tui は domain を参照できない(ADR-0002)ため、純粋関数への薄い入口を
// app に置く。判定そのものは domain.FilterCandidates が持つ。
func FilterCandidates(items []string, query string) []string {
	return domain.FilterCandidates(items, query)
}

// TaskCreateError はタスク作成ペインのエラー行を組み立てる。
func TaskCreateError(message string) string {
	return domain.RenderTaskCreateError(message)
}

// SearchDirsMissingMessage は search_dirs が 1 つも実在しないときの文言である。
const SearchDirsMissingMessage = domain.TaskCreateSearchDirsMissing
