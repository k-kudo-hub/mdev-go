package app_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// recordTranscript は claude-conductor の test.sh セクション 20 の transcript。
const recordTranscript = `{"type":"user","message":{"role":"user","content":"hello"}}
{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"tool_use","name":"Write","input":{"file_path":"/tmp/README.md"}}],"usage":{"input_tokens":1000,"output_tokens":1000}}}
`

// newRecordOutput は fake でつないだユースケースと各 fake を返す。
func newRecordOutput(t *testing.T) (*app.RecordOutput, *fakePendingStore, *fakeTranscriptReader, *fakeDailyStore) {
	t.Helper()

	pending := newFakePendingStore()
	transcripts := newFakeTranscriptReader()
	daily := &fakeDailyStore{}
	usecase := &app.RecordOutput{
		Pending:    pending,
		Transcript: transcripts,
		Daily:      daily,
		Pricing:    fakePricingLoader{pricing: testPricing(t)},
		Clock:      testClock,
	}
	return usecase, pending, transcripts, daily
}

func TestRecordOutputIsNoOpWithoutTab(t *testing.T) {
	t.Parallel()

	// 現行版は引数が空なら pending も読まずに exit 0 する。
	usecase, pending, _, daily := newRecordOutput(t)
	pending.byTab["test-session/"] = domain.Pending{Tab: "", Message: "x"}

	if err := usecase.Execute("", app.RecordEnv{ZellijSession: "test-session"}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if len(daily.appended) != 0 {
		t.Errorf("daily に %d 件追記された, want 0", len(daily.appended))
	}
	if pending.findCalls != 0 {
		t.Errorf("pending を %d 回引いた, want 0", pending.findCalls)
	}
}

func TestRecordOutputIsNoOpWithoutPending(t *testing.T) {
	t.Parallel()

	// 該当する pending が無ければ何も書かない(現行版も exit 0)。
	usecase, _, _, daily := newRecordOutput(t)

	if err := usecase.Execute("missing-tab", app.RecordEnv{ZellijSession: "test-session"}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if len(daily.appended) != 0 {
		t.Errorf("daily に %d 件追記された, want 0", len(daily.appended))
	}
}

func TestRecordOutputDoesNotDeletePending(t *testing.T) {
	t.Parallel()

	// pending の削除は呼び出し側(タスク削除)の責務である。record 単体では
	// 消さない(現行版も pending を残す)。
	usecase, pending, _, _ := newRecordOutput(t)
	pending.byTab["test-session/record-test"] = domain.Pending{
		Tab: "record-test", Message: "Task complete", ClaudeSessionID: "sess-rec",
	}

	if err := usecase.Execute("record-test", app.RecordEnv{ZellijSession: "test-session"}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if len(pending.deleted) != 0 {
		t.Errorf("pending を削除した: %v", pending.deleted)
	}
}

func TestRecordOutputAppendsRecord(t *testing.T) {
	t.Parallel()

	usecase, pending, transcripts, daily := newRecordOutput(t)
	pending.byTab["test-session/record-test"] = domain.Pending{
		Tab:             "record-test",
		Session:         "test-session",
		ClaudeSessionID: "sess-rec",
		Message:         "Task complete",
		Event:           domain.EventStop,
		TranscriptPath:  "/tmp/t.jsonl",
		Dir:             "/tmp/myapp",
		TaskType:        "dev",
		Agent:           "claude",
	}
	transcripts.files["/tmp/t.jsonl"] = recordTranscript

	if err := usecase.Execute("record-test", app.RecordEnv{ZellijSession: "test-session"}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if len(daily.appended) != 1 {
		t.Fatalf("daily に %d 件追記された, want 1", len(daily.appended))
	}

	got := daily.appended[0]
	// 追記先はセッション名と Clock の日付で決まる。
	if got.session != "test-session" || got.date != "2026-08-08" {
		t.Errorf("追記先 = %s/%s, want test-session/2026-08-08", got.session, got.date)
	}
	// completed_at は registry の updated_at と同じ書式。
	if got.record.CompletedAt != "2026-08-08T10:11:12+0900" {
		t.Errorf("CompletedAt = %q", got.record.CompletedAt)
	}
	// tab と session は引数と環境変数から、それ以外は pending から取る。
	if got.record.Tab != "record-test" || got.record.Session != "test-session" {
		t.Errorf("tab/session = %q/%q", got.record.Tab, got.record.Session)
	}
	if got.record.Dir != "/tmp/myapp" || got.record.TaskType != "dev" ||
		got.record.ClaudeSessionID != "sess-rec" || got.record.TranscriptPath != "/tmp/t.jsonl" ||
		got.record.Agent != "claude" {
		t.Errorf("pending の内容が引き継がれていない: %+v", got.record)
	}
	if got.record.Summary == nil {
		t.Fatal("Summary = nil, want summary あり")
	}
	// 1000*5 + 1000*25 = 30000 → 0.03(fake の pricing は opus 5/25)。
	if got.record.Summary.TotalCostUSD == nil || *got.record.Summary.TotalCostUSD != 0.03 {
		t.Errorf("TotalCostUSD = %v, want 0.03", got.record.Summary.TotalCostUSD)
	}
	if !got.record.Markers.Doc {
		t.Error("Markers.Doc = false, want true")
	}
}

func TestRecordOutputWithoutTranscript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		transcriptPath string
		// files に無いパスは「ファイルが無い」ことを表す。
		registerFile bool
	}{
		{name: "transcript_path が空", transcriptPath: ""},
		{name: "transcript ファイルが無い", transcriptPath: "/tmp/gone.jsonl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			usecase, pending, _, daily := newRecordOutput(t)
			pending.byTab["test-session/no-transcript"] = domain.Pending{
				Tab: "no-transcript", TranscriptPath: tt.transcriptPath,
			}

			if err := usecase.Execute("no-transcript", app.RecordEnv{ZellijSession: "test-session"}); err != nil {
				t.Fatalf("Execute() = %v", err)
			}
			if len(daily.appended) != 1 {
				t.Fatalf("daily に %d 件追記された, want 1", len(daily.appended))
			}
			record := daily.appended[0].record
			if record.Summary != nil {
				t.Errorf("Summary = %+v, want nil", record.Summary)
			}
			if record.Message != domain.NoSummaryMessage {
				t.Errorf("Message = %q, want %q", record.Message, domain.NoSummaryMessage)
			}
		})
	}
}

func TestRecordOutputUsesUnknownSessionOutsideZellij(t *testing.T) {
	t.Parallel()

	// ZELLIJ_SESSION_NAME が無ければ "unknown" セッション扱いになる。
	usecase, pending, _, daily := newRecordOutput(t)
	pending.byTab["unknown/solo-tab"] = domain.Pending{Tab: "solo-tab", Message: "done"}

	if err := usecase.Execute("solo-tab", app.RecordEnv{}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if len(daily.appended) != 1 {
		t.Fatalf("daily に %d 件追記された, want 1", len(daily.appended))
	}
	if got := daily.appended[0]; got.session != domain.DefaultSessionName {
		t.Errorf("追記先セッション = %q, want %q", got.session, domain.DefaultSessionName)
	}
	if got := daily.appended[0].record.Session; got != domain.DefaultSessionName {
		t.Errorf("record.Session = %q, want %q", got, domain.DefaultSessionName)
	}
}

func TestRecordOutputPropagatesErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("書けない")

	t.Run("pending の探索に失敗", func(t *testing.T) {
		t.Parallel()
		usecase, pending, _, daily := newRecordOutput(t)
		pending.findErr = wantErr

		if err := usecase.Execute("tab", app.RecordEnv{}); !errors.Is(err, wantErr) {
			t.Errorf("Execute() = %v, want %v を含む", err, wantErr)
		}
		if len(daily.appended) != 0 {
			t.Error("探索に失敗したのに追記された")
		}
	})

	t.Run("daily への追記に失敗", func(t *testing.T) {
		t.Parallel()
		usecase, pending, _, daily := newRecordOutput(t)
		pending.byTab["unknown/tab"] = domain.Pending{Tab: "tab"}
		daily.appendErr = wantErr

		if err := usecase.Execute("tab", app.RecordEnv{}); !errors.Is(err, wantErr) {
			t.Errorf("Execute() = %v, want %v を含む", err, wantErr)
		}
	})
}

func TestRecordOutputUsesCodexAggregationForCodexAgent(t *testing.T) {
	t.Parallel()

	usecase, pending, transcripts, daily := newRecordOutput(t)
	pending.byTab["test-session/cx"] = domain.Pending{
		Tab: "cx", TranscriptPath: "/tmp/cx.jsonl", Agent: domain.AgentCodex,
	}
	transcripts.files["/tmp/cx.jsonl"] = `{"type":"event_msg","payload":{"type":"user_message","message":"a"}}` + "\n"

	if err := usecase.Execute("cx", app.RecordEnv{ZellijSession: "test-session"}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	summary := daily.appended[0].record.Summary
	if summary == nil {
		t.Fatal("Summary = nil")
	}
	if summary.TotalTurns != 1 {
		t.Errorf("TotalTurns = %d, want 1(codex の user_message で数える)", summary.TotalTurns)
	}
	// codex の summary は cache_write_tokens を持つ。
	if summary.CacheWriteTokens == nil || summary.CacheWrite5mTokens != nil {
		t.Errorf("キャッシュ書き込みのキーが codex 用になっていない: %+v", summary)
	}
}

func TestRecordOutputPassesPendingThrough(t *testing.T) {
	t.Parallel()

	// pending からレコードへ渡る値の対応表。tab と session だけは
	// pending ではなく引数・環境変数が優先される。
	usecase, pending, _, daily := newRecordOutput(t)
	pending.byTab["env-session/arg-tab"] = domain.Pending{
		Tab:             "pending-tab",
		Session:         "pending-session",
		Message:         "msg",
		Dir:             "/d",
		TaskType:        "review",
		ClaudeSessionID: "sid",
		Agent:           "codex",
	}

	if err := usecase.Execute("arg-tab", app.RecordEnv{ZellijSession: "env-session"}); err != nil {
		t.Fatalf("Execute() = %v", err)
	}

	want := domain.DailyRecord{
		Tab:             "arg-tab",
		Session:         "env-session",
		CompletedAt:     "2026-08-08T10:11:12+0900",
		Message:         "msg",
		Dir:             "/d",
		TaskType:        "review",
		ClaudeSessionID: "sid",
		Agent:           "codex",
	}
	if got := daily.appended[0].record; !reflect.DeepEqual(got, want) {
		t.Errorf("record =\n  %+v\nwant\n  %+v", got, want)
	}
}
