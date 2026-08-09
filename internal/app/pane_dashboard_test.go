package app_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// errUploadFailed は upload-log.sh が非 0 で終わった状況を表す。
var errUploadFailed = errors.New("upload-log が失敗した")

// fakeRecorder は RecordOutput の代わりに呼び出しだけを記録する。
type fakeRecorder struct {
	journal *paneJournal
	tabs    []string
	err     error
}

var _ app.TaskRecorder = (*fakeRecorder)(nil)

func (f *fakeRecorder) Execute(tab string, _ app.RecordEnv) error {
	f.tabs = append(f.tabs, tab)
	f.journal.add("record " + tab)
	return f.err
}

// dashboardFixture は Dashboard ユースケースと観測用の fake をまとめて組み立てる。
type dashboardFixture struct {
	pane     *app.DashboardPane
	journal  *paneJournal
	tabs     *fakeTabLister
	closer   *fakeTabCloser
	pending  *fakePendingLister
	remover  *fakePendingRemover
	registry *fakeRegistryRemover
	screen   *fakeScreenStateRemover
	focuser  *fakePaneFocuser
	config   *fakeConfigLoader
	recorder *fakeRecorder
	shell    *fakeShellRunner
}

func newDashboardFixture(views []domain.PendingView, tabOutput string) *dashboardFixture {
	journal := &paneJournal{}
	f := &dashboardFixture{
		journal:  journal,
		tabs:     &fakeTabLister{output: tabOutput},
		closer:   &fakeTabCloser{journal: journal},
		pending:  &fakePendingLister{views: map[string][]domain.PendingView{"s1": views}},
		remover:  &fakePendingRemover{journal: journal},
		registry: &fakeRegistryRemover{journal: journal},
		screen:   &fakeScreenStateRemover{journal: journal},
		focuser:  &fakePaneFocuser{journal: journal},
		config:   &fakeConfigLoader{},
		recorder: &fakeRecorder{journal: journal},
		shell:    &fakeShellRunner{journal: journal},
	}
	f.pane = &app.DashboardPane{
		Pending:     f.pending,
		Remover:     f.remover,
		Registry:    f.registry,
		ScreenState: f.screen,
		Tabs:        f.tabs,
		Closer:      f.closer,
		Focuser:     f.focuser,
		Config:      f.config,
		Recorder:    f.recorder,
		Shell:       f.shell,
	}
	return f
}

var dashboardEnv = app.PaneEnv{ZellijSession: "s1"}

func TestDashboardPaneRefresh(t *testing.T) {
	t.Parallel()

	f := newDashboardFixture([]domain.PendingView{
		{Name: "a.json", Tab: "alpha", Event: "Notification", Message: "needs permission", Time: "10:00:00"},
		{Name: "b.json", Tab: "beta", Event: "Stop", Message: "done", Time: "10:01:00"},
		{Name: "c.json", Tab: "beta", Event: "Waiting", Message: "pr review", Time: "10:02:00"},
	}, "ID POS NAME\n1 x beta\n2 x alpha\n")

	snapshot, err := f.pane.Refresh(dashboardEnv)
	if err != nil {
		t.Fatalf("Refresh() = %v", err)
	}

	// タブ順(beta, alpha)が優先され、Waiting は除かれる。
	want := []string{"beta", "alpha"}
	if !reflect.DeepEqual(snapshot.Tabs, want) {
		t.Errorf("表示順 = %v, want %v", snapshot.Tabs, want)
	}
	// 画面の文字列は domain のレンダリング結果そのものである。
	if !strings.Contains(snapshot.Text, "Current Tasks") || !strings.Contains(snapshot.Text, "[s1]") {
		t.Errorf("描画結果が入っていない: %q", snapshot.Text)
	}
}

func TestDashboardPaneRefreshRunsScreenDetectionFirst(t *testing.T) {
	t.Parallel()

	// スクリーン検出はポーリングの先頭で走らせる。pending を読む前に走らせないと、
	// その回の観測結果が一覧に反映されない(screen 方式のタスクが出てこない)。
	f := newDashboardFixture(nil, "ID POS NAME\n")
	if _, err := f.pane.Refresh(dashboardEnv); err != nil {
		t.Fatalf("Refresh() = %v", err)
	}

	if want := []string{"s1"}; !reflect.DeepEqual(f.shell.detectSessions, want) {
		t.Errorf("screen_detect_tick の呼び出し = %v, want %v", f.shell.detectSessions, want)
	}
	if len(f.journal.entries) == 0 || f.journal.entries[0] != "screen-detect-tick s1" {
		t.Errorf("先頭が screen 検出ではない: %v", f.journal.entries)
	}
}

