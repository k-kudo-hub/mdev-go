package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// fakeDailySessionReader は 1 セッションぶんの daily ファイルを返す。
type fakeDailySessionReader struct {
	files    [][]byte
	sessions []string
}

func (f *fakeDailySessionReader) ReadSession(session string) [][]byte {
	f.sessions = append(f.sessions, session)
	return f.files
}

// fakeSummarizer は要約の代わりに、渡された会話をそのまま覚えて返す。
type fakeSummarizer struct {
	// got は Summarize に渡された会話(マスク済みのはず)。
	got string
	// summary は返す要約。
	summary string
	err     error
}

func (f *fakeSummarizer) Summarize(conversation string) (string, error) {
	f.got = conversation
	if f.err != nil {
		return "", f.err
	}
	return f.summary, nil
}

// fakePusher は push の引数を覚える。
type fakePusher struct {
	repo, branch, relPath, content string
	calls                          int
	reference                      string
	err                            error
}

func (f *fakePusher) Push(repo, branch, relPath, content string) (string, error) {
	f.calls++
	f.repo, f.branch, f.relPath, f.content = repo, branch, relPath, content
	if f.err != nil {
		return "", f.err
	}
	return f.reference, nil
}

// uploadFixture はアップロードのユースケースと差し替えた依存をまとめて返す。
type uploadFixture struct {
	uploader   *app.LogUploader
	config     *fakeConfigLoader
	pending    *fakePendingStore
	transcript *fakeTranscriptReader
	daily      *fakeDailySessionReader
	summarizer *fakeSummarizer
	pusher     *fakePusher
}

// claudeTranscript は要約が取れる最小の claude transcript である。
const claudeTranscript = `{"type":"user","message":{"content":"do the thing"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}
`

func newUploadFixture(t *testing.T) *uploadFixture {
	t.Helper()
	config := &fakeConfigLoader{config: domain.Config{Upload: domain.UploadConfig{
		Enabled: true,
		Repo:    "owner/logs",
		BaseDir: "work-log",
		Branch:  "main",
	}}}
	pending := newFakePendingStore()
	pending.byTab[pendingKey("test-session", "demo")] = domain.Pending{
		Tab:            "demo",
		TranscriptPath: "/t/demo.jsonl",
	}
	transcript := newFakeTranscriptReader()
	transcript.files["/t/demo.jsonl"] = claudeTranscript
	daily := &fakeDailySessionReader{}
	summarizer := &fakeSummarizer{summary: "- 要約"}
	pusher := &fakePusher{reference: "https://example/log.md"}

	return &uploadFixture{
		uploader: &app.LogUploader{
			Config:     config,
			Pending:    pending,
			Transcript: transcript,
			Daily:      daily,
			Summarizer: summarizer,
			Pusher:     pusher,
			Clock:      testClock,
		},
		config:     config,
		pending:    pending,
		transcript: transcript,
		daily:      daily,
		summarizer: summarizer,
		pusher:     pusher,
	}
}

var uploadEnv = app.PaneEnv{ZellijSession: "test-session"}

// TestUploadLogSuccess は成功経路を確かめる。
func TestUploadLogSuccess(t *testing.T) {
	f := newUploadFixture(t)
	f.daily.files = [][]byte{[]byte(`{"tab":"demo","session":"test-session",` +
		`"completed_at":"2026-07-04T15:30:12+0900",` +
		`"summary":{"model":"m","total_turns":3,"total_tool_calls":5,` +
		`"total_cost_usd":0.42,"tools_used":["Edit"]},"markers":{"merged":true}}`)}

	got, err := f.uploader.UploadLog(uploadEnv, "demo")
	if err != nil {
		t.Fatalf("UploadLog が失敗しました: %v", err)
	}
	if want := "アップロードしました -> https://example/log.md"; got != want {
		t.Errorf("戻り値 = %q, want %q", got, want)
	}
	if f.pusher.repo != "owner/logs" || f.pusher.branch != "main" {
		t.Errorf("push 先 = %q / %q, want owner/logs / main", f.pusher.repo, f.pusher.branch)
	}
	// レコードの completed_at から日付ごとの置き場所が決まる。
	if want := "work-log/2026/07/04/153012_demo.md"; f.pusher.relPath != want {
		t.Errorf("相対パス = %q, want %q", f.pusher.relPath, want)
	}
	for _, want := range []string{"# demo", "| コスト(USD) | 0.42 |", "- 要約"} {
		if !strings.Contains(f.pusher.content, want) {
			t.Errorf("markdown に %q がありません:\n%s", want, f.pusher.content)
		}
	}
}

