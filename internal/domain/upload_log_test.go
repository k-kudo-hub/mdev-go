package domain_test

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestBuildLogPath は test.sh「43. build_log_path」の期待値と、現行版へ実際に
// 同じ引数を渡して確かめた境界の挙動を固定する。
func TestBuildLogPath(t *testing.T) {
	tests := []struct {
		name        string
		baseDir     string
		completedAt string
		taskname    string
		want        string
	}{
		{
			name:        "test.sh の期待値",
			baseDir:     "work-log",
			completedAt: "2026-07-04T15:30:12+0900",
			taskname:    "my task/name",
			want:        "work-log/2026/07/04/153012_my-task-name.md",
		},
		{
			// 使える文字が 1 つも無いと空になり、既定名へ落ちる。
			name:        "使えない文字だけの名前は task になる",
			baseDir:     "work-log",
			completedAt: "2026-07-04T15:30:12+0900",
			taskname:    "日本語 タスク",
			want:        "work-log/2026/07/04/153012_task.md",
		},
		{
			name:        "ハイフンだけの名前も task になる",
			baseDir:     "work-log",
			completedAt: "2026-07-04T15:30:12+0900",
			taskname:    "---",
			want:        "work-log/2026/07/04/153012_task.md",
		},
		{
			name:        "先頭と末尾のハイフンだけを落とす",
			baseDir:     "work-log",
			completedAt: "2026-07-04T15:30:12+0900",
			taskname:    "--lead-and-trail--",
			want:        "work-log/2026/07/04/153012_lead-and-trail.md",
		},
		{
			name:        "使える文字はそのまま残す",
			baseDir:     "logs/sub",
			completedAt: "2026-12-31T23:59:59Z",
			taskname:    "ok_name.v1",
			want:        "logs/sub/2026/12/31/235959_ok_name.v1.md",
		},
		{
			// 固定オフセットの切り出しなので、短い文字列でもエラーにはならない。
			name:        "completed_at が空でも空の区切りが並ぶだけ",
			baseDir:     "work-log",
			completedAt: "",
			taskname:    "x",
			want:        "work-log////_x.md",
		},
		{
			name:        "completed_at が途中までなら取れた分だけ入る",
			baseDir:     "work-log",
			completedAt: "2026-07",
			taskname:    "x",
			want:        "work-log/2026/07//_x.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.BuildLogPath(tt.baseDir, tt.completedAt, tt.taskname)
			if got != tt.want {
				t.Errorf("BuildLogPath(%q, %q, %q) = %q, want %q",
					tt.baseDir, tt.completedAt, tt.taskname, got, tt.want)
			}
		})
	}
}

// markdown は期待値を組み立てる補助である。11 の値を並び順で受け取る。
func markdown(tab, session, completedAt, model, turns, calls, cost, tools, merged, slack, doc, summary string) string {
	return strings.Join([]string{
		"# " + tab,
		"",
		"- **Session**: " + session,
		"- **Completed**: " + completedAt,
		"- **Model**: " + model,
		"",
		"## サマリ",
		"",
		"| 項目 | 値 |",
		"|---|---|",
		"| ターン数 | " + turns + " |",
		"| ツール呼び出し | " + calls + " |",
		"| コスト(USD) | " + cost + " |",
		"| 使用ツール | " + tools + " |",
		"| マージ | " + merged + " |",
		"| Slack | " + slack + " |",
		"| ドキュメント | " + doc + " |",
		"",
		"## 会話要約",
		"",
		summary,
	}, "\n")
}