func TestDashboardPaneRefreshUsesUnknownSessionWhenOutsideZellij(t *testing.T) {
	t.Parallel()

	f := newDashboardFixture(nil, "ID POS NAME\n")
	snapshot, err := f.pane.Refresh(app.PaneEnv{})
	if err != nil {
		t.Fatalf("Refresh() = %v", err)
	}
	// 見出しのセッション名が unknown に落ちる。
	if !strings.Contains(snapshot.Text, "["+domain.DefaultSessionName+"]") {
		t.Errorf("セッション名が %q になっていない: %q", domain.DefaultSessionName, snapshot.Text)
	}
}

func TestDashboardPaneStartupRestoresSession(t *testing.T) {
	t.Parallel()

	// 起動時に restore-session.sh を呼ぶ(issue #36)。ONCE 経路でも走る。
	f := newDashboardFixture(nil, "")
	f.pane.Startup()

	if f.shell.restoreCalls != 1 {
		t.Errorf("restore-session の呼び出し = %d, want 1", f.shell.restoreCalls)
	}
}

func TestDashboardPaneJump(t *testing.T) {
	t.Parallel()

	screenConfig := domain.Config{Agents: map[string]domain.AgentConfig{
		"codex":   {Detection: domain.DetectionScreen},
		"somecli": {
			// detection を書いていないので hooks 扱いになる。
		},
	}}

	tests := []struct {
		name string
		item domain.PendingView
		// wantCleared はジャンプで pending が消えるべきかどうか。
		wantCleared bool
	}{
		{
			// hooks も screen 検出も持たないエージェントだけがジャンプで消える。
			// 他に消す担い手がいないためである。
			name:        "hooks も screen も持たないエージェントは消す",
			item:        domain.PendingView{Name: "sc.json", Tab: "somecli-task", AgentOrDefault: "somecli"},
			wantCleared: true,
		},
		{
			// claude は hooks がライフサイクルを持つ。
			name:        "claude は消さない",
			item:        domain.PendingView{Name: "cl.json", Tab: "claude-task", AgentOrDefault: "claude"},
			wantCleared: false,
		},
		{
			// screen 方式はターンが再開するまで pending が残るのが正しい。
			// ここで消しても次のポーリングで作り直されるだけである。
			name:        "screen 方式は消さない",
			item:        domain.PendingView{Name: "cx.json", Tab: "codex-task", AgentOrDefault: "codex"},
			wantCleared: false,
		},
		{
			// agent キーが無い古い pending は claude として扱う。
			name:        "agent が無い pending は claude 扱い",
			item:        domain.PendingView{Name: "old.json", Tab: "old-task", AgentOrDefault: "claude"},
			wantCleared: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// 一覧に 1 件だけ載った状態を作り、その 1 番目へジャンプする。
			f := newDashboardFixture([]domain.PendingView{tt.item},
				"ID POS NAME\n1 x "+tt.item.Tab+"\n")
			f.config.config = screenConfig

			snapshot, err := f.pane.Refresh(dashboardEnv)
			if err != nil {
				t.Fatalf("Refresh() = %v", err)
			}
			if err := f.pane.Jump(dashboardEnv, snapshot, 1); err != nil {
				t.Fatalf("Jump() = %v", err)
			}

			if want := []string{tt.item.Tab}; !reflect.DeepEqual(f.focuser.focused, want) {
				t.Errorf("フォーカス先 = %v, want %v", f.focuser.focused, want)
			}

			cleared := len(f.remover.deletedNames) > 0
			if cleared != tt.wantCleared {
				t.Errorf("pending の削除 = %v, want %v (削除された: %v)",
					cleared, tt.wantCleared, f.remover.deletedNames)
			}
			if tt.wantCleared {
				if want := []string{"s1/" + tt.item.Name}; !reflect.DeepEqual(f.remover.deletedNames, want) {
					t.Errorf("削除された pending = %v, want %v", f.remover.deletedNames, want)
				}
			}
		})
	}
}