// TestUploadLogSkips は「飛ばす」経路が ("", nil) になることを確かめる。
// ここで error を返すと、アップロードを使っていない利用者の dd が全部止まる。
func TestUploadLogSkips(t *testing.T) {
	tests := []struct {
		name  string
		tab   string
		setup func(f *uploadFixture)
	}{
		{
			name:  "タブ名が空",
			tab:   "",
			setup: func(*uploadFixture) {},
		},
		{
			name: "アップロードが無効",
			tab:  "demo",
			setup: func(f *uploadFixture) {
				f.config.config.Upload.Enabled = false
			},
		},
		{
			name: "リポジトリが未設定",
			tab:  "demo",
			setup: func(f *uploadFixture) {
				f.config.config.Upload.Repo = ""
			},
		},
		{
			name: "設定を読めない",
			tab:  "demo",
			setup: func(f *uploadFixture) {
				f.config.config = domain.Config{}
				f.config.failed = true
			},
		},
		{
			name:  "対象の pending が無い",
			tab:   "other",
			setup: func(*uploadFixture) {},
		},
		{
			name: "pending の読み取りに失敗",
			tab:  "demo",
			setup: func(f *uploadFixture) {
				f.pending.findErr = errors.New("読めない")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newUploadFixture(t)
			tt.setup(f)

			got, err := f.uploader.UploadLog(uploadEnv, tt.tab)
			if err != nil {
				t.Fatalf("飛ばすはずが error になりました: %v", err)
			}
			if got != "" {
				t.Errorf("戻り値 = %q, want 空(表示するものは無い)", got)
			}
			if f.pusher.calls != 0 {
				t.Errorf("push が %d 回呼ばれました, want 0", f.pusher.calls)
			}
		})
	}
}

// TestUploadLogFailures は失敗が error になり、**push もされない**ことを
// 確かめる。呼び出し側はこの error でタブの削除を中止する。
func TestUploadLogFailures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *uploadFixture)
	}{
		{
			name: "transcript のパスが無い",
			setup: func(f *uploadFixture) {
				f.pending.byTab[pendingKey("test-session", "demo")] = domain.Pending{Tab: "demo"}
			},
		},
		{
			name: "transcript を読めない",
			setup: func(f *uploadFixture) {
				delete(f.transcript.files, "/t/demo.jsonl")
			},
		},
		{
			name: "会話を取り出せない transcript",
			setup: func(f *uploadFixture) {
				f.transcript.files["/t/demo.jsonl"] = `{"type":"session_meta","payload":{}}` + "\n"
			},
		},
		{
			name: "要約に失敗",
			setup: func(f *uploadFixture) {
				f.summarizer.err = errors.New("claude が落ちた")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newUploadFixture(t)
			tt.setup(f)

			got, err := f.uploader.UploadLog(uploadEnv, "demo")
			if err == nil {
				t.Fatalf("error になりませんでした(戻り値 = %q)", got)
			}
			if got != "" {
				t.Errorf("失敗時の戻り値 = %q, want 空", got)
			}
			if f.pusher.calls != 0 {
				t.Errorf("失敗しているのに push が %d 回呼ばれました", f.pusher.calls)
			}
		})
	}
}

// TestUploadLogPushFailure は push の失敗が error になることを確かめる。
func TestUploadLogPushFailure(t *testing.T) {
	f := newUploadFixture(t)
	f.pusher.err = errors.New("push できない")

	got, err := f.uploader.UploadLog(uploadEnv, "demo")
	if err == nil {
		t.Fatalf("error になりませんでした(戻り値 = %q)", got)
	}
	if got != "" {
		t.Errorf("失敗時の戻り値 = %q, want 空", got)
	}
}

