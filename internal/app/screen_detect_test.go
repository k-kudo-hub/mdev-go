package app_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// screenDetectFixture は ScreenDetector と観測用の fake をまとめて組み立てる。
type screenDetectFixture struct {
	detector *app.ScreenDetector
	journal  *paneJournal
	panes    *fakePaneLister
	dumper   *fakeScreenDumper
	config   *fakeConfigLoader
	state    *fakeScreenStateStore
	pending  *fakePendingLister
	remover  *fakePendingRemover
	saver    *fakePendingSaver
	registry *fakeRegistryLookup
	focuser  *fakePaneFocuser
}

// codexScreenConfig は codex を screen 方式にした設定である。
// blocked パターンは fixture の 1 行にだけ当たるものを使う。
func codexScreenConfig() domain.Config {
	return domain.Config{Agents: map[string]domain.AgentConfig{
		"codex": {
			Detection: domain.DetectionScreen,
			Patterns: domain.ScreenPatterns{
				Blocked: []string{`^ *Would you like to run the following command\? *$`},
				Working: []string{`to interrupt\)`},
			},
		},
		"claude": {Detection: domain.DetectionHooks},
	}}
}

func newScreenDetectFixture(panes []app.AgentPane, dumps map[string]string) *screenDetectFixture {
	journal := &paneJournal{}
	f := &screenDetectFixture{
		journal:  journal,
		panes:    &fakePaneLister{panes: panes},
		dumper:   &fakeScreenDumper{journal: journal, dumps: dumps},
		config:   &fakeConfigLoader{config: codexScreenConfig()},
		state:    &fakeScreenStateStore{journal: journal, lines: map[string]string{}},
		pending:  &fakePendingLister{views: map[string][]domain.PendingView{}},
		remover:  &fakePendingRemover{journal: journal},
		saver:    &fakePendingSaver{journal: journal},
		registry: &fakeRegistryLookup{},
		focuser:  &fakePaneFocuser{journal: journal},
	}
	f.detector = &app.ScreenDetector{
		Panes:    f.panes,
		Dumper:   f.dumper,
		Config:   f.config,
		State:    f.state,
		Pending:  f.pending,
		Remover:  f.remover,
		Writer:   f.saver,
		Registry: f.registry,
		Focuser:  f.focuser,
		Clock:    paneClock{now: time.Date(2026, 8, 11, 10, 20, 30, 0, time.UTC)},
	}
	return f
}

const blockedDump = "• Running touch probe.txt\n\n  Would you like to run the following command?\n\n  Press y\n"

const workingDump = "• Working (3s • esc to interrupt)\n"

const idleDump = "› Summarize recent commits\n"

// TestScreenDetectorTickSurfacesApproval は tick の一巡(ペイン列挙 → 絞り込み →
// dump → 分類 → 副作用)を固定する(test.sh 17b7 相当)。
func TestScreenDetectorTickSurfacesApproval(t *testing.T) {
	t.Parallel()

	f := newScreenDetectFixture(
		[]app.AgentPane{
			{Tab: "cx-task", ID: "5", Agent: "codex"},
			{Tab: "cl-task", ID: "7", Agent: "claude"},
		},
		map[string]string{"5": blockedDump, "7": idleDump},
	)

	if err := f.detector.Tick(dashboardEnv); err != nil {
		t.Fatalf("Tick() = %v", err)
	}

	// hooks 方式のペインは dump すらしない(現行版も detection で先に弾く)。
	if want := []string{"5"}; !reflect.DeepEqual(f.dumper.ids, want) {
		t.Errorf("dump したペイン = %v, want %v", f.dumper.ids, want)
	}

	slug := domain.ScreenTabSlug("cx-task")
	wantJournal := []string{
		"dump-screen 5",
		"screen-state-write " + slug + " blocked",
		"pending-save " + domain.ScreenPendingSessionID("cx-task"),
	}
	if !reflect.DeepEqual(f.journal.entries, wantJournal) {
		t.Errorf("副作用の並び = %v, want %v", f.journal.entries, wantJournal)
	}

	saved := f.saver.saved[0]
	if saved.Event != domain.EventNotification ||
		saved.Message != "Would you like to run the following command?" {
		t.Errorf("書いた pending = %+v", saved)
	}
	if saved.Tab != "cx-task" || saved.Session != "s1" || saved.Agent != "codex" {
		t.Errorf("pending の識別子が違う: %+v", saved)
	}
	if saved.ClaudeSessionID != domain.ScreenPendingSessionID("cx-task") {
		t.Errorf("claude_session_id = %q", saved.ClaudeSessionID)
	}
	if saved.Time != "10:20:30" {
		t.Errorf("time = %q, want 10:20:30", saved.Time)
	}
}

