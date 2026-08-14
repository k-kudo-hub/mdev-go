package tui_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/tui"
)

// task-control モデルのキー操作を確かめる。ユースケースの中身
// (何を消すか・何を書くか)は internal/app のテストが持つ。

type stubTaskControl struct {
	text string
	// refreshErr を入れると Refresh が失敗する。
	refreshErr error
	prep       app.DeletePreparation
	prepErr    error
	commitErr  error
	toggleErr  error

	calls []string
}

var _ tui.TaskControlService = (*stubTaskControl)(nil)

func (s *stubTaskControl) Refresh(_ app.PaneEnv, tab string) (string, error) {
	s.calls = append(s.calls, "refresh "+tab)
	if s.refreshErr != nil {
		return "", s.refreshErr
	}
	return s.text, nil
}

func (s *stubTaskControl) GoToMain() error {
	s.calls = append(s.calls, "main")
	return nil
}

func (s *stubTaskControl) ToggleWaiting(_ app.PaneEnv, tab string) error {
	s.calls = append(s.calls, "toggle "+tab)
	return s.toggleErr
}

func (s *stubTaskControl) PrepareDelete(_ app.PaneEnv, tab string) (app.DeletePreparation, error) {
	s.calls = append(s.calls, "prepare "+tab)
	return s.prep, s.prepErr
}

func (s *stubTaskControl) CommitDelete(_ app.PaneEnv, tab string) error {
	s.calls = append(s.calls, "commit "+tab)
	return s.commitErr
}

// ForceDelete は通常の削除と **別の呼び出しとして記録する**。同じ扱いに
// すると、強制削除を選んでいないのに通ってしまう回帰を捕まえられない。
func (s *stubTaskControl) ForceDelete(_ app.PaneEnv, tab string) error {
	s.calls = append(s.calls, "force "+tab)
	return s.commitErr
}

// newTaskControl はモデルとスタブを組み立てる。
func newTaskControl(t *testing.T, pane *stubTaskControl) tui.TaskControlModel {
	t.Helper()
	return tui.NewTaskControlModel(pane, app.PaneEnv{ZellijSession: "s1"}, "my-task")
}

// pressTC はキーを 1 つ押して、届いたコマンドの結果を返す。
func pressTC(t *testing.T, m tea.Model, r rune) (tea.Model, tea.Msg) {
	t.Helper()
	next, cmd := m.Update(key(r))
	if cmd == nil {
		return next, nil
	}
	return next, cmd()
}

func TestTaskControlOnce(t *testing.T) {
	t.Parallel()

	// --once は現行版の CONDUCTOR_TASKCTL_ONCE=1 と同じ経路である。
	pane := &stubTaskControl{text: "バー\n"}
	got, err := newTaskControl(t, pane).Once()
	if err != nil {
		t.Fatalf("Once() = %v", err)
	}
	if got != "バー\n" {
		t.Errorf("Once() = %q", got)
	}
	if len(pane.calls) != 1 || pane.calls[0] != "refresh my-task" {
		t.Errorf("呼び出し = %v", pane.calls)
	}
}

func TestTaskControlInitRefreshes(t *testing.T) {
	t.Parallel()

	// 起動時に 1 回読み直して操作バーを出す。
	pane := &stubTaskControl{text: "バー\n"}
	m := newTaskControl(t, pane)
	next, _ := m.Update(m.Init()())
	if got := content(next); got != "バー\n" {
		t.Errorf("表示 = %q, want バー", got)
	}
	if want := []string{"refresh my-task"}; !equalStrings(pane.calls, want) {
		t.Errorf("呼び出し = %v, want %v", pane.calls, want)
	}
}

func TestTaskControlGoToMain(t *testing.T) {
	t.Parallel()

	pane := &stubTaskControl{}
	if _, msg := pressTC(t, newTaskControl(t, pane), 'm'); msg == nil {
		t.Fatal("m でコマンドが出ていない")
	}
	if len(pane.calls) != 1 || pane.calls[0] != "main" {
		t.Errorf("呼び出し = %v, want [main]", pane.calls)
	}
}

func TestTaskControlToggleWaiting(t *testing.T) {
	t.Parallel()

	// w は切り替えたあと読み直して表示を合わせる。
	pane := &stubTaskControl{text: "バー\n"}
	if _, msg := pressTC(t, newTaskControl(t, pane), 'w'); msg == nil {
		t.Fatal("w でコマンドが出ていない")
	}
	want := []string{"toggle my-task", "refresh my-task"}
	if !equalStrings(pane.calls, want) {
		t.Errorf("呼び出し = %v, want %v", pane.calls, want)
	}
}

func TestTaskControlToggleWaitingKeepsGoingOnFailure(t *testing.T) {
	t.Parallel()

	// 切り替えに失敗しても読み直さない(表示は変わっていないため)。
	pane := &stubTaskControl{toggleErr: errors.New("書けない")}
	if _, msg := pressTC(t, newTaskControl(t, pane), 'w'); msg == nil {
		t.Fatal("w でコマンドが出ていない")
	}
	if want := []string{"toggle my-task"}; !equalStrings(pane.calls, want) {
		t.Errorf("呼び出し = %v, want %v", pane.calls, want)
	}
}

