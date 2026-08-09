package store_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

var _ app.PendingFinder = (*store.PendingStore)(nil)

// writePendingFiles は session 配下に pending ファイルを作る。
func writePendingFiles(t *testing.T, root, session string, files map[string]string) {
	t.Helper()

	dir := filepath.Join(root, session)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) = %v", name, err)
		}
	}
}

func TestPendingStoreFindByTab(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePendingFiles(t, root, "test-session", map[string]string{
		"sess-a.json": `{"tab":"other-tab","session":"test-session","claude_session_id":"sess-a","message":"a","event":"Stop","time":"10:00:00","agent":"claude"}`,
		"sess-b.json": `{"tab":"record-test","session":"test-session","claude_session_id":"sess-b","message":"Task complete","event":"Stop","time":"10:00:05","agent":"claude","transcript_path":"/tmp/t.jsonl","dir":"/tmp/myapp","task_type":"dev"}`,
	})

	got, found, err := store.NewPendingStore(root).FindByTab("test-session", "record-test")
	if err != nil {
		t.Fatalf("FindByTab() = %v", err)
	}
	if !found {
		t.Fatal("FindByTab() found = false, want true")
	}

	want := domain.Pending{
		Tab:             "record-test",
		Session:         "test-session",
		ClaudeSessionID: "sess-b",
		Message:         "Task complete",
		Event:           domain.EventStop,
		Time:            "10:00:05",
		Agent:           "claude",
		TranscriptPath:  "/tmp/t.jsonl",
		Dir:             "/tmp/myapp",
		TaskType:        "dev",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindByTab() =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestPendingStoreFindByTabPicksFirstInFilenameOrder(t *testing.T) {
	t.Parallel()

	// --resume で再開すると同じタブに複数のセッション ID が残る。現行版は
	// `for f in *.json` の展開順(= ファイル名の昇順)で最初の 1 件を採る。
	root := t.TempDir()
	writePendingFiles(t, root, "s", map[string]string{
		"c-third.json":  `{"tab":"dup","claude_session_id":"c"}`,
		"a-first.json":  `{"tab":"dup","claude_session_id":"a"}`,
		"b-second.json": `{"tab":"dup","claude_session_id":"b"}`,
	})

	got, found, err := store.NewPendingStore(root).FindByTab("s", "dup")
	if err != nil {
		t.Fatalf("FindByTab() = %v", err)
	}
	if !found {
		t.Fatal("FindByTab() found = false, want true")
	}
	if got.ClaudeSessionID != "a" {
		t.Errorf("ClaudeSessionID = %q, want %q(ファイル名の昇順で最初)", got.ClaudeSessionID, "a")
	}
}

func TestPendingStoreFindByTabSkipsUnreadableEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		want  string // 見つかるべき claude_session_id。空なら見つからない
	}{
		{
			name: "壊れた JSON は読み飛ばす",
			files: map[string]string{
				"a.json": `{"tab":"dup",`,
				"b.json": `{"tab":"dup","claude_session_id":"b"}`,
			},
			want: "b",
		},
		{
			name: "JSON でない内容は読み飛ばす",
			files: map[string]string{
				"a.json": "not json at all",
				"b.json": `{"tab":"dup","claude_session_id":"b"}`,
			},
			want: "b",
		},
		{
			name: "空ファイルは読み飛ばす",
			files: map[string]string{
				"a.json": ``,
				"b.json": `{"tab":"dup","claude_session_id":"b"}`,
			},
			want: "b",
		},
		{
			// 現行版の glob は *.json しか拾わない。
			name: "拡張子が違うファイルは見ない",
			files: map[string]string{
				"a.json.bak": `{"tab":"dup","claude_session_id":"bak"}`,
				"b.json":     `{"tab":"dup","claude_session_id":"b"}`,
			},
			want: "b",
		},
		{
			name: "tab が一致しなければ見つからない",
			files: map[string]string{
				"a.json": `{"tab":"other","claude_session_id":"a"}`,
			},
			want: "",
		},
		{
			name: "tab キーが無ければ見つからない",
			files: map[string]string{
				"a.json": `{"claude_session_id":"a"}`,
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writePendingFiles(t, root, "s", tt.files)

			got, found, err := store.NewPendingStore(root).FindByTab("s", "dup")
			if err != nil {
				t.Fatalf("FindByTab() = %v", err)
			}
			if tt.want == "" {
				if found {
					t.Errorf("FindByTab() found = true(%+v), want false", got)
				}
				return
			}
			if !found {
				t.Fatal("FindByTab() found = false, want true")
			}
			if got.ClaudeSessionID != tt.want {
				t.Errorf("ClaudeSessionID = %q, want %q", got.ClaudeSessionID, tt.want)
			}
		})
	}
}

func TestPendingStoreFindByTabIgnoresDirectories(t *testing.T) {
	t.Parallel()

	// 現行版は `[ -f "$f" ]` でディレクトリを弾く。
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "s", "a.json"), 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	writePendingFiles(t, root, "s", map[string]string{
		"b.json": `{"tab":"dup","claude_session_id":"b"}`,
	})

	got, found, err := store.NewPendingStore(root).FindByTab("s", "dup")
	if err != nil {
		t.Fatalf("FindByTab() = %v", err)
	}
	if !found || got.ClaudeSessionID != "b" {
		t.Errorf("FindByTab() = %+v (found=%v), want b", got, found)
	}
}

func TestPendingStoreFindByTabWithoutSessionDirectory(t *testing.T) {
	t.Parallel()

	// まだ 1 件も pending が無い状態。エラーにはしない。
	_, found, err := store.NewPendingStore(t.TempDir()).FindByTab("no-such-session", "tab")
	if err != nil {
		t.Fatalf("FindByTab() = %v", err)
	}
	if found {
		t.Error("FindByTab() found = true, want false")
	}
}

func TestPendingStoreFindByTabReportsUnexpectedFailure(t *testing.T) {
	t.Parallel()

	// セッションディレクトリを読めない場合はエラーを返す。現行版は glob が
	// 何も返さず黙って exit 0 するが、原因の分かる失敗は伝える。
	root := t.TempDir()
	dir := filepath.Join(root, "s")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("Chmod() = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if _, _, err := store.NewPendingStore(root).FindByTab("s", "tab"); err == nil {
		t.Error("FindByTab() = nil, want エラー")
	}
}