// TestScreenDetectorTickBorrowsRegistryFields は screen 由来 pending が
// レジストリから 3 キーを借りることを固定する。
//
// これが無いと、そのタブの唯一の pending が screen 由来になったときに
// 削除時のログ収集(transcript_path)と Done からの復元(dir)が壊れる。
func TestScreenDetectorTickBorrowsRegistryFields(t *testing.T) {
	t.Parallel()

	f := newScreenDetectFixture(
		[]app.AgentPane{{Tab: "cx-task", ID: "5", Agent: "codex"}},
		map[string]string{"5": blockedDump},
	)
	f.registry.entries = map[string]domain.RegistryEntry{
		"s1/cx-task": {
			Dir: "/tmp/proj", TaskType: "dev", TranscriptPath: "/tmp/rollout.jsonl",
			// 借りるのは 3 キーだけ。セッション ID は借りない(screen- 前置の
			// 合成 ID のままにする)。
			ClaudeSessionID: "thread-real",
		},
	}

	if err := f.detector.Tick(dashboardEnv); err != nil {
		t.Fatalf("Tick() = %v", err)
	}

	saved := f.saver.saved[0]
	if saved.Dir != "/tmp/proj" || saved.TaskType != "dev" ||
		saved.TranscriptPath != "/tmp/rollout.jsonl" {
		t.Errorf("借用した 3 キーが入っていない: %+v", saved)
	}
	if saved.ClaudeSessionID != domain.ScreenPendingSessionID("cx-task") {
		t.Errorf("レジストリのセッション ID を借りてしまった: %q", saved.ClaudeSessionID)
	}
}

// TestScreenDetectorTickWithoutRegistryEntry はレジストリにエントリが無くても
// pending を書けることを固定する(承認待ちで始まる最初のターン)。
func TestScreenDetectorTickWithoutRegistryEntry(t *testing.T) {
	t.Parallel()

	f := newScreenDetectFixture(
		[]app.AgentPane{{Tab: "cx-task", ID: "5", Agent: "codex"}},
		map[string]string{"5": blockedDump},
	)

	if err := f.detector.Tick(dashboardEnv); err != nil {
		t.Fatalf("Tick() = %v", err)
	}
	saved := f.saver.saved[0]
	if saved.Dir != "" || saved.TaskType != "" || saved.TranscriptPath != "" {
		t.Errorf("借りるものが無いのに値が入った: %+v", saved)
	}
}

// TestScreenDetectorTickSkipsUnusablePanes は走査対象から外れる条件を固定する。
func TestScreenDetectorTickSkipsUnusablePanes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		panes []app.AgentPane
		dumps map[string]string
	}{
		{
			name:  "hooks 方式のエージェント",
			panes: []app.AgentPane{{Tab: "cl-task", ID: "7", Agent: "claude"}},
			dumps: map[string]string{"7": blockedDump},
		},
		{
			name:  "設定に無いエージェント(既定は hooks)",
			panes: []app.AgentPane{{Tab: "t", ID: "1", Agent: "unknown"}},
			dumps: map[string]string{"1": blockedDump},
		},
		{
			name:  "タブ名が空",
			panes: []app.AgentPane{{Tab: "", ID: "5", Agent: "codex"}},
			dumps: map[string]string{"5": blockedDump},
		},
		{
			name:  "ペイン id が空",
			panes: []app.AgentPane{{Tab: "cx-task", ID: "", Agent: "codex"}},
			dumps: map[string]string{"": blockedDump},
		},
		{
			name:  "ペインが 1 枚も無い",
			panes: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newScreenDetectFixture(tt.panes, tt.dumps)
			if err := f.detector.Tick(dashboardEnv); err != nil {
				t.Fatalf("Tick() = %v", err)
			}
			if len(f.journal.entries) != 0 {
				t.Errorf("副作用が起きた: %v", f.journal.entries)
			}
		})
	}
}

