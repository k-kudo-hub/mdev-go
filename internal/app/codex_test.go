package app_test

import (
	"errors"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

var _ app.CodexTranscriptLocator = (*fakeCodexLocator)(nil)

// fakeCodexLocator は会話ログの引き当ての代役である。
type fakeCodexLocator struct {
	// path は Locate が返す値。空なら「見つからなかった」を表す。
	path string
	// asked は Locate に渡されたスレッド ID。
	asked []string
}

func (l *fakeCodexLocator) Locate(threadID string) string {
	l.asked = append(l.asked, threadID)
	return l.path
}

// codexNow は codex のテストで使う固定時刻。
var codexNow = time.Date(2026, 8, 14, 10, 11, 12, 0, time.FixedZone("JST", 9*3600))

// codexPayload はターン完了の payload である。
const codexPayload = `{"type":"agent-turn-complete","thread-id":"th-1","cwd":"/w/repo",` +
	`"last-assistant-message":"直しました"}`

// newCodexNotifier は fake を組み合わせた Notifier を返す。
func newCodexNotifier() (*app.CodexNotifier, *fakePendingStore, *fakeRegistryStore, *fakeCodexLocator) {
	pending := newFakePendingStore()
	registry := &fakeRegistryStore{}
	locator := &fakeCodexLocator{path: "/codex/sessions/rollout-th-1.jsonl"}
	return &app.CodexNotifier{
		Pending:    pending,
		Registry:   registry,
		Transcript: locator,
		Clock:      fakeClock{now: codexNow},
	}, pending, registry, locator
}

// codexTaskEnv はタスクタブで動いているときの環境である。
var codexTaskEnv = app.HookEnv{
	ZellijSession: "mdev-go-1",
	TaskTabName:   "fix-bug",
	TaskType:      "bugfix",
}

// TestCodexNotifyWritesPendingAndRegistry はターン完了の 2 つの作用を確かめる。
func TestCodexNotifyWritesPendingAndRegistry(t *testing.T) {
	t.Parallel()

	notifier, pending, registry, locator := newCodexNotifier()

	if err := notifier.Notify([]byte(codexPayload), codexTaskEnv); err != nil {
		t.Fatalf("Notify = %v", err)
	}

	wantPending := domain.Pending{
		Tab:             "fix-bug",
		Session:         "mdev-go-1",
		ClaudeSessionID: "th-1",
		Message:         "直しました",
		Event:           domain.EventStop,
		Time:            "10:11:12",
		Agent:           "codex",
		TranscriptPath:  "/codex/sessions/rollout-th-1.jsonl",
		Dir:             "/w/repo",
		TaskType:        "bugfix",
	}
	got, ok := pending.saved[pendingKey("mdev-go-1", "th-1")]
	if !ok {
		t.Fatalf("pending が書かれていない: %#v", pending.saved)
	}
	if got != wantPending {
		t.Errorf("pending = %#v, want %#v", got, wantPending)
	}

	wantEntry := domain.RegistryEntry{
		Tab:             "fix-bug",
		Session:         "mdev-go-1",
		ClaudeSessionID: "th-1",
		UpdatedAt:       "2026-08-14T10:11:12+0900",
		Dir:             "/w/repo",
		TaskType:        "bugfix",
		Agent:           "codex",
		TranscriptPath:  "/codex/sessions/rollout-th-1.jsonl",
	}
	if len(registry.upserted) != 1 {
		t.Fatalf("レジストリの更新 = %d 件, want 1", len(registry.upserted))
	}
	if registry.upserted[0] != wantEntry {
		t.Errorf("レジストリ = %#v, want %#v", registry.upserted[0], wantEntry)
	}

	if len(locator.asked) != 1 || locator.asked[0] != "th-1" {
		t.Errorf("会話ログの引き当て = %v, want [th-1]", locator.asked)
	}
}

// TestCodexNotifyIgnoresIrrelevantPayloads は捨てるべき入力で何も書かない
// ことを確かめる。codex はターン完了以外でも notify を呼びうる。
func TestCodexNotifyIgnoresIrrelevantPayloads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "引数が空", raw: ``},
		{name: "別の種別", raw: `{"type":"session-start","thread-id":"th-1"}`},
		{name: "thread-id が空", raw: `{"type":"agent-turn-complete","thread-id":""}`},
		{name: "壊れた JSON", raw: `{`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			notifier, pending, registry, locator := newCodexNotifier()

			if err := notifier.Notify([]byte(tt.raw), codexTaskEnv); err != nil {
				t.Fatalf("Notify = %v", err)
			}
			if len(pending.saved) != 0 || len(registry.upserted) != 0 {
				t.Errorf("何も書かないはずが pending=%#v registry=%#v", pending.saved, registry.upserted)
			}
			// 引き当ては通信ではないが、捨てる入力で走らせる意味は無い。
			if len(locator.asked) != 0 {
				t.Errorf("会話ログを引きに行った: %v", locator.asked)
			}
		})
	}
}

// TestCodexNotifyKeepsWaitingPending は退避中のタスクを守ることを確かめる。
//
// Waiting は「外部の返答待ちとして意図的にダッシュボードから外した」印である。
// ターン完了で戻してしまうと、退避の操作が無かったことになる。
func TestCodexNotifyKeepsWaitingPending(t *testing.T) {
	t.Parallel()

	notifier, pending, registry, _ := newCodexNotifier()
	pending.events[pendingKey("mdev-go-1", "th-1")] = domain.EventWaiting

	if err := notifier.Notify([]byte(codexPayload), codexTaskEnv); err != nil {
		t.Fatalf("Notify = %v", err)
	}

	if len(pending.saved) != 0 {
		t.Errorf("Waiting を上書きした: %#v", pending.saved)
	}
	// pending を守っても、再起動後の復元のためにレジストリは最新化する。
	if len(registry.upserted) != 1 {
		t.Errorf("レジストリの更新 = %d 件, want 1", len(registry.upserted))
	}
}

