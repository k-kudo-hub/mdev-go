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

// testDedupeRecord は置換の検証に使うレコード。dedupe キーは tab と
// claude_session_id の組であり、message で世代を見分ける。
func testDedupeRecord(tab, claudeSessionID, message string) domain.DailyRecord {
	return domain.DailyRecord{
		Tab:             tab,
		Session:         "test-session",
		CompletedAt:     "2026-08-08T10:11:12+0900",
		Message:         message,
		ClaudeSessionID: claudeSessionID,
	}
}

// dailyMessages は daily ファイルの各行の message を並び順のまま返す。
func dailyMessages(t *testing.T, path string) []string {
	t.Helper()

	var messages []string
	for _, record := range readDailyLines(t, path) {
		message, _ := record["message"].(string)
		messages = append(messages, message)
	}
	return messages
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

// writeExistingDaily は実行前から存在する daily ファイルを作る。
func writeExistingDaily(t *testing.T, path string, lines ...string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
}

func TestDailyStoreAppendReplacesSameTabAndClaudeSessionID(t *testing.T) {
	t.Parallel()

	// アップロード失敗で dd が中止されると record は同じ pending に対して
	// 何度も走る。同じ (tab, claude_session_id) の再実行は行を増やさず、
	// 内容だけを置き換える。
	root := t.TempDir()
	daily := store.NewDailyStore(root, io.Discard)

	if err := daily.Append("s", "2026-08-08", testDedupeRecord("dedupe-tab", "sid-A", "first")); err != nil {
		t.Fatalf("Append(1 回目) = %v", err)
	}
	if err := daily.Append("s", "2026-08-08", testDedupeRecord("dedupe-tab", "sid-A", "second")); err != nil {
		t.Fatalf("Append(2 回目) = %v", err)
	}

	path := filepath.Join(root, "s", "2026-08-08.jsonl")
	if got, want := dailyMessages(t, path), []string{"second"}; !reflect.DeepEqual(got, want) {
		t.Errorf("message の並び = %v, want %v", got, want)
	}
}

func TestDailyStoreAppendKeepsOtherClaudeSessionID(t *testing.T) {
	t.Parallel()

	// --resume でセッション ID が変わった場合は別のタスクの記録なので、
	// 同じタブでも別行として残す。
	root := t.TempDir()
	daily := store.NewDailyStore(root, io.Discard)

	for _, record := range []domain.DailyRecord{
		testDedupeRecord("dedupe-tab", "sid-A", "first"),
		testDedupeRecord("dedupe-tab", "sid-B", "second"),
	} {
		if err := daily.Append("s", "2026-08-08", record); err != nil {
			t.Fatalf("Append() = %v", err)
		}
	}

	path := filepath.Join(root, "s", "2026-08-08.jsonl")
	if got, want := dailyMessages(t, path), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Errorf("message の並び = %v, want %v", got, want)
	}
}

func TestDailyStoreAppendKeepsOtherTab(t *testing.T) {
	t.Parallel()

	// 同じ claude_session_id でもタブが違えば別の記録として残す。
	root := t.TempDir()
	daily := store.NewDailyStore(root, io.Discard)

	for _, record := range []domain.DailyRecord{
		testDedupeRecord("tab-a", "sid-A", "first"),
		testDedupeRecord("tab-b", "sid-A", "second"),
	} {
		if err := daily.Append("s", "2026-08-08", record); err != nil {
			t.Fatalf("Append() = %v", err)
		}
	}

	path := filepath.Join(root, "s", "2026-08-08.jsonl")
	if got, want := dailyMessages(t, path), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Errorf("message の並び = %v, want %v", got, want)
	}
}