// TestScreenDetectorTickSkipsEmptyDump は dump が空のペインを飛ばすことを
// 固定する。
//
// 打ち切られた・まだ描画されていない画面から状態を決めてはならない。
// 現行版の `[[ -n "$text" ]] || continue` と同じで、neutral と同じ扱いになる。
func TestScreenDetectorTickSkipsEmptyDump(t *testing.T) {
	t.Parallel()

	f := newScreenDetectFixture(
		[]app.AgentPane{{Tab: "cx-task", ID: "5", Agent: "codex"}},
		map[string]string{"5": ""},
	)
	if err := f.detector.Tick(dashboardEnv); err != nil {
		t.Fatalf("Tick() = %v", err)
	}
	if want := []string{"dump-screen 5"}; !reflect.DeepEqual(f.journal.entries, want) {
		t.Errorf("副作用 = %v, want %v", f.journal.entries, want)
	}
}

// TestScreenDetectorTickAppliesEffectsInOrder は状態機械が返した副作用を
// その順で実行することを固定する。
//
// working は「pending を消してから Main へ戻る」。順序が入れ替わると、
// フォーカスが移った先で一覧が古いまま描かれる。
func TestScreenDetectorTickAppliesEffectsInOrder(t *testing.T) {
	t.Parallel()

	f := newScreenDetectFixture(
		[]app.AgentPane{{Tab: "cx-task", ID: "5", Agent: "codex"}},
		map[string]string{"5": workingDump},
	)
	slug := domain.ScreenTabSlug("cx-task")
	// 前回は承認待ちで、その pending が残っている状態。
	f.state.lines["s1/"+slug] = "blocked\n"
	f.pending.views["s1"] = []domain.PendingView{
		{Name: domain.ScreenPendingName("cx-task"), Tab: "cx-task", Event: domain.EventNotification},
		{Name: "thread-1.json", Tab: "cx-task", Event: domain.EventStop},
		{Name: "other.json", Tab: "other-tab", Event: domain.EventStop},
	}

	if err := f.detector.Tick(dashboardEnv); err != nil {
		t.Fatalf("Tick() = %v", err)
	}

	want := []string{
		"dump-screen 5",
		"screen-state-write " + slug + " working",
		"pending-delete-by-name " + domain.ScreenPendingName("cx-task"),
		"pending-delete-by-name thread-1.json",
		"go-to-tab-name Main",
	}
	if !reflect.DeepEqual(f.journal.entries, want) {
		t.Errorf("副作用の並び = %v, want %v", f.journal.entries, want)
	}
}

// TestScreenDetectorTickNeutralTouchesNothing は neutral の完全な no-op を
// ユースケースの側でも固定する。
func TestScreenDetectorTickNeutralTouchesNothing(t *testing.T) {
	t.Parallel()

	f := newScreenDetectFixture(
		[]app.AgentPane{{Tab: "cx-task", ID: "5", Agent: "codex"}},
		map[string]string{"5": "  ↑↓ scroll · esc to close\n"},
	)
	config := codexScreenConfig()
	agent := config.Agents["codex"]
	agent.Patterns.Neutral = []string{`esc to close *$`}
	config.Agents["codex"] = agent
	f.config.config = config

	if err := f.detector.Tick(dashboardEnv); err != nil {
		t.Fatalf("Tick() = %v", err)
	}
	if want := []string{"dump-screen 5"}; !reflect.DeepEqual(f.journal.entries, want) {
		t.Errorf("neutral で副作用が起きた: %v", f.journal.entries)
	}
}

// TestScreenDetectorTickUsesPreviousState は前回状態を状態ファイルから
// 読み直していることを固定する(idle の確定に要る)。
func TestScreenDetectorTickUsesPreviousState(t *testing.T) {
	t.Parallel()

	f := newScreenDetectFixture(
		[]app.AgentPane{{Tab: "cx-task", ID: "5", Agent: "codex"}},
		map[string]string{"5": idleDump},
	)
	slug := domain.ScreenTabSlug("cx-task")
	// 5 秒前(1786443630 - 5)から idle 保留中。今回の観測で確定して Stop が書かれる。
	f.state.lines["s1/"+slug] = "idle_pending 1786443625\n"

	if err := f.detector.Tick(dashboardEnv); err != nil {
		t.Fatalf("Tick() = %v", err)
	}
	if got := f.state.lines["s1/"+slug]; got != "idle" {
		t.Errorf("書いた状態 = %q, want idle", got)
	}
	if len(f.saver.saved) != 1 || f.saver.saved[0].Event != domain.EventStop {
		t.Errorf("Stop が書かれていない: %+v", f.saver.saved)
	}
	if f.saver.saved[0].Message != domain.ScreenCompleteMessage {
		t.Errorf("Stop の文言 = %q", f.saver.saved[0].Message)
	}
}