func TestTaskControlSingleDDoesNotDelete(t *testing.T) {
	t.Parallel()

	// d だけでは削除しない。2 打鍵目の確認を待つ。
	pane := &stubTaskControl{}
	next, _ := newTaskControl(t, pane).Update(key('d'))
	if len(pane.calls) != 0 {
		t.Errorf("1 打鍵で削除している: %v", pane.calls)
	}
	if got := content(next); !strings.Contains(got, "Press d to confirm delete") {
		t.Errorf("確認の表示が出ていない: %q", got)
	}
}

func TestTaskControlDDDeletes(t *testing.T) {
	t.Parallel()

	pane := &stubTaskControl{}
	m, _ := newTaskControl(t, pane).Update(key('d'))
	m, msg := pressTC(t, m, 'd')
	if msg == nil {
		t.Fatal("dd でコマンドが出ていない")
	}
	if want := []string{"prepare my-task"}; !equalStrings(pane.calls, want) {
		t.Errorf("呼び出し = %v, want %v", pane.calls, want)
	}

	// アップロード結果が空なら待たずに削除へ進む。
	m, cmd := m.Update(msg)
	if cmd == nil {
		t.Fatal("削除の後半へ進んでいない")
	}
	m, cmd = m.Update(cmd())
	if cmd == nil {
		t.Fatal("CommitDelete が呼ばれていない")
	}
	if _, quit := m.Update(cmd()); quit == nil {
		t.Error("削除の完了でペインが終わらない")
	}
	want := []string{"prepare my-task", "commit my-task"}
	if !equalStrings(pane.calls, want) {
		t.Errorf("呼び出し = %v, want %v", pane.calls, want)
	}
}

func TestTaskControlSecondKeyOtherThanDCancels(t *testing.T) {
	t.Parallel()

	// 2 打鍵目が d 以外なら削除しない。
	pane := &stubTaskControl{}
	m, _ := newTaskControl(t, pane).Update(key('d'))
	if _, msg := pressTC(t, m, 'x'); msg == nil {
		t.Fatal("読み直しが出ていない")
	}
	for _, call := range pane.calls {
		if strings.HasPrefix(call, "prepare") {
			t.Errorf("削除へ進んでいる: %v", pane.calls)
		}
	}
}

func TestTaskControlUploadFailureCancelsDeletion(t *testing.T) {
	t.Parallel()

	// アップロードに失敗したら何も消さず、理由を出して元へ戻る。
	pane := &stubTaskControl{prep: app.DeletePreparation{Cancelled: true}}
	m, _ := newTaskControl(t, pane).Update(key('d'))
	m, msg := pressTC(t, m, 'd')
	if msg == nil {
		t.Fatal("dd でコマンドが出ていない")
	}
	next, cmd := m.Update(msg)
	// **時間で消さない。** 理由を読んで判断してもらうための表示なので、
	// タイマーは張らない(2 秒で流れると何が起きたのか分からない)。
	if cmd != nil {
		t.Error("中止の通知にタイマーが張られている")
	}
	if got := content(next); !strings.Contains(got, "Upload failed. Deletion cancelled.") {
		t.Errorf("中止の表示が出ていない: %q", got)
	}
	for _, call := range pane.calls {
		if strings.HasPrefix(call, "commit") {
			t.Errorf("中止なのに削除している: %v", pane.calls)
		}
	}
}

func TestTaskControlShowsUploadURLBeforeClosing(t *testing.T) {
	t.Parallel()

	// タブが閉じる前にログの URL を確認できるよう、少し出してから削除する。
	pane := &stubTaskControl{prep: app.DeletePreparation{Message: "https://example.test/log"}}
	m, _ := newTaskControl(t, pane).Update(key('d'))
	m, msg := pressTC(t, m, 'd')
	if msg == nil {
		t.Fatal("dd でコマンドが出ていない")
	}
	next, cmd := m.Update(msg)
	if cmd == nil {
		t.Fatal("待ちのタイマーが張られていない")
	}
	if got := content(next); !strings.Contains(got, "https://example.test/log") {
		t.Errorf("URL が出ていない: %q", got)
	}
}

func TestTaskControlPromptTimeoutIsTwoSeconds(t *testing.T) {
	t.Parallel()

	// Dashboard の 2 打鍵目(3 秒)とは別の値である。現行版も
	// task-control.sh だけ read -t 2 になっている。
	if tui.TaskControlPromptTimeout != 2*time.Second {
		t.Errorf("TaskControlPromptTimeout = %v, want 2s", tui.TaskControlPromptTimeout)
	}
	if tui.PromptTimeout == tui.TaskControlPromptTimeout {
		t.Error("Dashboard と同じ値になっている(別々に決まっているべき)")
	}
	if tui.TaskControlInterval != 2*time.Second {
		t.Errorf("TaskControlInterval = %v, want 2s", tui.TaskControlInterval)
	}
}