func TestDailyStoreAppendKeepsRestoredEntries(t *testing.T) {
	t.Parallel()

	// restored: true はダッシュボードへ戻したタスクの履歴である。同じ
	// dedupe キーでも消さず、新しい記録はその後ろへ足す。
	root := t.TempDir()
	path := filepath.Join(root, "s", "2026-08-08.jsonl")
	writeExistingDaily(t, path,
		`{"tab":"dedupe-tab","claude_session_id":"sid-A","message":"restored-one","restored":true}`,
		`{"tab":"dedupe-tab","claude_session_id":"sid-A","message":"live-one"}`,
	)

	err := store.NewDailyStore(root, io.Discard).
		Append("s", "2026-08-08", testDedupeRecord("dedupe-tab", "sid-A", "fresh"))
	if err != nil {
		t.Fatalf("Append() = %v", err)
	}

	if got, want := dailyMessages(t, path), []string{"restored-one", "fresh"}; !reflect.DeepEqual(got, want) {
		t.Errorf("message の並び = %v, want %v", got, want)
	}
}

func TestDailyStoreAppendWithoutClaudeSessionIDAlwaysAppends(t *testing.T) {
	t.Parallel()

	// claude_session_id が無いレコードは dedupe キーを持たないため、
	// 移植前と同じく無条件に追記する。
	root := t.TempDir()
	daily := store.NewDailyStore(root, io.Discard)

	for _, message := range []string{"first", "second"} {
		if err := daily.Append("s", "2026-08-08", testDedupeRecord("dedupe-tab", "", message)); err != nil {
			t.Fatalf("Append() = %v", err)
		}
	}

	path := filepath.Join(root, "s", "2026-08-08.jsonl")
	if got, want := dailyMessages(t, path), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Errorf("message の並び = %v, want %v", got, want)
	}
}

func TestDailyStoreAppendMovesReplacedEntryToTail(t *testing.T) {
	t.Parallel()

	// 置換は「一致行を消して末尾へ追記」である。残った行の相対順序は変わらず、
	// 置換された行は最終行へ移る(Done ペインは書かれた順に読む)。
	root := t.TempDir()
	path := filepath.Join(root, "s", "2026-08-08.jsonl")
	writeExistingDaily(t, path,
		`{"tab":"dedupe-tab","claude_session_id":"sid-A","message":"old"}`,
		`{"tab":"other-tab","claude_session_id":"sid-B","message":"keep-1"}`,
		`{"tab":"other-tab","claude_session_id":"sid-C","message":"keep-2"}`,
	)

	err := store.NewDailyStore(root, io.Discard).
		Append("s", "2026-08-08", testDedupeRecord("dedupe-tab", "sid-A", "new"))
	if err != nil {
		t.Fatalf("Append() = %v", err)
	}

	want := []string{"keep-1", "keep-2", "new"}
	if got := dailyMessages(t, path); !reflect.DeepEqual(got, want) {
		t.Errorf("message の並び = %v, want %v", got, want)
	}
}

func TestDailyStoreAppendRemovesEveryMatchingEntry(t *testing.T) {
	t.Parallel()

	// 置換前から重複している場合(移植前に増殖した daily)も 1 回の追記で
	// まとめて 1 行へ畳む。
	root := t.TempDir()
	path := filepath.Join(root, "s", "2026-08-08.jsonl")
	writeExistingDaily(t, path,
		`{"tab":"dedupe-tab","claude_session_id":"sid-A","message":"old-1"}`,
		`{"tab":"dedupe-tab","claude_session_id":"sid-A","message":"old-2"}`,
	)

	err := store.NewDailyStore(root, io.Discard).
		Append("s", "2026-08-08", testDedupeRecord("dedupe-tab", "sid-A", "new"))
	if err != nil {
		t.Fatalf("Append() = %v", err)
	}

	if got, want := dailyMessages(t, path), []string{"new"}; !reflect.DeepEqual(got, want) {
		t.Errorf("message の並び = %v, want %v", got, want)
	}
}

