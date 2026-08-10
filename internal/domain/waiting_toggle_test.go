package domain_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// waiting-toggle.sh の jq フィルタ 2 本を移植したものを固定する。
//
//	Waiting のとき: .event = (.prev_event // "Notification") | del(.prev_event) | .time = $time
//	それ以外      : .prev_event = .event | .event = "Waiting" | .time = $time
//
// 期待値の根拠は test.sh 36。

// decodeJSON は JSON をキーの対応表に落とす(比較用)。
func decodeJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("結果が JSON として読めない: %v (%s)", err, b)
	}
	return m
}

func TestToggleWaiting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want map[string]any
	}{
		{
			name: "Notification は Waiting になり prev_event に退避される",
			in:   `{"tab":"pr-review","session":"s","message":"review","event":"Notification","time":"10:00:00"}`,
			want: map[string]any{
				"tab": "pr-review", "session": "s", "message": "review",
				"event": "Waiting", "time": "11:22:33", "prev_event": "Notification",
			},
		},
		{
			name: "Stop も同じく退避される(完了タスクは done へ戻れる)",
			in:   `{"tab":"done-task","session":"s","message":"m","event":"Stop","time":"10:00:00"}`,
			want: map[string]any{
				"tab": "done-task", "session": "s", "message": "m",
				"event": "Waiting", "time": "11:22:33", "prev_event": "Stop",
			},
		},
		{
			name: "Waiting は prev_event へ戻り prev_event 自体は消える",
			in:   `{"tab":"t","session":"s","event":"Waiting","time":"10:00:00","prev_event":"Stop"}`,
			want: map[string]any{
				"tab": "t", "session": "s", "event": "Stop", "time": "11:22:33",
			},
		},
		{
			name: "prev_event が無い Waiting は Notification へ戻る",
			in:   `{"tab":"t","session":"s","event":"Waiting","time":"10:00:00"}`,
			want: map[string]any{
				"tab": "t", "session": "s", "event": "Notification", "time": "11:22:33",
			},
		},
		{
			name: "prev_event が null の Waiting も Notification へ戻る(jq の // 相当)",
			in:   `{"tab":"t","event":"Waiting","time":"10:00:00","prev_event":null}`,
			want: map[string]any{
				"tab": "t", "event": "Notification", "time": "11:22:33",
			},
		},
		{
			name: "Waiting へ入るとき既にある prev_event は上書きされる",
			in:   `{"tab":"t","event":"Notification","time":"10:00:00","prev_event":"Stop"}`,
			want: map[string]any{
				"tab": "t", "event": "Waiting", "time": "11:22:33", "prev_event": "Notification",
			},
		},
		{
			name: "未知のキーは触らずに持ち越す",
			in:   `{"tab":"t","event":"Stop","time":"10:00:00","transcript_path":"/p","future_key":{"a":1}}`,
			want: map[string]any{
				"tab": "t", "event": "Waiting", "time": "11:22:33", "prev_event": "Stop",
				"transcript_path": "/p", "future_key": map[string]any{"a": float64(1)},
			},
		},
		{
			name: "event キーが無い場合は prev_event が null になる(jq の .event 参照と同じ)",
			in:   `{"tab":"t","time":"10:00:00"}`,
			want: map[string]any{
				"tab": "t", "event": "Waiting", "time": "11:22:33", "prev_event": nil,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := domain.ToggleWaiting([]byte(tc.in), "11:22:33")
			if !ok {
				t.Fatalf("ToggleWaiting が失敗した: %s", tc.in)
			}
			if diff := decodeJSON(t, got); !reflect.DeepEqual(diff, tc.want) {
				t.Errorf("ToggleWaiting() = %v, want %v", diff, tc.want)
			}
		})
	}
}

func TestToggleWaitingKeepsKeyOrder(t *testing.T) {
	t.Parallel()

	// jq は既存キーの位置を保ち、新しいキーだけを末尾に足す。同じ順で書くと
	// 現行版と並べたときの差分が値だけになり、突き合わせが読みやすい。
	in := `{"tab":"t","session":"s","event":"Stop","time":"10:00:00"}`
	got, ok := domain.ToggleWaiting([]byte(in), "11:22:33")
	if !ok {
		t.Fatal("ToggleWaiting が失敗した")
	}
	want := `{"tab":"t","session":"s","event":"Waiting","time":"11:22:33","prev_event":"Stop"}`
	if string(got) != want {
		t.Errorf("ToggleWaiting() = %s, want %s", got, want)
	}
}

func TestToggleWaitingRejectsBrokenInput(t *testing.T) {
	t.Parallel()

	// 現行版は jq が失敗したら一時ファイルを捨てて元のファイルを保つ。
	// ok=false は「何も書き換えない」の合図である。
	for _, in := range []string{"", "not json", "[1,2]", `"string"`, "null"} {
		if _, ok := domain.ToggleWaiting([]byte(in), "11:22:33"); ok {
			t.Errorf("ToggleWaiting(%q) が成功してしまった", in)
		}
	}
}
