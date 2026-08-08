package app_test

import (
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// port の fake 実装。ユースケースのテストから副作用を観測するために使う。
var (
	_ app.PendingStore  = (*fakePendingStore)(nil)
	_ app.RegistryStore = (*fakeRegistryStore)(nil)
	_ app.Focuser       = (*fakeFocuser)(nil)
	_ app.Clock         = fakeClock{}
)

// pendingKey は fake が (session, sessionID) を 1 つのキーにまとめるための形式。
func pendingKey(session, sessionID string) string {
	return session + "/" + sessionID
}

type fakePendingStore struct {
	// events は Event が返す値。キーが無ければ空文字を返す
	// (pending 無し・壊れ JSON の両方を表す)。
	events map[string]string

	saved     map[string]domain.Pending
	deleted   []string
	saveErr   error
	deleteErr error
}

func newFakePendingStore() *fakePendingStore {
	return &fakePendingStore{
		events: map[string]string{},
		saved:  map[string]domain.Pending{},
	}
}

func (s *fakePendingStore) Event(session, sessionID string) string {
	return s.events[pendingKey(session, sessionID)]
}

func (s *fakePendingStore) Save(session, sessionID string, pending domain.Pending) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved[pendingKey(session, sessionID)] = pending
	return nil
}

func (s *fakePendingStore) Delete(session, sessionID string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = append(s.deleted, pendingKey(session, sessionID))
	return nil
}

type fakeRegistryStore struct {
	upserted  []domain.RegistryEntry
	upsertErr error
}

func (s *fakeRegistryStore) Upsert(entry domain.RegistryEntry) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserted = append(s.upserted, entry)
	return nil
}

type fakeFocuser struct {
	focused  []string
	focusErr error
}

func (f *fakeFocuser) FocusTab(name string) error {
	if f.focusErr != nil {
		return f.focusErr
	}
	f.focused = append(f.focused, name)
	return nil
}

type fakeClock struct {
	now time.Time
}

func (c fakeClock) Now() time.Time { return c.now }

// newTestRegistryEntry は fake の記録を確認するための最小のエントリ。
func newTestRegistryEntry() domain.RegistryEntry {
	return domain.RegistryEntry{
		Tab:             "tab-one",
		Session:         "sess",
		ClaudeSessionID: "sid",
		UpdatedAt:       "2026-08-08T10:11:12+0900",
	}
}

// testClock はユースケースのテストで使う固定時刻。
// UTC ではなく固定オフセットにして、updated_at の %z 相当の出力を検証できるようにする。
var testClock = fakeClock{
	now: time.Date(2026, 8, 8, 10, 11, 12, 0, time.FixedZone("JST", 9*60*60)),
}
