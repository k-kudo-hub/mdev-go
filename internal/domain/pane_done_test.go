package domain_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// linesOf は JSONL 文字列を daily の行の並びに変換する。
func linesOf(s string) [][]byte {
	out := [][]byte{}
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		out = append(out, []byte(line))
	}
	return out
}

func TestBuildDoneViewAggregates(t *testing.T) {
	t.Parallel()

	// restored のエントリは件数にも合計にも入らない。
	view := domain.BuildDoneView(linesOf(`
{"tab":"alpha","session":"s1","completed_at":"2026-08-09T10:00:00+0900","summary":{"total_turns":3,"total_tool_calls":5,"total_cost_usd":0.42}}
{"tab":"skipped","session":"s1","completed_at":"2026-08-09T09:00:00+0900","restored":true,"summary":{"total_turns":99,"total_tool_calls":99,"total_cost_usd":9.99}}
{"tab":"beta","session":"s2","completed_at":"2026-08-09T11:00:00+0900","summary":{"total_turns":2,"total_tool_calls":2,"total_cost_usd":1}}
`))

	if view.Count != 2 {
		t.Errorf("Count = %d, want 2", view.Count)
	}
	if view.Turns != "5" || view.Calls != "7" || view.Cost != "$1.42" {
		t.Errorf("統計 = %s turns / %s calls / %s, want 5 / 7 / $1.42", view.Turns, view.Calls, view.Cost)
	}
}

func TestBuildDoneViewSortsByCompletedAtAscending(t *testing.T) {
	t.Parallel()

	// ファイル内の並びではなく completed_at の昇順になる。
	view := domain.BuildDoneView(linesOf(`
{"tab":"late","session":"s1","completed_at":"2026-08-09T13:00:00+0900","summary":{"total_turns":1,"total_tool_calls":1,"total_cost_usd":0.1}}
{"tab":"early","session":"s1","completed_at":"2026-08-09T09:00:00+0900","summary":{"total_turns":1,"total_tool_calls":1,"total_cost_usd":0.1}}
{"tab":"mid","session":"s2","completed_at":"2026-08-09T11:00:00+0900","summary":{"total_turns":1,"total_tool_calls":1,"total_cost_usd":0.1}}
`))

	want := []string{"early", "mid", "late"}
	got := make([]string, 0, len(view.Rows))
	for _, r := range view.Rows {
		got = append(got, r.Tab)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("並び = %v, want %v", got, want)
	}
}

func TestBuildDoneViewKeepsInputOrderOnTies(t *testing.T) {
	t.Parallel()

	// jq の sort_by は安定なので、completed_at が同じなら入力順が保たれる
	// (実測で確認済み)。
	view := domain.BuildDoneView(linesOf(`
{"tab":"first","session":"s1","completed_at":"2026-08-09T10:00:00+0900","summary":{"total_turns":1,"total_tool_calls":1,"total_cost_usd":0}}
{"tab":"second","session":"s1","completed_at":"2026-08-09T10:00:00+0900","summary":{"total_turns":1,"total_tool_calls":1,"total_cost_usd":0}}
{"tab":"third","session":"s1","completed_at":"2026-08-09T10:00:00+0900","summary":{"total_turns":1,"total_tool_calls":1,"total_cost_usd":0}}
`))

	want := []string{"first", "second", "third"}
	got := make([]string, 0, len(view.Rows))
	for _, r := range view.Rows {
		got = append(got, r.Tab)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("同着の並び = %v, want %v", got, want)
	}
}

func TestBuildDoneViewOneBrokenLineHidesEverything(t *testing.T) {
	t.Parallel()

	// 現行は全ファイルを `jq -s` に流すため、1 行でも壊れていると集計ごと
	// 失敗し、正常なエントリまで表示されなくなる。この既知バグを再現する。
	view := domain.BuildDoneView(linesOf(`
{"tab":"good","session":"s1","completed_at":"2026-08-09T10:00:00+0900","summary":{"total_turns":1,"total_tool_calls":1,"total_cost_usd":0.1}}
{broken json
`))

	if view.Count != 0 || len(view.Rows) != 0 {
		t.Errorf("壊れ行があるのに表示されている: Count=%d Rows=%d", view.Count, len(view.Rows))
	}
}

func TestBuildDoneViewRowFields(t *testing.T) {
	t.Parallel()

	view := domain.BuildDoneView(linesOf(`
{"tab":"alpha","session":"s1","completed_at":"2026-08-09T10:00:00+0900","summary":{"total_turns":3,"total_tool_calls":5,"total_cost_usd":0.42},"markers":{"merged":true,"slack":false,"doc":true}}
{"tab":"nosummary","session":"s1","completed_at":"2026-08-09T13:00:00+0900","summary":null}
`))

	want := []domain.DoneRow{
		{
			Tab: "alpha", Session: "s1", CompletedAt: "2026-08-09T10:00:00+0900",
			// 時刻は completed_at の 11..15 文字目。
			Turns: "3", Cost: "$0.42", Time: "10:00", Markers: "🚀📝",
		},
		{
			// summary が null なら turns も cost も "-" になる。
			Tab: "nosummary", Session: "s1", CompletedAt: "2026-08-09T13:00:00+0900",
			Turns: "-", Cost: "-", Time: "13:00", Markers: "",
		},
	}
	if !reflect.DeepEqual(view.Rows, want) {
		t.Errorf("Rows = %+v\nwant %+v", view.Rows, want)
	}
}