func TestTaskControlShowsRefreshError(t *testing.T) {
	t.Parallel()

	pane := &stubTaskControl{refreshErr: errors.New("読めない")}
	m := newTaskControl(t, pane)
	next, _ := m.Update(m.Init()())
	if got := content(next); !strings.Contains(got, "読めない") {
		t.Errorf("エラーが出ていない: %q", got)
	}
}

func TestTaskControlQuitsOnCtrlC(t *testing.T) {
	t.Parallel()

	if _, cmd := newTaskControl(t, &stubTaskControl{}).Update(ctrlKey('c')); cmd == nil {
		t.Error("Ctrl+C で終了しない")
	}
}

// equalStrings は 2 つの文字列の並びが同じかを返す。
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestTaskControlShowsUploadFailureReason は中止の理由を画面へ出すことを
// 確かめる。Dashboard と同じ扱いにする(両方の経路で同じだけ分かるように)。
func TestTaskControlShowsUploadFailureReason(t *testing.T) {
	t.Parallel()

	const reason = "ログリポジトリへのpushに失敗しました: 認証エラー"
	pane := &stubTaskControl{prep: app.DeletePreparation{Cancelled: true, Reason: reason}}
	m, _ := newTaskControl(t, pane).Update(key('d'))
	m, msg := pressTC(t, m, 'd')
	if msg == nil {
		t.Fatal("dd でコマンドが出ていない")
	}
	next, _ := m.Update(msg)

	got := content(next)
	if !strings.Contains(got, "Upload failed. Deletion cancelled.") {
		t.Errorf("中止の表示が出ていない: %q", got)
	}
	if !strings.Contains(got, reason) {
		t.Errorf("理由が出ていない: %q", got)
	}
}

// TestTaskControlForceDeleteAfterCancel は中止の後に強制削除を選べることを
// 確かめる(Dashboard と同じ契約)。
func TestTaskControlForceDeleteAfterCancel(t *testing.T) {
	t.Parallel()

	const reason = "会話要約の生成に失敗しました: transcript のパスが記録されていません"
	pane := &stubTaskControl{prep: app.DeletePreparation{Cancelled: true, Reason: reason}}
	m, _ := newTaskControl(t, pane).Update(key('d'))
	m, msg := pressTC(t, m, 'd')
	if msg == nil {
		t.Fatal("dd でコマンドが出ていない")
	}
	shown, _ := m.Update(msg)

	got := content(shown)
	if !strings.Contains(got, reason) {
		t.Errorf("理由が出ていない: %q", got)
	}
	if !strings.Contains(got, "アップロードせずに削除") {
		t.Errorf("強制削除の案内が出ていない: %q", got)
	}

	forced, cmd := shown.Update(key('!'))
	if cmd == nil {
		t.Fatal("強制削除のコマンドが出ていない")
	}
	forced, run := forced.Update(cmd())
	if run == nil {
		t.Fatal("削除の実行コマンドが出ていない")
	}
	forced.Update(run())

	var forcedCall, commitCall bool
	for _, call := range pane.calls {
		switch {
		case strings.HasPrefix(call, "force "):
			forcedCall = true
		case strings.HasPrefix(call, "commit "):
			commitCall = true
		}
	}
	if !forcedCall {
		t.Errorf("強制削除が呼ばれていない: %v", pane.calls)
	}
	if commitCall {
		t.Errorf("通常の削除も呼ばれた: %v", pane.calls)
	}
}

// TestTaskControlForceOfferClearedByOtherKey は他のキーで提示が消えることを
// 確かめる。
func TestTaskControlForceOfferClearedByOtherKey(t *testing.T) {
	t.Parallel()

	pane := &stubTaskControl{prep: app.DeletePreparation{Cancelled: true, Reason: "だめでした"}}
	m, _ := newTaskControl(t, pane).Update(key('d'))
	m, msg := pressTC(t, m, 'd')
	shown, _ := m.Update(msg)

	cleared, _ := shown.Update(key('m'))
	forced, _ := cleared.Update(key('!'))
	_ = forced

	for _, call := range pane.calls {
		if strings.HasPrefix(call, "force") {
			t.Errorf("提示が消えた後に強制削除が走った: %v", pane.calls)
		}
	}
}

// TestUploadFailedNoticeIndentsContinuationLines は 2 行目以降にも字下げが
// 入ることを確かめる。
//
// 呼び出し側は先頭行にしか字下げを付けないため、ここで入れないと 2 行目
// だけが左端に寄って読みにくくなる。
func TestUploadFailedNoticeIndentsContinuationLines(t *testing.T) {
	t.Parallel()

	pane := &stubTaskControl{prep: app.DeletePreparation{Cancelled: true, Reason: "理由の行"}}
	m, _ := newTaskControl(t, pane).Update(key('d'))
	m, msg := pressTC(t, m, 'd')
	shown, _ := m.Update(msg)

	for i, line := range strings.Split(strings.TrimRight(content(shown), "\n"), "\n") {
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("%d 行目に字下げが無い: %q", i+1, line)
		}
	}
}