func TestDailyStoreAppendKeepsBrokenDailyIntact(t *testing.T) {
	t.Parallel()

	// 既存 daily を読めないときは削除を諦めてそのまま追記する。
	// 重複した記録は後から消せるが、切り詰めた記録は取り戻せない。
	root := t.TempDir()
	path := filepath.Join(root, "s", "2026-08-08.jsonl")
	writeExistingDaily(t, path,
		`{"tab":"dedupe-tab","claude_session_id":"sid-A","message":"old"}`,
		`これは JSON ではない`,
	)

	err := store.NewDailyStore(root, io.Discard).
		Append("s", "2026-08-08", testDedupeRecord("dedupe-tab", "sid-A", "new"))
	if err != nil {
		t.Fatalf("Append() = %v", err)
	}

	b, err := os.ReadFile(path) //nolint:gosec // テストが書いたパス
	if err != nil {
		t.Fatalf("ReadFile() = %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("%d 行, want 3 行(既存 2 行 + 追記 1 行): %q", len(lines), string(b))
	}
	if !strings.Contains(lines[0], `"message":"old"`) || lines[1] != "これは JSON ではない" {
		t.Errorf("既存の行が書き換わった: %q", string(b))
	}
	if !strings.Contains(lines[2], `"message":"new"`) {
		t.Errorf("追記されていない: %q", lines[2])
	}
}

func TestDailyStoreAppendReleasesLockAfterReplace(t *testing.T) {
	t.Parallel()

	// 置換の経路(削除 → rename → 追記)を通ってもロックは持ち越さない。
	root := t.TempDir()
	daily := store.NewDailyStore(root, io.Discard)

	for _, message := range []string{"first", "second"} {
		if err := daily.Append("s", "2026-08-08", testDedupeRecord("dedupe-tab", "sid-A", message)); err != nil {
			t.Fatalf("Append() = %v", err)
		}
	}

	lockDir := filepath.Join(root, "s", "2026-08-08.jsonl.lock")
	if _, err := os.Stat(lockDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ロックが残っている: %s", lockDir)
	}
}

func TestDailyStoreAppendKeepsScreenSessionEntries(t *testing.T) {
	t.Parallel()

	// スクリーン検出の claude_session_id はタブ名から作った合成値で、同じ名前の
	// タブなら別のタスクでも同じになる。これを置換キーにすると過去のタスクの
	// 記録まで消えるため、置換せず追記する(重複は残るが履歴は失わない)。
	root := t.TempDir()
	daily := store.NewDailyStore(root, io.Discard)

	sid := domain.ScreenSessionIDPrefix + "dedupe-tab-2917289248"
	for _, message := range []string{"first", "second"} {
		if err := daily.Append("s", "2026-08-08", testDedupeRecord("dedupe-tab", sid, message)); err != nil {
			t.Fatalf("Append() = %v", err)
		}
	}

	path := filepath.Join(root, "s", "2026-08-08.jsonl")
	if got, want := dailyMessages(t, path), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Errorf("message の並び = %v, want %v", got, want)
	}
}

