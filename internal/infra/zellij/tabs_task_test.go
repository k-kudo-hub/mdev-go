package zellij

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// タスク作成が使う zellij 操作の引数の並びを固定する。
//
// 期待値の根拠は claude-conductor v0.7.4 の task-lib.sh(create_task /
// apply_layout)と task-control.sh である。並びが 1 語でも違うと zellij が
// 黙って別のことをするため(存在しないタブ名への go-to-tab-name は rc=0 の
// no-op になる)、コマンド行そのものを突き合わせる。

// recorder は timeout 付きの実行関数を記録する。
type recorder struct {
	calls    [][]string
	timeouts []time.Duration
	out      string
	// outErr は output が返す失敗。空の出力と区別するために持つ。
	outErr error
	err    error
}

func (r *recorder) output(timeout time.Duration, name string, args ...string) (string, error) {
	r.record(timeout, name, args)
	return r.out, r.outErr
}

func (r *recorder) run(timeout time.Duration, name string, args ...string) error {
	r.record(timeout, name, args)
	return r.err
}

func (r *recorder) record(timeout time.Duration, name string, args []string) {
	r.calls = append(r.calls, append([]string{name}, args...))
	r.timeouts = append(r.timeouts, timeout)
}

func (r *recorder) controller() *TabController {
	return &TabController{output: r.output, run: r.run}
}

func TestTabControllerTaskActions(t *testing.T) {
	t.Parallel()

	const limit = 3 * time.Second

	tests := []struct {
		name string
		call func(c *TabController)
		want []string
	}{
		{
			name: "new-tab はコマンドを -- の後ろへ置く",
			call: func(c *TabController) {
				_ = c.NewTab(limit, "my-task", "/tmp/proj",
					[]string{"env", "TASK_TAB_NAME=my-task", "claude"})
			},
			want: []string{"zellij", "action", "new-tab", "-n", "my-task", "--cwd", "/tmp/proj",
				"--", "env", "TASK_TAB_NAME=my-task", "claude"},
		},
		{
			name: "コマンドが無い new-tab は -- を付けない",
			call: func(c *TabController) { _ = c.NewTab(limit, "bare", "/tmp", nil) },
			want: []string{"zellij", "action", "new-tab", "-n", "bare", "--cwd", "/tmp"},
		},
		{
			name: "new-pane はコマンド付き",
			call: func(c *TabController) {
				_ = c.NewPane(limit, "down", "/tmp/proj", []string{"bash", "/x/task-control.sh", "t"})
			},
			want: []string{"zellij", "action", "new-pane", "--direction", "down", "--cwd", "/tmp/proj",
				"--", "bash", "/x/task-control.sh", "t"},
		},
		{
			name: "new-pane はコマンド省略時に -- を付けない(k8s レイアウトの素のシェル)",
			call: func(c *TabController) { _ = c.NewPane(limit, "down", "/tmp/proj", nil) },
			want: []string{"zellij", "action", "new-pane", "--direction", "down", "--cwd", "/tmp/proj"},
		},
		{
			name: "move-focus",
			call: func(c *TabController) { _ = c.MoveFocus(limit, "left") },
			want: []string{"zellij", "action", "move-focus", "left"},
		},
		{
			name: "focus-previous-pane",
			call: func(c *TabController) { _ = c.FocusPreviousPane(limit) },
			want: []string{"zellij", "action", "focus-previous-pane"},
		},
		{
			name: "resize は語をそのまま並べる(create_task の decrease up)",
			call: func(c *TabController) { _ = c.Resize(limit, "decrease", "up") },
			want: []string{"zellij", "action", "resize", "decrease", "up"},
		},
		{
			name: "resize は 1 語でも撃てる(apply_layout の direction のみ)",
			call: func(c *TabController) { _ = c.Resize(limit, "up") },
			want: []string{"zellij", "action", "resize", "up"},
		},
		{
			name: "close-tab は今のタブを閉じる",
			call: func(c *TabController) { c.CloseActiveTab() },
			want: []string{"zellij", "action", "close-tab"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &recorder{}
			tc.call(r.controller())
			if len(r.calls) != 1 {
				t.Fatalf("呼び出し回数 = %d, want 1 (%v)", len(r.calls), r.calls)
			}
			if !reflect.DeepEqual(r.calls[0], tc.want) {
				t.Errorf("実行コマンド = %v, want %v", r.calls[0], tc.want)
			}
		})
	}
}

func TestTabControllerQueryTabNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want []string
	}{
		{"1 行 1 タブ名", "Main\nmy-task\n", []string{"Main", "my-task"}},
		{"末尾の改行が無くても読める", "Main\nmy-task", []string{"Main", "my-task"}},
		{"空白を含むタブ名もそのまま", "Main\nmy task\n", []string{"Main", "my task"}},
		// 失敗(zellij の外・上限で打ち切り)は「タブが 1 つも無い」に潰す。
		// ensure_unique_tab_name はこの場合に元の名前をそのまま使う。
		{"空の出力は候補なし", "", nil},
		{"改行だけの出力も候補なし", "\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &recorder{out: tc.out}
			got, err := r.controller().QueryTabNames(time.Second)
			if err != nil {
				t.Fatalf("QueryTabNames() = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("QueryTabNames() = %#v, want %#v", got, tc.want)
			}
			want := []string{"zellij", "action", "query-tab-names"}
			if !reflect.DeepEqual(r.calls[0], want) {
				t.Errorf("実行コマンド = %v, want %v", r.calls[0], want)
			}
		})
	}
}

func TestTabControllerFocusTabVerified(t *testing.T) {
	t.Parallel()

	// zellij 0.44.1 の go-to-tab-name は存在しないタブ名でも rc=0 で戻る
	// 無言の no-op で、成否の差は stdout にしか出ない(ヒット時のみ index)。
	tests := []struct {
		name string
		out  string
		want bool
	}{
		{"index が出れば成功", "1\n", true},
		{"0 番のタブでも成功", "0", true},
		{"stdout が空なら失敗", "", false},
		{"改行だけも失敗(コマンド置換は末尾改行を落とすため)", "\n", false},
		{"空白は非空として扱う(現行の [[ -n ]] と同じ)", " ", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &recorder{out: tc.out}
			if got := r.controller().FocusTabVerified(time.Second, "my-task"); got != tc.want {
				t.Errorf("FocusTabVerified() = %v, want %v", got, tc.want)
			}
			want := []string{"zellij", "action", "go-to-tab-name", "my-task"}
			if !reflect.DeepEqual(r.calls[0], want) {
				t.Errorf("実行コマンド = %v, want %v", r.calls[0], want)
			}
		})
	}
}

func TestTabControllerPassesTheGivenCap(t *testing.T) {
	t.Parallel()

	// 1 回ごとの上限は呼び出し側(予算を持つユースケース)が決める。
	// 現行 task-lib.sh の _zj_budget_cap が渡す値に対応する。
	r := &recorder{}
	c := r.controller()
	_ = c.Resize(2*time.Second, "decrease", "up")
	if r.timeouts[0] != 2*time.Second {
		t.Errorf("渡した上限 = %v, want 2s", r.timeouts[0])
	}
}

func TestTabControllerClampsTheCap(t *testing.T) {
	t.Parallel()

	// 上限そのものは commandTimeout を超えない。予算が余っていても
	// 1 回の zellij 呼び出しに 10 秒以上待つ理由は無い。
	// 0 以下(予算切れ)も同じ扱いにする。呼び出し側は予算切れでは
	// そもそも撃たないが、撃ってしまった場合に無制限にはしない。
	for _, limit := range []time.Duration{time.Hour, 0, -time.Second} {
		r := &recorder{}
		_ = r.controller().FocusPreviousPane(limit)
		if r.timeouts[0] != commandTimeout {
			t.Errorf("cap=%v のとき渡した上限 = %v, want %v", limit, r.timeouts[0], commandTimeout)
		}
	}
}

func TestTabControllerReturnsRunError(t *testing.T) {
	t.Parallel()

	// new-tab の失敗は呼び出し元(create_task)が見る。復元処理がこの
	// 戻り値でタブ作成の成否を判断している。
	want := errors.New("失敗")
	r := &recorder{err: want}
	if err := r.controller().NewTab(time.Second, "t", "/tmp", []string{"claude"}); !errors.Is(err, want) {
		t.Errorf("NewTab() = %v, want %v", err, want)
	}
}

func TestTabControllerCloseActiveTabIgnoresFailure(t *testing.T) {
	t.Parallel()

	// 既に閉じられているタブを指した場合。削除フローとしては進んでよい。
	r := &recorder{err: errors.New("失敗")}
	r.controller().CloseActiveTab()
}

// TestTabControllerQueryTabNamesReportsFailure は問い合わせの失敗を
// 空の一覧と区別して返すことを固定する。
//
// 上限で打ち切られた結果を空と読むと、復元処理が生きているタブを作り直して
// 同じ名前のタブが二重になる。
func TestTabControllerQueryTabNamesReportsFailure(t *testing.T) {
	t.Parallel()

	r := &recorder{outErr: errStub}
	got, err := r.controller().QueryTabNames(time.Second)
	if err == nil {
		t.Fatal("QueryTabNames() = nil, want エラー")
	}
	if got != nil {
		t.Errorf("失敗時の一覧 = %#v, want nil", got)
	}
}