// TestUploadLogMasksConversationBeforeSummary は 1 回目のマスク(要約へ渡す前)を
// 確かめる。会話そのものをモデルへ送る経路なので、ここが抜けると秘密が外へ出る。
func TestUploadLogMasksConversationBeforeSummary(t *testing.T) {
	f := newUploadFixture(t)
	f.transcript.files["/t/demo.jsonl"] =
		`{"type":"user","message":{"content":"token ghp_abcdefghijklmnopqrstuvwxyz0123456789"}}` + "\n"

	if _, err := f.uploader.UploadLog(uploadEnv, "demo"); err != nil {
		t.Fatalf("UploadLog が失敗しました: %v", err)
	}
	if strings.Contains(f.summarizer.got, "ghp_abcdef") {
		t.Errorf("要約へ渡す会話に秘密が残っています: %q", f.summarizer.got)
	}
	if !strings.Contains(f.summarizer.got, "***REDACTED***") {
		t.Errorf("要約へ渡す会話がマスクされていません: %q", f.summarizer.got)
	}
	// コマンド置換と同じく末尾の改行は落とす。
	if strings.HasSuffix(f.summarizer.got, "\n") {
		t.Errorf("要約へ渡す会話の末尾に改行が残っています: %q", f.summarizer.got)
	}
}

// TestUploadLogMasksSummaryInOutput は 2 回目のマスク(最終出力)を確かめる。
// 要約を書くのはモデルなので、会話に出た秘密を書き戻すことがある。
func TestUploadLogMasksSummaryInOutput(t *testing.T) {
	f := newUploadFixture(t)
	f.summarizer.summary = "- 誤って ghp_abcdefghijklmnopqrstuvwxyz0123456789 を含む要約"

	if _, err := f.uploader.UploadLog(uploadEnv, "demo"); err != nil {
		t.Fatalf("UploadLog が失敗しました: %v", err)
	}
	if strings.Contains(f.pusher.content, "ghp_abcdef") {
		t.Errorf("push する内容に秘密が残っています:\n%s", f.pusher.content)
	}
}

// TestUploadLogExcludesRawMessage は daily レコードの生の報告文が
// アップロードされないことを確かめる。
func TestUploadLogExcludesRawMessage(t *testing.T) {
	f := newUploadFixture(t)
	f.daily.files = [][]byte{[]byte(`{"tab":"demo","session":"test-session",` +
		`"completed_at":"2026-07-04T15:30:12+0900","message":"RAWMESSAGEMARKER",` +
		`"summary":{"tools_used":[]},"markers":{}}`)}

	if _, err := f.uploader.UploadLog(uploadEnv, "demo"); err != nil {
		t.Fatalf("UploadLog が失敗しました: %v", err)
	}
	if strings.Contains(f.pusher.content, "RAWMESSAGEMARKER") {
		t.Errorf("生の message がアップロードされています:\n%s", f.pusher.content)
	}
}

// TestUploadLogUsesPlaceholderWhenNoRecord は daily に記録が無いときの経路を
// 確かめる。記録が無くても会話要約だけは残す。
func TestUploadLogUsesPlaceholderWhenNoRecord(t *testing.T) {
	f := newUploadFixture(t)

	if _, err := f.uploader.UploadLog(uploadEnv, "demo"); err != nil {
		t.Fatalf("UploadLog が失敗しました: %v", err)
	}
	// completed_at が無いので現在時刻(fakeClock)から置き場所が決まる。
	if want := "work-log/2026/08/08/101112_demo.md"; f.pusher.relPath != want {
		t.Errorf("相対パス = %q, want %q", f.pusher.relPath, want)
	}
	for _, want := range []string{"# demo", "- **Session**: test-session", "- **Model**: unknown"} {
		if !strings.Contains(f.pusher.content, want) {
			t.Errorf("markdown に %q がありません:\n%s", want, f.pusher.content)
		}
	}
}

// TestUploadLogUsesCrossDayRecord は test.sh「47. cross-day」に対応する。
// 記録が当日以外の daily にしか無くても、その統計と日付を使う。
func TestUploadLogUsesCrossDayRecord(t *testing.T) {
	f := newUploadFixture(t)
	f.daily.files = [][]byte{
		[]byte(`{"tab":"demo","session":"test-session","completed_at":"2020-01-01T09:00:00+0900",` +
			`"summary":{"model":"m","total_turns":7,"total_tool_calls":9,` +
			`"total_cost_usd":1.23,"tools_used":["Bash"]},"markers":{}}`),
		[]byte(`{"tab":"someone-else","session":"test-session"}`),
	}

	if _, err := f.uploader.UploadLog(uploadEnv, "demo"); err != nil {
		t.Fatalf("UploadLog が失敗しました: %v", err)
	}
	if want := "work-log/2020/01/01/090000_demo.md"; f.pusher.relPath != want {
		t.Errorf("相対パス = %q, want %q", f.pusher.relPath, want)
	}
	if !strings.Contains(f.pusher.content, "| コスト(USD) | 1.23 |") {
		t.Errorf("統計が引き継がれていません:\n%s", f.pusher.content)
	}
	if got := f.daily.sessions; len(got) != 1 || got[0] != "test-session" {
		t.Errorf("読んだセッション = %v, want [test-session]", got)
	}
}

