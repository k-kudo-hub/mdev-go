package domain_test

import (
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestParseScreenState は状態ファイルの 1 行形式を現行版と同じ規則で読むことを
// 固定する。
//
// 現行 screen-detect-lib.sh:138-141 は
//
//	prev_raw=$(cat "$state_file" 2>/dev/null || true)
//	prev="${prev_raw%% *}"
//	prev_at="${prev_raw#* }"
//	[[ "$prev_at" == "$prev_raw" ]] && prev_at=""
//
// で読む。コマンド置換が末尾の改行を落とす点も含めて再現する。
func TestParseScreenState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want domain.ScreenState
	}{
		{
			name: "空(初回観測)",
			raw:  "",
			want: domain.ScreenState{},
		},
		{
			name: "時刻を持たない状態",
			raw:  "working\n",
			want: domain.ScreenState{State: domain.ScreenWorking},
		},
		{
			name: "idle_pending は時刻を伴う",
			raw:  "idle_pending 1754870400\n",
			want: domain.ScreenState{State: domain.ScreenIdlePending, At: "1754870400"},
		},
		{
			name: "末尾の改行は複数でも落とす",
			raw:  "idle\n\n",
			want: domain.ScreenState{State: domain.ScreenIdle},
		},
		{
			name: "2 つ目以降の空白は時刻側に残る(現行の ${raw#* } と同じ)",
			raw:  "idle_pending 1 2",
			want: domain.ScreenState{State: domain.ScreenIdlePending, At: "1 2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := domain.ParseScreenState(tt.raw); got != tt.want {
				t.Errorf("ParseScreenState(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}

// TestScreenStateFormat は状態ファイルへ書く 1 行の組み立てを固定する
// (現行版の `echo "$effective" > "$state_file"`)。
func TestScreenStateFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state domain.ScreenState
		want  string
	}{
		{
			name:  "時刻が無ければ状態だけ",
			state: domain.ScreenState{State: domain.ScreenWorking},
			want:  "working",
		},
		{
			name:  "idle_pending は空白区切りで時刻を伴う",
			state: domain.ScreenState{State: domain.ScreenIdlePending, At: "1754870400"},
			want:  "idle_pending 1754870400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.state.Format(); got != tt.want {
				t.Errorf("Format() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestScreenStatePendingSince は保留開始時刻の読み取りを固定する。
//
// 現行版は `[[ "$prev_at" =~ ^[0-9]+$ ]]` で数値かどうかを見て、数値でなければ
// 経過時間の判定を飛ばして**確定側へ倒す**。ok=false がその合図になる。
func TestScreenStatePendingSince(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state domain.ScreenState
		want  int64
		wantO bool
	}{
		{name: "数字だけなら読める", state: domain.ScreenState{At: "1754870400"}, want: 1754870400, wantO: true},
		{name: "空は読めない", state: domain.ScreenState{At: ""}, wantO: false},
		{name: "非数値は読めない", state: domain.ScreenState{At: "abc"}, wantO: false},
		{name: "符号付きは読めない", state: domain.ScreenState{At: "-1"}, wantO: false},
		{name: "空白混じりは読めない", state: domain.ScreenState{At: "1 2"}, wantO: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := tt.state.PendingSince()
			if ok != tt.wantO || (ok && got != tt.want) {
				t.Errorf("PendingSince() = (%d, %v), want (%d, %v)", got, ok, tt.want, tt.wantO)
			}
		})
	}
}