// TestScreenDetectorTickReportsWriteFailures は書き込みの失敗を握り潰さない
// ことを固定する。
//
// 現行 Shell 版は失敗を黙って捨てるため、pending を書けなくなっても
// ダッシュボードは古い一覧を出し続け、利用者は原因に気づけない。
func TestScreenDetectorTickReportsWriteFailures(t *testing.T) {
	t.Parallel()

	errWrite := errors.New("書けない")

	tests := []struct {
		name  string
		setup func(*screenDetectFixture)
		dump  string
	}{
		{
			name:  "状態ファイル",
			dump:  blockedDump,
			setup: func(f *screenDetectFixture) { f.state.err = errWrite },
		},
		{
			name:  "pending の書き込み",
			dump:  blockedDump,
			setup: func(f *screenDetectFixture) { f.saver.err = errWrite },
		},
		{
			name: "pending の削除",
			dump: workingDump,
			setup: func(f *screenDetectFixture) {
				f.pending.views["s1"] = []domain.PendingView{
					{Name: "thread-1.json", Tab: "cx-task", Event: domain.EventStop},
				}
				f.remover.err = errWrite
			},
		},
		{
			name:  "pending の読み取り",
			dump:  blockedDump,
			setup: func(f *screenDetectFixture) { f.pending.err = errWrite },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newScreenDetectFixture(
				[]app.AgentPane{{Tab: "cx-task", ID: "5", Agent: "codex"}},
				map[string]string{"5": tt.dump},
			)
			tt.setup(f)

			err := f.detector.Tick(dashboardEnv)
			if err == nil || !errors.Is(err, errWrite) {
				t.Errorf("Tick() = %v, want %v を含むエラー", err, errWrite)
			}
		})
	}
}

// TestScreenDetectorTickWithUnreadableConfig は設定が読めないときの扱いを
// 固定する。
//
// 現行 task-lib.sh の agent_detection は `jq ... 2>/dev/null` で失敗を握り潰し、
// 検出方式を既定の hooks に落とす。結果として走査対象が 1 つも無くなる。
func TestScreenDetectorTickWithUnreadableConfig(t *testing.T) {
	t.Parallel()

	f := newScreenDetectFixture(
		[]app.AgentPane{{Tab: "cx-task", ID: "5", Agent: "codex"}},
		map[string]string{"5": blockedDump},
	)
	f.config.failed = true
	f.config.config = domain.Config{}

	if err := f.detector.Tick(dashboardEnv); err != nil {
		t.Fatalf("Tick() = %v", err)
	}
	if len(f.journal.entries) != 0 {
		t.Errorf("副作用が起きた: %v", f.journal.entries)
	}
}

// TestScreenDetectorTickOutsideZellij はセッション名が空のときに
// unknown へ落ちることを固定する。
func TestScreenDetectorTickOutsideZellij(t *testing.T) {
	t.Parallel()

	f := newScreenDetectFixture(
		[]app.AgentPane{{Tab: "cx-task", ID: "5", Agent: "codex"}},
		map[string]string{"5": blockedDump},
	)
	if err := f.detector.Tick(app.PaneEnv{}); err != nil {
		t.Fatalf("Tick() = %v", err)
	}
	if len(f.saver.sessions) != 1 || f.saver.sessions[0] != domain.DefaultSessionName {
		t.Errorf("書き込み先のセッション = %v, want %q", f.saver.sessions, domain.DefaultSessionName)
	}
}

// TestScreenDetectorTickScansEveryScreenPane は screen 方式のペインが複数
// あってもすべて走査することを固定する。
func TestScreenDetectorTickScansEveryScreenPane(t *testing.T) {
	t.Parallel()

	f := newScreenDetectFixture(
		[]app.AgentPane{
			{Tab: "a", ID: "1", Agent: "codex"},
			{Tab: "b", ID: "2", Agent: "codex"},
		},
		map[string]string{"1": blockedDump, "2": blockedDump},
	)
	if err := f.detector.Tick(dashboardEnv); err != nil {
		t.Fatalf("Tick() = %v", err)
	}
	if len(f.saver.saved) != 2 {
		t.Fatalf("書いた pending = %d 件, want 2", len(f.saver.saved))
	}
	tabs := []string{f.saver.saved[0].Tab, f.saver.saved[1].Tab}
	if !reflect.DeepEqual(tabs, []string{"a", "b"}) {
		t.Errorf("走査したタブ = %v", tabs)
	}
	if !strings.Contains(strings.Join(f.dumper.ids, " "), "2") {
		t.Errorf("2 枚目を dump していない: %v", f.dumper.ids)
	}
}
