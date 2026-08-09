package store_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

var _ app.DailyAppender = (*store.DailyStore)(nil)

// testDailyRecord は追記の検証に使う最小のレコード。
func testDailyRecord(tab string) domain.DailyRecord {
	return domain.DailyRecord{
		Tab:         tab,
		Session:     "test-session",
		CompletedAt: "2026-08-08T10:11:12+0900",
		Message:     "done",
	}
}

// readDailyLines は daily ファイルの各行を JSON として読む。
func readDailyLines(t *testing.T, path string) []map[string]any {
	t.Helper()

	b, err := os.ReadFile(path) //nolint:gosec // テストが書いたパス
	if err != nil {
		t.Fatalf("ReadFile(%s) = %v", path, err)
	}
	raw := string(b)
	if !strings.HasSuffix(raw, "\n") {
		t.Errorf("daily ファイルが改行で終わっていない: %q", raw)
	}

	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		var fields map[string]any
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			t.Fatalf("行が JSON として読めない(%q): %v", line, err)
		}
		records = append(records, fields)
	}
	return records
}

func TestDailyRoot(t *testing.T) {
	t.Parallel()

	// 現行版の `$CONDUCTOR_HOME/daily/$SESSION_NAME`。
	if got, want := store.DailyRoot("/opt/conductor"), "/opt/conductor/daily"; got != want {
		t.Errorf("DailyRoot() = %q, want %q", got, want)
	}
}

func TestDailyStoreAppendCreatesDirectoryAndFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	daily := store.NewDailyStore(root, io.Discard)

	if err := daily.Append("test-session", "2026-08-08", testDailyRecord("first")); err != nil {
		t.Fatalf("Append() = %v", err)
	}

	path := filepath.Join(root, "test-session", "2026-08-08.jsonl")
	records := readDailyLines(t, path)
	if len(records) != 1 {
		t.Fatalf("%d 行, want 1 行", len(records))
	}
	want := map[string]any{
		"tab":          "first",
		"session":      "test-session",
		"completed_at": "2026-08-08T10:11:12+0900",
		"message":      "done",
		"summary":      nil,
		"markers":      map[string]any{"merged": false, "slack": false, "doc": false},
	}
	if !reflect.DeepEqual(records[0], want) {
		t.Errorf("追記された内容 =\n  %v\nwant\n  %v", records[0], want)
	}
}

func TestDailyStoreAppendKeepsExistingLines(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	daily := store.NewDailyStore(root, io.Discard)

	for _, tab := range []string{"first", "second", "third"} {
		if err := daily.Append("s", "2026-08-08", testDailyRecord(tab)); err != nil {
			t.Fatalf("Append(%s) = %v", tab, err)
		}
	}

	records := readDailyLines(t, filepath.Join(root, "s", "2026-08-08.jsonl"))
	if len(records) != 3 {
		t.Fatalf("%d 行, want 3 行", len(records))
	}
	for i, want := range []string{"first", "second", "third"} {
		if records[i]["tab"] != want {
			t.Errorf("%d 行目の tab = %v, want %q", i+1, records[i]["tab"], want)
		}
	}
}

func TestDailyStoreAppendUsesSeparateFilePerSessionAndDate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	daily := store.NewDailyStore(root, io.Discard)

	if err := daily.Append("s1", "2026-08-08", testDailyRecord("a")); err != nil {
		t.Fatalf("Append() = %v", err)
	}
	if err := daily.Append("s1", "2026-08-09", testDailyRecord("b")); err != nil {
		t.Fatalf("Append() = %v", err)
	}
	if err := daily.Append("s2", "2026-08-08", testDailyRecord("c")); err != nil {
		t.Fatalf("Append() = %v", err)
	}

	for path, wantTab := range map[string]string{
		filepath.Join(root, "s1", "2026-08-08.jsonl"): "a",
		filepath.Join(root, "s1", "2026-08-09.jsonl"): "b",
		filepath.Join(root, "s2", "2026-08-08.jsonl"): "c",
	} {
		records := readDailyLines(t, path)
		if len(records) != 1 || records[0]["tab"] != wantTab {
			t.Errorf("%s = %v, want tab %q が 1 行", path, records, wantTab)
		}
	}
}

func TestDailyStoreAppendReleasesLock(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	daily := store.NewDailyStore(root, io.Discard)

	if err := daily.Append("s", "2026-08-08", testDailyRecord("a")); err != nil {
		t.Fatalf("Append() = %v", err)
	}

	lockDir := filepath.Join(root, "s", "2026-08-08.jsonl.lock")
	if _, err := os.Stat(lockDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ロックが残っている: %s", lockDir)
	}
}

func TestDailyStoreAppendReclaimsStaleLock(t *testing.T) {
	t.Parallel()

	// 所有者プロセスが消えたロックは回収される。警告は出ない。
	root := t.TempDir()
	dir := filepath.Join(root, "s")
	if err := os.MkdirAll(filepath.Join(dir, "2026-08-08.jsonl.lock"), 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	// 存在しない PID を所有者として書く(lock_test.go と同じ手口)。
	pidPath := filepath.Join(dir, "2026-08-08.jsonl.lock", "pid")
	if err := os.WriteFile(pidPath, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	var warn bytes.Buffer
	if err := store.NewDailyStore(root, &warn).Append("s", "2026-08-08", testDailyRecord("a")); err != nil {
		t.Fatalf("Append() = %v", err)
	}
	if warn.Len() != 0 {
		t.Errorf("警告 = %q, want 出力なし", warn.String())
	}
	if records := readDailyLines(t, filepath.Join(dir, "2026-08-08.jsonl")); len(records) != 1 {
		t.Errorf("%d 行, want 1 行", len(records))
	}
}

func TestDailyStoreAppendFailsOpenWhenLockHeld(t *testing.T) {
	t.Parallel()

	// 生きているプロセスがロックを持ち続けている場合、待ち時間を過ぎたら
	// 警告を出して追記を続ける(fail-open)。ロック待ちで完了記録を失うより、
	// まれな競合を受け入れるほうが実害が小さいためである。
	root := t.TempDir()
	dir := filepath.Join(root, "s")
	lockDir := filepath.Join(dir, "2026-08-08.jsonl.lock")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	// 自プロセスの PID を置くと「所有者は生きている」と判定される。
	if err := os.WriteFile(filepath.Join(lockDir, "pid"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	var warn bytes.Buffer
	if err := store.NewDailyStore(root, &warn).Append("s", "2026-08-08", testDailyRecord("a")); err != nil {
		t.Fatalf("Append() = %v", err)
	}

	if !strings.Contains(warn.String(), "ロック") {
		t.Errorf("警告 = %q, want ロックに触れた警告", warn.String())
	}
	if records := readDailyLines(t, filepath.Join(dir, "2026-08-08.jsonl")); len(records) != 1 {
		t.Errorf("%d 行, want 1 行(ロックを取れなくても追記する)", len(records))
	}
	// 他プロセスのロックを横取りしない。
	if _, err := os.Stat(lockDir); err != nil {
		t.Errorf("保持中のロックを消してしまった: %v", err)
	}
}

func TestDailyStoreAppendReportsWriteFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("Chmod() = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	err := store.NewDailyStore(root, io.Discard).Append("s", "2026-08-08", testDailyRecord("a"))
	if err == nil {
		t.Error("Append() = nil, want エラー")
	}
}
