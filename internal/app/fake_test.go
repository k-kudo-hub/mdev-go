package app_test

import (
	"encoding/json"
	"testing"
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
	// byTab は FindByTab が返す値。キーは (session, tab)。
	byTab map[string]domain.Pending

	saved     map[string]domain.Pending
	deleted   []string
	findCalls int
	saveErr   error
	deleteErr error
	findErr   error
}

func newFakePendingStore() *fakePendingStore {
	return &fakePendingStore{
		events: map[string]string{},
		byTab:  map[string]domain.Pending{},
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

// record ユースケース用の fake。
var (
	_ app.PendingFinder    = (*fakePendingStore)(nil)
	_ app.TranscriptReader = (*fakeTranscriptReader)(nil)
	_ app.DailyAppender    = (*fakeDailyStore)(nil)
	_ app.PricingLoader    = fakePricingLoader{}
)

// FindByTab は (session, tab) の pending を返す。
// fakePendingStore が PendingStore と PendingFinder の両方を実装しているのは、
// record が pending を削除しないことをこのテストから観測するためである。
func (s *fakePendingStore) FindByTab(session, tab string) (domain.Pending, bool, error) {
	s.findCalls++
	if s.findErr != nil {
		return domain.Pending{}, false, s.findErr
	}
	pending, ok := s.byTab[pendingKey(session, tab)]
	return pending, ok, nil
}

// fakeTranscriptReader はメモリ上の transcript を返す。
type fakeTranscriptReader struct {
	files map[string]string
}

func newFakeTranscriptReader() *fakeTranscriptReader {
	return &fakeTranscriptReader{files: map[string]string{}}
}

func (r *fakeTranscriptReader) Read(path string) ([]byte, bool) {
	content, ok := r.files[path]
	if !ok {
		return nil, false
	}
	return []byte(content), true
}

// appendedRecord は fakeDailyStore が記録する 1 回分の追記である。
type appendedRecord struct {
	session string
	date    string
	record  domain.DailyRecord
}

type fakeDailyStore struct {
	appended  []appendedRecord
	appendErr error
}

func (s *fakeDailyStore) Append(session, date string, record domain.DailyRecord) error {
	if s.appendErr != nil {
		return s.appendErr
	}
	s.appended = append(s.appended, appendedRecord{session: session, date: date, record: record})
	return nil
}

type fakePricingLoader struct {
	pricing domain.Pricing
}

func (l fakePricingLoader) Load() domain.Pricing { return l.pricing }

// testPricing は record のテストで使う単価表(config.default.json の一部)。
func testPricing(t *testing.T) domain.Pricing {
	t.Helper()

	raw := `{
	  "claude-opus-4-6":  {"input": 5.0, "output": 25.0, "cache_write_5m": 6.25, "cache_write_1h": 10.0, "cache_hit": 0.5},
	  "claude-sonnet-4-6":{"input": 3.0, "output": 15.0, "cache_write_5m": 3.75, "cache_write_1h": 6.0,  "cache_hit": 0.3},
	  "fast_multiplier": 6
	}`
	var pricing domain.Pricing
	if err := json.Unmarshal([]byte(raw), &pricing); err != nil {
		t.Fatalf("Unmarshal() = %v", err)
	}
	return pricing
}
