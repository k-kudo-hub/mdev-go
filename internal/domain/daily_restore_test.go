package domain_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// dailyLines は JSON Lines をテストの入力に整える。
func dailyLines(lines ...string) [][]byte {
	out := make([][]byte, 0, len(lines))
	for _, line := range lines {
		out = append(out, []byte(line))
	}
	return out
}

const (
	restoreMe = `{"tab":"restore-me","session":"s","completed_at":"2026-08-11T10:00:00+0900",` +
		`"dir":"/w/proj","task_type":"dev","claude_session_id":"sess-1",` +
		`"transcript_path":"/w/t.jsonl","agent":"claude"}`
	restoreDone = `{"tab":"restore-me","session":"s","completed_at":"2026-08-11T10:00:00+0900",` +
		`"dir":"/w/old","restored":true}`
	restoreOther = `{"tab":"other","session":"s","completed_at":"2026-08-11T09:00:00+0900","dir":"/w/other"}`
)

// TestFindRestorableDaily は復元対象の 1 件の選び方を固定する。
//
// 現行 restore-task.sh は
// `jq -c 'select(.tab == $t and .completed_at == $c and (.restored // false) != true)' | head -1`
// で選ぶ。
func TestFindRestorableDaily(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		lines       [][]byte
		tab         string
		completedAt string
		want        domain.DailyRestoreTarget
		wantFound   bool
	}{
		{
			name:        "一致する 1 件を返す",
			lines:       dailyLines(restoreOther, restoreMe),
			tab:         "restore-me",
			completedAt: "2026-08-11T10:00:00+0900",
			want: domain.DailyRestoreTarget{
				Dir: "/w/proj", TaskType: "dev", ClaudeSessionID: "sess-1",
				TranscriptPath: "/w/t.jsonl", Agent: "claude",
			},
			wantFound: true,
		},
		{
			name:        "restored 済みは対象外",
			lines:       dailyLines(restoreDone),
			tab:         "restore-me",
			completedAt: "2026-08-11T10:00:00+0900",
		},
		{
			name:        "restored 済みを飛ばして未復元を拾う",
			lines:       dailyLines(restoreDone, restoreMe),
			tab:         "restore-me",
			completedAt: "2026-08-11T10:00:00+0900",
			want: domain.DailyRestoreTarget{
				Dir: "/w/proj", TaskType: "dev", ClaudeSessionID: "sess-1",
				TranscriptPath: "/w/t.jsonl", Agent: "claude",
			},
			wantFound: true,
		},
		{
			name:        "完了時刻が違えば一致しない",
			lines:       dailyLines(restoreMe),
			tab:         "restore-me",
			completedAt: "2026-08-11T11:00:00+0900",
		},
		{
			name:        "タブ名が違えば一致しない",
			lines:       dailyLines(restoreMe),
			tab:         "another",
			completedAt: "2026-08-11T10:00:00+0900",
		},
		{
			name:        "同じ (tab, completed_at) が並ぶなら先頭を返す",
			lines:       dailyLines(restoreMe, strings.Replace(restoreMe, "/w/proj", "/w/second", 1)),
			tab:         "restore-me",
			completedAt: "2026-08-11T10:00:00+0900",
			want: domain.DailyRestoreTarget{
				Dir: "/w/proj", TaskType: "dev", ClaudeSessionID: "sess-1",
				TranscriptPath: "/w/t.jsonl", Agent: "claude",
			},
			wantFound: true,
		},
		{
			name:        "空行は読み飛ばす",
			lines:       dailyLines("", "  ", restoreMe),
			tab:         "restore-me",
			completedAt: "2026-08-11T10:00:00+0900",
			want: domain.DailyRestoreTarget{
				Dir: "/w/proj", TaskType: "dev", ClaudeSessionID: "sess-1",
				TranscriptPath: "/w/t.jsonl", Agent: "claude",
			},
			wantFound: true,
		},
		{
			name: "壊れた行より前に見つかったものは返る",
			// jq は流し読みなので、壊れた行に当たるまでの出力は残る。
			lines:       dailyLines(restoreMe, "{broken"),
			tab:         "restore-me",
			completedAt: "2026-08-11T10:00:00+0900",
			want: domain.DailyRestoreTarget{
				Dir: "/w/proj", TaskType: "dev", ClaudeSessionID: "sess-1",
				TranscriptPath: "/w/t.jsonl", Agent: "claude",
			},
			wantFound: true,
		},
		{
			name:        "壊れた行より後ろは読まない",
			lines:       dailyLines("{broken", restoreMe),
			tab:         "restore-me",
			completedAt: "2026-08-11T10:00:00+0900",
		},
		{
			name:        "1 件も無い",
			lines:       nil,
			tab:         "restore-me",
			completedAt: "2026-08-11T10:00:00+0900",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, found := domain.FindRestorableDaily(tt.lines, tt.tab, tt.completedAt)
			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v", found, tt.wantFound)
			}
			if found && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FindRestorableDaily() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestMarkRestoredDaily は restored: true の付け方を固定する。
func TestMarkRestoredDaily(t *testing.T) {
	t.Parallel()

	const at = "2026-08-11T10:00:00+0900"

	tests := []struct {
		name    string
		content string
		want    string
		wantOK  bool
	}{
		{
			name:    "一致した行にだけ足す",
			content: restoreOther + "\n" + restoreMe + "\n",
			want: restoreOther + "\n" +
				strings.TrimSuffix(restoreMe, "}") + `,"restored":true}` + "\n",
			wantOK: true,
		},
		{
			name: "同じ (tab, completed_at) が並んでも最初の 1 件だけ",
			// (tab, completed_at) は一意な鍵ではない。作り直したタブは 1 つ
			// なので、片方は Done に残さなければならない(test.sh 26g)。
			content: restoreMe + "\n" + restoreMe + "\n",
			want: strings.TrimSuffix(restoreMe, "}") + `,"restored":true}` + "\n" +
				restoreMe + "\n",
			wantOK: true,
		},
		{
			name:    "既に restored 済みの行は飛ばす",
			content: restoreDone + "\n" + restoreMe + "\n",
			want: restoreDone + "\n" +
				strings.TrimSuffix(restoreMe, "}") + `,"restored":true}` + "\n",
			wantOK: true,
		},
		{
			name:    "一致が無ければそのまま",
			content: restoreOther + "\n",
			want:    restoreOther + "\n",
			wantOK:  true,
		},
		{
			name:    "空行は落ちる(現行の jq -s も出さない)",
			content: "\n" + restoreOther + "\n\n",
			want:    restoreOther + "\n",
			wantOK:  true,
		},
		{
			name: "1 行でも壊れていれば書き直さない",
			// 現行版は `jq -s` が全体で失敗して mv へ進まない(exit 5)。
			content: restoreMe + "\n{broken\n",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := domain.MarkRestoredDaily([]byte(tt.content), "restore-me", at)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && string(got) != tt.want {
				t.Errorf("MarkRestoredDaily() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// TestMarkRestoredDailyKeepsUntouchedLinesVerbatim は、触っていない行を
// バイト列のまま残すことを固定する。
//
// 現行版は `jq -s ... | .[]` でファイル全体を出し直すため、無関係な行まで
// jq の整形に置き換わる(数値表記や空白が正規化される)。Go 版は対象行にだけ
// キーを差し込み、他は 1 バイトも変えない(evidence §5-2)。
func TestMarkRestoredDailyKeepsUntouchedLinesVerbatim(t *testing.T) {
	t.Parallel()

	const verbatim = `{"tab":"keep", "completed_at":"x", "summary":{"total_tokens":1600000}, "cost":9.50}`
	content := verbatim + "\n" + restoreMe + "\n"

	got, ok := domain.MarkRestoredDaily([]byte(content), "restore-me", "2026-08-11T10:00:00+0900")
	if !ok {
		t.Fatal("書き直せなかった")
	}
	if !strings.HasPrefix(string(got), verbatim+"\n") {
		t.Errorf("無関係な行が書き換わった:\n%s", got)
	}
}