// TestCodexNotifyOverwritesNotificationPending は Notification を上書きする
// ことを確かめる。
//
// Claude Code 側(ShouldOverwritePending)との差である。codex には許可待ちを
// pending へ書く経路が無いため、守る相手がいない。
func TestCodexNotifyOverwritesNotificationPending(t *testing.T) {
	t.Parallel()

	notifier, pending, _, _ := newCodexNotifier()
	pending.events[pendingKey("mdev-go-1", "th-1")] = domain.EventNotification

	if err := notifier.Notify([]byte(codexPayload), codexTaskEnv); err != nil {
		t.Fatalf("Notify = %v", err)
	}
	if len(pending.saved) != 1 {
		t.Errorf("上書きするはずが pending = %#v", pending.saved)
	}
}

// TestCodexNotifyOutsideTaskTab は conductor のタスクタブ以外での挙動を確かめる。
//
// notify は同じマシンのすべての codex セッションで発火する。タスクタブ以外を
// レジストリへ入れると、復元時に無関係な作業を復活させてしまう。
func TestCodexNotifyOutsideTaskTab(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  app.HookEnv
		// wantTab は pending に入るべきタブ名。
		wantTab string
		// wantSession は pending に入るべきセッション名。
		wantSession string
	}{
		{
			// cwd の basename へ落ちる。
			name:        "TASK_TAB_NAME が無ければ cwd から決める",
			env:         app.HookEnv{ZellijSession: "mdev-go-1"},
			wantTab:     "repo",
			wantSession: "mdev-go-1",
		},
		{
			name:        "zellij の外でも pending は書く",
			env:         app.HookEnv{TaskTabName: "fix-bug"},
			wantTab:     "fix-bug",
			wantSession: domain.DefaultSessionName,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			notifier, pending, registry, _ := newCodexNotifier()

			if err := notifier.Notify([]byte(codexPayload), tt.env); err != nil {
				t.Fatalf("Notify = %v", err)
			}
			if len(registry.upserted) != 0 {
				t.Errorf("レジストリへ入れてはいけない: %#v", registry.upserted)
			}
			got, ok := pending.saved[pendingKey(tt.wantSession, "th-1")]
			if !ok {
				t.Fatalf("pending が書かれていない: %#v", pending.saved)
			}
			if got.Tab != tt.wantTab {
				t.Errorf("タブ名 = %q, want %q", got.Tab, tt.wantTab)
			}
		})
	}
}

// TestCodexNotifyDefaultAgent は TASK_AGENT の既定値を確かめる。
//
// Claude Code 側は claude に落ちるが、この経路は codex に落ちる。取り違えると
// Done の一覧でエージェントが入れ替わって見える。
func TestCodexNotifyDefaultAgent(t *testing.T) {
	t.Parallel()

	tests := []struct{ taskAgent, want string }{
		{taskAgent: "", want: "codex"},
		{taskAgent: "codex-mini", want: "codex-mini"},
	}
	for _, tt := range tests {
		t.Run("TASK_AGENT="+tt.taskAgent, func(t *testing.T) {
			t.Parallel()
			notifier, pending, _, _ := newCodexNotifier()
			env := codexTaskEnv
			env.TaskAgent = tt.taskAgent

			if err := notifier.Notify([]byte(codexPayload), env); err != nil {
				t.Fatalf("Notify = %v", err)
			}
			if got := pending.saved[pendingKey("mdev-go-1", "th-1")].Agent; got != tt.want {
				t.Errorf("agent = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCodexNotifyMissingTranscript は会話ログが見つからない場合を確かめる。
// 記録自体は成り立つため、空のまま書く。
func TestCodexNotifyMissingTranscript(t *testing.T) {
	t.Parallel()

	notifier, pending, registry, locator := newCodexNotifier()
	locator.path = ""

	if err := notifier.Notify([]byte(codexPayload), codexTaskEnv); err != nil {
		t.Fatalf("Notify = %v", err)
	}
	if got := pending.saved[pendingKey("mdev-go-1", "th-1")].TranscriptPath; got != "" {
		t.Errorf("transcript_path = %q, want 空", got)
	}
	if got := registry.upserted[0].TranscriptPath; got != "" {
		t.Errorf("registry の transcript_path = %q, want 空", got)
	}
}

// TestCodexNotifyContinuesAfterRegistryFailure はレジストリの失敗で pending の
// 書き込みを止めないことを確かめる。
//
// レジストリは再起動後の復元にしか使わない。そちらの失敗でダッシュボードへの
// 反映まで落とすと、終わったタスクが画面に出てこない。
func TestCodexNotifyContinuesAfterRegistryFailure(t *testing.T) {
	t.Parallel()

	notifier, pending, registry, _ := newCodexNotifier()
	registry.upsertErr = errors.New("書けない")

	err := notifier.Notify([]byte(codexPayload), codexTaskEnv)
	if err == nil {
		t.Fatal("失敗を返すはず")
	}
	if len(pending.saved) != 1 {
		t.Errorf("pending は書くはずが %#v", pending.saved)
	}
}

// TestCodexNotifyReportsPendingFailure は pending の書き込み失敗を返すことを
// 確かめる。
func TestCodexNotifyReportsPendingFailure(t *testing.T) {
	t.Parallel()

	notifier, pending, _, _ := newCodexNotifier()
	pending.saveErr = errors.New("書けない")

	if err := notifier.Notify([]byte(codexPayload), codexTaskEnv); err == nil {
		t.Fatal("失敗を返すはず")
	}
}
