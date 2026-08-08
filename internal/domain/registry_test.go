package domain_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

func TestRegistryEntryMarshalOmitsEmptyOptionalFields(t *testing.T) {
	t.Parallel()

	// registry-lib.sh の registry_upsert は tab / session / claude_session_id /
	// updated_at を必ず書き、dir / task_type / agent / transcript_path は
	// 空ならキーごと省略する。
	e := domain.RegistryEntry{
		Tab:             "tab-one",
		Session:         "reg-sess",
		ClaudeSessionID: "sid-1",
		UpdatedAt:       "2026-08-08T10:11:12+0900",
	}

	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal() = %v", err)
	}
	got := decode(t, b)

	want := map[string]any{
		"tab":               "tab-one",
		"session":           "reg-sess",
		"claude_session_id": "sid-1",
		"updated_at":        "2026-08-08T10:11:12+0900",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Marshal() = %v, want %v", got, want)
	}
}

func TestRegistryEntryRoundTrip(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
	  "tab": "tab-one",
	  "session": "reg-sess",
	  "claude_session_id": "sid-1",
	  "updated_at": "2026-08-08T10:11:12+0900",
	  "dir": "/tmp/dir1",
	  "task_type": "dev",
	  "agent": "claude",
	  "transcript_path": "/tmp/t1.jsonl"
	}`)

	var e domain.RegistryEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("Unmarshal() = %v", err)
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal() = %v", err)
	}
	if got, want := decode(t, b), decode(t, raw); !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %v, want %v", got, want)
	}
}

func TestLatestPerTab(t *testing.T) {
	t.Parallel()

	// restore-session.sh:69 の
	//   jq -sc 'group_by(.tab) | map(max_by(.updated_at // "")) | .[]'
	// と同じ選択を行う。group_by は tab の昇順に並べ替え、max_by は
	// 同値のとき入力順で最後の要素を返す(jq 1.7.1 で実測)。
	tests := []struct {
		name    string
		entries []domain.RegistryEntry
		want    []domain.RegistryEntry
	}{
		{
			name:    "空入力",
			entries: nil,
			want:    []domain.RegistryEntry{},
		},
		{
			name: "タブごとに updated_at が最新の 1 件",
			entries: []domain.RegistryEntry{
				{Tab: "alpha", ClaudeSessionID: "old", UpdatedAt: "2026-08-08T10:00:00+0900"},
				{Tab: "alpha", ClaudeSessionID: "new", UpdatedAt: "2026-08-08T12:00:00+0900"},
				{Tab: "beta", ClaudeSessionID: "b1", UpdatedAt: "2026-08-08T11:00:00+0900"},
			},
			want: []domain.RegistryEntry{
				{Tab: "alpha", ClaudeSessionID: "new", UpdatedAt: "2026-08-08T12:00:00+0900"},
				{Tab: "beta", ClaudeSessionID: "b1", UpdatedAt: "2026-08-08T11:00:00+0900"},
			},
		},
		{
			name: "結果は tab の昇順",
			entries: []domain.RegistryEntry{
				{Tab: "zulu", UpdatedAt: "2026-08-08T10:00:00+0900"},
				{Tab: "alpha", UpdatedAt: "2026-08-08T10:00:00+0900"},
				{Tab: "mike", UpdatedAt: "2026-08-08T10:00:00+0900"},
			},
			want: []domain.RegistryEntry{
				{Tab: "alpha", UpdatedAt: "2026-08-08T10:00:00+0900"},
				{Tab: "mike", UpdatedAt: "2026-08-08T10:00:00+0900"},
				{Tab: "zulu", UpdatedAt: "2026-08-08T10:00:00+0900"},
			},
		},
		{
			name: "updated_at が同値なら入力順で最後を採る",
			entries: []domain.RegistryEntry{
				{Tab: "alpha", ClaudeSessionID: "first", UpdatedAt: "2026-08-08T10:00:00+0900"},
				{Tab: "alpha", ClaudeSessionID: "last", UpdatedAt: "2026-08-08T10:00:00+0900"},
			},
			want: []domain.RegistryEntry{
				{Tab: "alpha", ClaudeSessionID: "last", UpdatedAt: "2026-08-08T10:00:00+0900"},
			},
		},
		{
			name: "updated_at 欠落は空文字として最小に扱う",
			entries: []domain.RegistryEntry{
				{Tab: "alpha", ClaudeSessionID: "dated", UpdatedAt: "2026-08-08T10:00:00+0900"},
				{Tab: "alpha", ClaudeSessionID: "undated"},
			},
			want: []domain.RegistryEntry{
				{Tab: "alpha", ClaudeSessionID: "dated", UpdatedAt: "2026-08-08T10:00:00+0900"},
			},
		},
		{
			name: "updated_at が全て欠落なら入力順で最後",
			entries: []domain.RegistryEntry{
				{Tab: "alpha", ClaudeSessionID: "first"},
				{Tab: "alpha", ClaudeSessionID: "last"},
			},
			want: []domain.RegistryEntry{
				{Tab: "alpha", ClaudeSessionID: "last"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.LatestPerTab(tt.entries)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LatestPerTab() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLatestPerTabDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	entries := []domain.RegistryEntry{
		{Tab: "zulu", UpdatedAt: "2026-08-08T10:00:00+0900"},
		{Tab: "alpha", UpdatedAt: "2026-08-08T10:00:00+0900"},
	}
	before := append([]domain.RegistryEntry(nil), entries...)

	domain.LatestPerTab(entries)

	if !reflect.DeepEqual(entries, before) {
		t.Errorf("入力が変更された: %+v, want %+v", entries, before)
	}
}