func TestDailyStoreAppendSkipsReplacementWhenLockUnavailable(t *testing.T) {
	t.Parallel()

	// ロックを取れないまま置換すると、ファイル全体の書き直しが並行する restore の
	// 結果(restored: true の付与)を巻き戻しかねない。追記だけなら失うものは
	// 無いため、fail-open のときは置換をあきらめて追記に徹する。
	root := t.TempDir()
	path := filepath.Join(root, "s", "2026-08-08.jsonl")
	writeExistingDaily(t, path,
		`{"tab":"dedupe-tab","claude_session_id":"sid-A","message":"old"}`,
	)
	lockDir := path + ".lock"
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	// 自プロセスの PID を置くと「所有者は生きている」と判定され、待ち時間を
	// 過ぎるまでロックは空かない。
	if err := os.WriteFile(filepath.Join(lockDir, "pid"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	var warn bytes.Buffer
	err := store.NewDailyStore(root, &warn).
		Append("s", "2026-08-08", testDedupeRecord("dedupe-tab", "sid-A", "new"))
	if err != nil {
		t.Fatalf("Append() = %v", err)
	}

	if got, want := dailyMessages(t, path), []string{"old", "new"}; !reflect.DeepEqual(got, want) {
		t.Errorf("message の並び = %v, want %v(ロック無しでは置換しない)", got, want)
	}
	if !strings.Contains(warn.String(), "ロック") {
		t.Errorf("警告 = %q, want ロックに触れた警告", warn.String())
	}
}

// TestDailyStoreFindRestorable は Done から復元する 1 件の引き方を固定する。
func TestDailyStoreFindRestorable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := store.NewDailyStore(root, io.Discard)

	const at = "2026-08-11T10:00:00+0900"
	dir := filepath.Join(root, "s1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("ディレクトリの作成に失敗: %v", err)
	}
	content := `{"tab":"t","completed_at":"` + at + `","dir":"/w","task_type":"dev",` +
		`"claude_session_id":"sid","transcript_path":"/w/t.jsonl","agent":"codex"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "2026-08-11.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("daily の作成に失敗: %v", err)
	}

	got, ok := s.FindRestorable("s1", "2026-08-11", "t", at)
	if !ok {
		t.Fatal("見つからなかった")
	}
	want := domain.DailyRestoreTarget{
		Dir: "/w", TaskType: "dev", ClaudeSessionID: "sid",
		TranscriptPath: "/w/t.jsonl", Agent: "codex",
	}
	if got != want {
		t.Errorf("FindRestorable() = %+v, want %+v", got, want)
	}

	// ファイルが無い日付・セッションは「見つからない」に落ちる。
	if _, ok := s.FindRestorable("s1", "2026-08-10", "t", at); ok {
		t.Error("存在しない日付で見つかったと返した")
	}
	if _, ok := s.FindRestorable("missing", "2026-08-11", "t", at); ok {
		t.Error("存在しないセッションで見つかったと返した")
	}
}

// TestDailyStoreMarkRestored は restored: true の書き戻しを固定する。
func TestDailyStoreMarkRestored(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := store.NewDailyStore(root, io.Discard)

	const at = "2026-08-11T12:00:00+0900"
	dir := filepath.Join(root, "s1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("ディレクトリの作成に失敗: %v", err)
	}
	path := filepath.Join(dir, "2026-08-11.jsonl")
	line := `{"tab":"t","completed_at":"` + at + `","dir":"/w"}`
	if err := os.WriteFile(path, []byte(line+"\n"+line+"\n"), 0o600); err != nil {
		t.Fatalf("daily の作成に失敗: %v", err)
	}

	if err := s.MarkRestored("s1", "2026-08-11", "t", at); err != nil {
		t.Fatalf("MarkRestored() = %v", err)
	}

	b, err := os.ReadFile(path) //nolint:gosec // テストの一時ディレクトリ
	if err != nil {
		t.Fatalf("読み直しに失敗: %v", err)
	}
	// 同じ (tab, completed_at) が 2 件あっても片方だけ反転する。
	if got := strings.Count(string(b), `"restored":true`); got != 1 {
		t.Errorf("反転した件数 = %d, want 1\n%s", got, b)
	}
}

// TestDailyStoreMarkRestoredFailsOnBrokenFile は 1 行でも壊れていれば
// 書き戻さずエラーにすることを固定する(現行版の exit 5)。
func TestDailyStoreMarkRestoredFailsOnBrokenFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := store.NewDailyStore(root, io.Discard)

	const at = "2026-08-11T12:00:00+0900"
	dir := filepath.Join(root, "s1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("ディレクトリの作成に失敗: %v", err)
	}
	path := filepath.Join(dir, "2026-08-11.jsonl")
	content := `{"tab":"t","completed_at":"` + at + `","dir":"/w"}` + "\n{broken\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("daily の作成に失敗: %v", err)
	}

	if err := s.MarkRestored("s1", "2026-08-11", "t", at); err == nil {
		t.Error("MarkRestored() = nil, want エラー")
	}
	b, err := os.ReadFile(path) //nolint:gosec // テストの一時ディレクトリ
	if err != nil {
		t.Fatalf("読み直しに失敗: %v", err)
	}
	if string(b) != content {
		t.Errorf("壊れたファイルを書き換えた:\n%s", b)
	}
}

// TestDailyStoreMarkRestoredFailsWithoutFile はファイルが無い場合を固定する。
func TestDailyStoreMarkRestoredFailsWithoutFile(t *testing.T) {
	t.Parallel()

	s := store.NewDailyStore(t.TempDir(), io.Discard)
	if err := s.MarkRestored("s1", "2026-08-11", "t", "x"); err == nil {
		t.Error("MarkRestored() = nil, want エラー")
	}
}