func TestBuildDoneViewShiftsFieldsWhenSessionIsMissing(t *testing.T) {
	t.Parallel()

	// 現行は jq が組んだ TSV を `IFS=<tab> read` で読み直す。タブは IFS の
	// 空白文字なので連続タブが 1 つに畳まれ、空フィールドがあると以降が
	// 1 つずつ手前へずれる。session が無いエントリで実際に起きる既知バグで、
	// 表示だけでなく restore の引数もずれる。実測で確認して再現している。
	view := domain.BuildDoneView(linesOf(`
{"tab":"first","completed_at":"2026-08-09T10:00:00+0900","summary":{"total_turns":1,"total_tool_calls":1,"total_cost_usd":0}}
`))

	want := []domain.DoneRow{{
		Tab:         "first",
		Session:     "2026-08-09T10:00:00+0900", // 本来 completed_at
		CompletedAt: "1",                        // 本来 turns
		Turns:       "$0.00",                    // 本来 cost
		Cost:        "10:00",                    // 本来 time
		Time:        "",                         // markers が空なので空のまま
		Markers:     "",
	}}
	if !reflect.DeepEqual(view.Rows, want) {
		t.Errorf("ずれた Rows = %+v\nwant %+v", view.Rows, want)
	}
}

func TestBuildDoneViewCostFormatting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cost string
		want string
	}{
		{name: "小数 2 桁はそのまま", cost: "0.42", want: "$0.42"},
		{name: "整数は .00 を足す", cost: "1", want: "$1.00"},
		{name: "小数 1 桁は 0 を足す", cost: "0.1", want: "$0.10"},
		{name: "0 は $0.00", cost: "0", want: "$0.00"},
		{name: "3 桁目は四捨五入", cost: "0.005", want: "$0.01"},
		{name: "丸めて 0 になる値も $0.00", cost: "0.001", want: "$0.00"},
		{name: "大きい値", cost: "12.5", want: "$12.50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			view := domain.BuildDoneView(linesOf(
				`{"tab":"t","session":"s","completed_at":"2026-08-09T10:00:00+0900",` +
					`"summary":{"total_turns":1,"total_tool_calls":1,"total_cost_usd":` + tt.cost + `}}`))
			if len(view.Rows) != 1 {
				t.Fatalf("行が 1 つではない: %+v", view.Rows)
			}
			if view.Rows[0].Cost != tt.want {
				t.Errorf("Cost = %q, want %q", view.Rows[0].Cost, tt.want)
			}
		})
	}
}

// 現行 done-loop.sh の ONCE 出力を隔離環境で実測して写したもの。
// tab は %-14s、turns は %3s、cost は %7s で、いずれも bash の printf に
// 合わせてバイト幅で詰める(zsh の printf は文字幅なので混同しないこと)。
const wantDoneTwoRows = "\x1b[1m  Done Tasks\x1b[0m\n" +
	"\x1b[2m  ──────────────────────────\x1b[0m\n" +
	"\n" +
	"  \x1b[0;33m\x1b[1m2\x1b[0m tasks  \x1b[2m5 turns / 7 calls / $1.42\x1b[0m\n" +
	"\n" +
	"  \x1b[0;33m[1]\x1b[0m \x1b[0;32m⚡\x1b[0m alpha            3 t    $0.42  \x1b[2m[10:00]\x1b[0m 🚀📝\n" +
	"  \x1b[0;33m[2]\x1b[0m \x1b[0;32m⚡\x1b[0m beta             2 t    $1.00  \x1b[2m[11:00]\x1b[0m\n" +
	"\n" +
	"\x1b[2m  ──────────────────────────\x1b[0m\n" +
	"  \x1b[2mr+[num]: restore to dashboard\x1b[0m\n"

const wantDoneEmpty = "\x1b[1m  Done Tasks\x1b[0m\n" +
	"\x1b[2m  ──────────────────────────\x1b[0m\n" +
	"\n" +
	"  \x1b[2mNo tasks completed yet\x1b[0m\n"

func TestRenderDone(t *testing.T) {
	t.Parallel()

	view := domain.BuildDoneView(linesOf(`
{"tab":"alpha","session":"s1","completed_at":"2026-08-09T10:00:00+0900","summary":{"total_turns":3,"total_tool_calls":5,"total_cost_usd":0.42},"markers":{"merged":true,"slack":false,"doc":true}}
{"tab":"beta","session":"s2","completed_at":"2026-08-09T11:00:00+0900","summary":{"total_turns":2,"total_tool_calls":2,"total_cost_usd":1},"markers":{"merged":false,"slack":false,"doc":false}}
`))

	if got := domain.RenderDone(view); got != wantDoneTwoRows {
		t.Errorf("RenderDone() の出力が違う\n got: %q\nwant: %q", got, wantDoneTwoRows)
	}
}

func TestRenderDoneEmpty(t *testing.T) {
	t.Parallel()

	if got := domain.RenderDone(domain.BuildDoneView(nil)); got != wantDoneEmpty {
		t.Errorf("RenderDone() の空表示が違う\n got: %q\nwant: %q", got, wantDoneEmpty)
	}
}

func TestRenderDonePadsByBytesNotRunes(t *testing.T) {
	t.Parallel()

	// bash の printf はバイト幅で詰めるため、15 バイトの日本語タブ名は
	// %-14s の幅を超えて一切詰められない(桁がずれる既知の見た目バグ)。
	view := domain.BuildDoneView(linesOf(
		`{"tab":"日本語タブ","session":"s1","completed_at":"2026-08-09T11:00:00+0900",` +
			`"summary":{"total_turns":2,"total_tool_calls":2,"total_cost_usd":1}}`))

	if got := domain.RenderDone(view); !strings.Contains(got, "⚡\x1b[0m 日本語タブ   2 t") {
		t.Errorf("バイト幅で詰められていない: %q", got)
	}
}