// TestBuildMarkdown は test.sh「43. build_markdown」の期待値を移植したもので、
// 完全な期待文字列との一致で確かめる。
func TestBuildMarkdown(t *testing.T) {
	tests := []struct {
		name    string
		record  string
		summary string
		want    string
	}{
		{
			// test.sh のレコードそのもの。.message は出力に含まれてはならない。
			name: "test.sh のレコード",
			record: `{"tab":"demo-task","session":"s1","completed_at":"2026-07-04T15:30:12+0900",` +
				`"message":"RAWMESSAGEMARKER should not appear",` +
				`"summary":{"model":"claude-opus-4-6","total_turns":3,"total_tool_calls":5,` +
				`"total_cost_usd":0.42,"tools_used":["Edit","Bash"]},` +
				`"markers":{"merged":true,"slack":false,"doc":true}}`,
			summary: "- 要約テスト行",
			want: markdown("demo-task", "s1", "2026-07-04T15:30:12+0900", "claude-opus-4-6",
				"3", "5", "0.42", "Edit, Bash", "✅", "-", "✅", "- 要約テスト行"),
		},
		{
			// 要約はモデルが書くので、会話に出た秘密を書き戻すことがある。
			name: "要約に混ざった秘密を最終出力でも伏せる",
			record: `{"tab":"t","session":"s","completed_at":"c",` +
				`"summary":{"model":"m","total_turns":1,"total_tool_calls":2,` +
				`"total_cost_usd":1,"tools_used":["A"]},"markers":{}}`,
			summary: "- leaked ghp_abcdefghijklmnopqrstuvwxyz0123456789",
			want: markdown("t", "s", "c", "m", "1", "2", "1", "A", "-", "-", "-",
				"- leaked ***REDACTED***"),
		},
		{
			// jq は数値の字面を保つ(1.0 は 1 にならない)。
			name: "数値は入力の字面のまま出す",
			record: `{"tab":"t","session":"s","completed_at":"c",` +
				`"summary":{"model":"m","total_turns":1,"total_tool_calls":2,` +
				`"total_cost_usd":1.0,"tools_used":[]},"markers":{}}`,
			summary: "S",
			want:    markdown("t", "s", "c", "m", "1", "2", "1.0", "", "-", "-", "-", "S"),
		},
		{
			// summary が null / markers が空のプレースホルダレコード。
			// 現行版はここで @tsv の空フィールドが畳まれて値がずれるが、
			// ここでは既定値をそのまま出す(BuildMarkdown のコメントを参照)。
			name:    "プレースホルダレコードは既定値で埋める",
			record:  `{"tab":"t","session":"s","completed_at":"","summary":null,"markers":{}}`,
			summary: "S",
			want:    markdown("t", "s", "", uploadUnknownForTest, "0", "0", "0", "", "-", "-", "-", "S"),
		},
		{
			name:    "キーが 1 つも無ければ全部が既定値になる",
			record:  `{}`,
			summary: "S",
			want: markdown(uploadUnknownForTest, uploadUnknownForTest, "", uploadUnknownForTest,
				"0", "0", "0", "", "-", "-", "-", "S"),
		},
		{
			// jq の真偽判定。null と false だけが偽で、それ以外は真になる。
			name:    "markers は null と false だけが偽になる",
			record:  `{"tab":"t","session":"s","completed_at":"c","markers":{"merged":"no","slack":0,"doc":null}}`,
			summary: "S",
			want: markdown("t", "s", "c", uploadUnknownForTest, "0", "0", "0", "",
				"✅", "✅", "-", "S"),
		},
		{
			// join は null を空文字、文字列以外を tojson の表記にする。
			name: "tools_used は null を空文字にして繋ぐ",
			record: `{"tab":"t","session":"s","completed_at":"c",` +
				`"summary":{"tools_used":["a",null,5]},"markers":{}}`,
			summary: "S",
			want: markdown("t", "s", "c", uploadUnknownForTest, "0", "0", "0", "a, , 5",
				"-", "-", "-", "S"),
		},
		{
			// 現行版は jq がエラーになり read が空のまま返る。
			name:    "summary がスカラーなら全フィールドが空になる",
			record:  `{"tab":"t","summary":5}`,
			summary: "S",
			want:    markdown("", "", "", "", "", "", "", "", "", "", "", "S"),
		},
		{
			name:    "markers がスカラーなら全フィールドが空になる",
			record:  `{"tab":"t","markers":5}`,
			summary: "S",
			want:    markdown("", "", "", "", "", "", "", "", "", "", "", "S"),
		},
		{
			name:    "tools_used が配列でなければ全フィールドが空になる",
			record:  `{"tab":"t","summary":{"tools_used":"x"}}`,
			summary: "S",
			want:    markdown("", "", "", "", "", "", "", "", "", "", "", "S"),
		},
		{
			name:    "レコードがオブジェクトでなければ全フィールドが空になる",
			record:  `[1]`,
			summary: "S",
			want:    markdown("", "", "", "", "", "", "", "", "", "", "", "S"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.BuildMarkdown([]byte(tt.record), tt.summary)
			if got != tt.want {
				t.Errorf("BuildMarkdown = \n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// uploadUnknownForTest は domain 側の既定値をテストから参照するための写しである。
const uploadUnknownForTest = "unknown"

// TestBuildMarkdownExcludesRawMessage は「アップロードするのはサマリと会話要約
// だけ」という方針を固定する。daily レコードの .message には作業の生の報告文が
// 入っており、これがログリポジトリへ出てはならない。
func TestBuildMarkdownExcludesRawMessage(t *testing.T) {
	record := `{"tab":"t","session":"s","completed_at":"c","message":"RAWMESSAGEMARKER",` +
		`"summary":{"model":"m","total_turns":1,"total_tool_calls":1,"total_cost_usd":1,"tools_used":[]},` +
		`"markers":{}}`
	got := domain.BuildMarkdown([]byte(record), "S")
	if strings.Contains(got, "RAWMESSAGEMARKER") {
		t.Errorf("生の message がアップロード内容に含まれています: %q", got)
	}
}

// TestRecordCompletedAt は `.completed_at // empty` の写しを固定する。
func TestRecordCompletedAt(t *testing.T) {
	tests := []struct {
		name   string
		record string
		want   string
	}{
		{"値がある", `{"completed_at":"2026-07-04T15:30:12+0900"}`, "2026-07-04T15:30:12+0900"},
		{"空文字は空のまま", `{"completed_at":""}`, ""},
		{"null は空", `{"completed_at":null}`, ""},
		{"キーが無ければ空", `{}`, ""},
		{"オブジェクトでなければ空", `5`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.RecordCompletedAt([]byte(tt.record)); got != tt.want {
				t.Errorf("RecordCompletedAt(%s) = %q, want %q", tt.record, got, tt.want)
			}
		})
	}
}