// TestUploadLogSkipsScreenPendingWithoutTranscript は会話ゼロの codex タブを
// 削除できることを確かめる。
//
// スクリーン検出は hook を持たないエージェントのために pending を **合成する**。
// 1 ターンも会話していないタブでは transcript がまだ無く、合成 pending は
// transcript_path を持たない。これをアップロード対象にすると
// 「transcript のパスが記録されていません」で hard fail し、タブの削除が
// 永久にブロックされる。**守るべき会話が無いのに会話を守るための防御が
// 働く**という設計矛盾で、利用者から見るとタブが消せなくなる。
func TestUploadLogSkipsScreenPendingWithoutTranscript(t *testing.T) {
	f := newUploadFixture(t)
	f.pending.byTab[pendingKey("test-session", "demo")] = domain.Pending{
		Tab:             "demo",
		ClaudeSessionID: domain.ScreenPendingSessionID("demo"),
		Agent:           "codex",
		// transcript_path は無い(1 ターンも会話していない)。
	}

	got, err := f.uploader.UploadLog(uploadEnv, "demo")
	if err != nil {
		t.Fatalf("UploadLog = %v, want nil(削除を止めてはいけない)", err)
	}
	if got != "" {
		t.Errorf("戻り値 = %q, want 空(飛ばした)", got)
	}
	if f.pusher.calls != 0 {
		t.Errorf("push = %d 回, want 0", f.pusher.calls)
	}
	if f.summarizer.got != "" {
		t.Errorf("要約を作ろうとした: %q", f.summarizer.got)
	}
}

// TestUploadLogUploadsScreenPendingWithTranscript は合成 pending でも
// transcript があれば従来どおりアップロードすることを確かめる。
//
// 会話が始まった後のタブは、スクリーン検出が所有していても記録すべき中身を
// 持っている。飛ばしてよいのは「会話の記録が無い」ことを自ら示している
// 場合だけである。
func TestUploadLogUploadsScreenPendingWithTranscript(t *testing.T) {
	f := newUploadFixture(t)
	f.pending.byTab[pendingKey("test-session", "demo")] = domain.Pending{
		Tab:             "demo",
		ClaudeSessionID: domain.ScreenPendingSessionID("demo"),
		Agent:           "codex",
		TranscriptPath:  "/t/demo.jsonl",
	}

	got, err := f.uploader.UploadLog(uploadEnv, "demo")
	if err != nil {
		t.Fatalf("UploadLog が失敗しました: %v", err)
	}
	if want := "アップロードしました -> https://example/log.md"; got != want {
		t.Errorf("戻り値 = %q, want %q", got, want)
	}
	if f.pusher.calls != 1 {
		t.Errorf("push = %d 回, want 1", f.pusher.calls)
	}
}

// TestUploadLogFailsForRealSessionWithoutTranscript は実セッションの
// pending では従来どおり失敗することを確かめる。
//
// **飛ばしてよい範囲を広げてはならない。** hook が書いた pending に
// transcript が無いのは異常であり、そのまま削除を通すと会話が失われる。
func TestUploadLogFailsForRealSessionWithoutTranscript(t *testing.T) {
	f := newUploadFixture(t)
	f.pending.byTab[pendingKey("test-session", "demo")] = domain.Pending{
		Tab:             "demo",
		ClaudeSessionID: "019ffa99-28ef-7d93-9d02-a606a979e0b7",
	}

	got, err := f.uploader.UploadLog(uploadEnv, "demo")
	if err == nil {
		t.Fatal("エラーを返すはず(削除を止めなければ会話が失われる)")
	}
	if got != "" {
		t.Errorf("戻り値 = %q, want 空", got)
	}
	if f.pusher.calls != 0 {
		t.Errorf("push = %d 回, want 0", f.pusher.calls)
	}
}