func TestDashboardPanePrepareDeleteRecordsThenUploads(t *testing.T) {
	t.Parallel()

	f := newDashboardFixture(nil, "")
	f.shell.uploadOutput = "https://example.com/log/1"

	prep, err := f.pane.PrepareDelete(dashboardEnv, "alpha")
	if err != nil {
		t.Fatalf("PrepareDelete() = %v", err)
	}
	if prep.Cancelled {
		t.Error("成功したのに中止扱いになっている")
	}
	if prep.Message != "https://example.com/log/1" {
		t.Errorf("Message = %q, want URL", prep.Message)
	}

	// record が先、upload が後。まだ何も消してはいけない。
	want := []string{"record alpha", "upload-log alpha"}
	if !reflect.DeepEqual(f.journal.entries, want) {
		t.Errorf("呼び出し順 = %v, want %v", f.journal.entries, want)
	}
}

func TestDashboardPanePrepareDeleteCancelsOnUploadFailure(t *testing.T) {
	t.Parallel()

	// これが削除フローのいちばん重要な契約である。upload-log.sh が非 0 で
	// 終わったら、作業ログを失わないために何一つ消してはならない。
	f := newDashboardFixture(nil, "ID POS NAME\n1 x alpha\n")
	f.shell.uploadErr = errUploadFailed

	prep, err := f.pane.PrepareDelete(dashboardEnv, "alpha")
	if err != nil {
		t.Fatalf("PrepareDelete() = %v", err)
	}
	if !prep.Cancelled {
		t.Fatal("upload が失敗したのに中止になっていない")
	}

	for _, entry := range f.journal.entries {
		switch entry {
		case "pending-delete-by-tab alpha", "registry-remove alpha":
			t.Errorf("中止したのに削除している: %v", f.journal.entries)
		}
	}
	if len(f.closer.ids) != 0 {
		t.Errorf("中止したのにタブを閉じている: %v", f.closer.ids)
	}
	if len(f.screen.removed) != 0 {
		t.Errorf("中止したのに screen-state を消している: %v", f.screen.removed)
	}
}

func TestDashboardPanePrepareDeleteWithoutUploadOutput(t *testing.T) {
	t.Parallel()

	// 出力が空 = アップロードが無効、または対象が無かった。表示するものが
	// 無いので、呼び出し側は待たずにそのまま削除へ進んでよい。
	f := newDashboardFixture(nil, "")
	prep, err := f.pane.PrepareDelete(dashboardEnv, "alpha")
	if err != nil {
		t.Fatalf("PrepareDelete() = %v", err)
	}
	if prep.Cancelled || prep.Message != "" {
		t.Errorf("PrepareDelete() = %+v, want 中止でも表示でもない", prep)
	}
}

func TestDashboardPaneCommitDeleteOrder(t *testing.T) {
	t.Parallel()

	f := newDashboardFixture(nil, "ID POS NAME\n1 x other\n7 x alpha\n")

	if err := f.pane.CommitDelete(dashboardEnv, "alpha"); err != nil {
		t.Fatalf("CommitDelete() = %v", err)
	}

	// pending → registry → screen-state → close-tab-by-id の順に消す。
	want := []string{
		"pending-delete-by-tab alpha",
		"registry-remove alpha",
		"screen-state-remove " + domain.ScreenTabSlug("alpha"),
		"close-tab-by-id 7",
	}
	if !reflect.DeepEqual(f.journal.entries, want) {
		t.Errorf("削除の順序 = %v, want %v", f.journal.entries, want)
	}
}

func TestDashboardPaneCommitDeleteWithoutResolvableTabID(t *testing.T) {
	t.Parallel()

	// id が引けなければタブは閉じない。現行 Dashboard は close-tab への
	// フォールバックを持たないため(task-control 側との非対称)、そのまま
	// 何もしないのが正しい。
	f := newDashboardFixture(nil, "ID POS NAME\n1 x other\n")

	if err := f.pane.CommitDelete(dashboardEnv, "alpha"); err != nil {
		t.Fatalf("CommitDelete() = %v", err)
	}
	if len(f.closer.ids) != 0 {
		t.Errorf("id が引けないのにタブを閉じている: %v", f.closer.ids)
	}
	// 閉じられなくても pending とレジストリの掃除は済ませる。
	if len(f.remover.deletedTabs) != 1 || len(f.registry.removed) != 1 {
		t.Errorf("掃除が行われていない: pending=%v registry=%v",
			f.remover.deletedTabs, f.registry.removed)
	}
}
