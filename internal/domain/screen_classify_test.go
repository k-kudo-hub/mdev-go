package domain_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// codexPatterns は config.default.json の codex のパターンである。
// 分類のテーブルテストはこれを既定の入力にする。
func codexPatterns() domain.ScreenPatterns {
	return domain.ScreenPatterns{
		Blocked: []string{
			`^ *Would you like to run the following command\? *$`,
			`^ *Would you like to make the following edits\? *$`,
			`^ *Would you like to grant these permissions\? *$`,
			`^ *Press enter to confirm or esc to cancel *$`,
		},
		Working: []string{`\([^)]*• [^)]* to interrupt\)`},
	}
}

// codexPatternsWithNeutral は test.sh が neutral の検証で流し込む設定である
// (`jq '.agents.codex.patterns.neutral = [...]'`)。
func codexPatternsWithNeutral() domain.ScreenPatterns {
	patterns := codexPatterns()
	patterns.Neutral = []string{`esc to close *$`, `^ *↑↓ scroll`}
	return patterns
}

// screenFixture は test.sh から持ち込んだ実機の dump-screen 抜粋を読む。
//
// test.sh は `$(cat ...)` で読むためコマンド置換が末尾の改行を落とす。
// ここでも同じ形にしてから分類へ渡す。
func screenFixture(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("testdata", "screen", name+".txt"))
	if err != nil {
		t.Fatalf("fixture %s が読めない: %v", name, err)
	}
	text := string(b)
	for len(text) > 0 && text[len(text)-1] == '\n' {
		text = text[:len(text)-1]
	}
	return text
}

// TestClassifyScreen は現行 screen-detect-lib.sh の screen_classify と同じ
// 分類になることを固定する。
//
// 期待値は claude-conductor の test.sh 17b5(:1156-1212)をそのまま移したもので、
// 実際に Shell 版へ同じ fixture を与えて突き合わせてある(evidence §1)。
func TestClassifyScreen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		patterns domain.ScreenPatterns
		fixture  string
		text     string
		want     domain.ScreenObservation
	}{
		{
			name:     "既知のコマンド承認は blocked(一致行がメッセージになる)",
			patterns: codexPatterns(),
			fixture:  "blocked-command",
			want: domain.ScreenObservation{
				State:   domain.ScreenBlocked,
				Message: "Would you like to run the following command?",
			},
		},
		{
			name:     "編集承認も blocked",
			patterns: codexPatterns(),
			fixture:  "blocked-edit",
			want: domain.ScreenObservation{
				State:   domain.ScreenBlocked,
				Message: "Would you like to make the following edits?",
			},
		},
		{
			name:     "スピナー行があれば working",
			patterns: codexPatterns(),
			fixture:  "working",
			want:     domain.ScreenObservation{State: domain.ScreenWorking},
		},
		{
			name:     "マーカーが無ければ idle",
			patterns: codexPatterns(),
			fixture:  "idle",
			want:     domain.ScreenObservation{State: domain.ScreenIdle},
		},
		{
			name:     "未知のダイアログは blocked にせず idle へ倒す",
			patterns: codexPatterns(),
			fixture:  "unknown-prompt",
			want:     domain.ScreenObservation{State: domain.ScreenIdle},
		},
		{
			name:     "質問行が末尾窓から押し出されてもフッターで blocked",
			patterns: codexPatterns(),
			fixture:  "long-dialog",
			want: domain.ScreenObservation{
				State:   domain.ScreenBlocked,
				Message: "Press enter to confirm or esc to cancel",
			},
		},
		{
			name:     "ログ中の引用は行頭アンカーで拾わない",
			patterns: codexPatterns(),
			fixture:  "transcript-echo",
			want:     domain.ScreenObservation{State: domain.ScreenIdle},
		},
		{
			name:     "パターンを持たないエージェントは常に idle",
			patterns: domain.ScreenPatterns{},
			fixture:  "blocked-command",
			want:     domain.ScreenObservation{State: domain.ScreenIdle},
		},
		{
			name:     "ビューア画面は neutral",
			patterns: codexPatternsWithNeutral(),
			fixture:  "neutral-viewer",
			want:     domain.ScreenObservation{State: domain.ScreenNeutral},
		},
		{
			name:     "neutral 未定義なら同じ画面が idle のまま",
			patterns: codexPatterns(),
			fixture:  "neutral-viewer",
			want:     domain.ScreenObservation{State: domain.ScreenIdle},
		},
		{
			name:     "承認画面でも neutral が勝つ",
			patterns: codexPatternsWithNeutral(),
			text:     "  Would you like to run the following command?\n  esc to close",
			want:     domain.ScreenObservation{State: domain.ScreenNeutral},
		},
		{
			name:     "空の画面は idle",
			patterns: codexPatterns(),
			text:     "",
			want:     domain.ScreenObservation{State: domain.ScreenIdle},
		},
		{
			name:     "不正な正規表現は不一致として飛ばす",
			patterns: domain.ScreenPatterns{Blocked: []string{`[`, `^ *Press enter`}},
			fixture:  "long-dialog",
			want: domain.ScreenObservation{
				State:   domain.ScreenBlocked,
				Message: "Press enter to confirm or esc to cancel",
			},
		},
		{
			name:     "空文字のパターンは飛ばす(全行一致にしない)",
			patterns: domain.ScreenPatterns{Neutral: []string{""}},
			fixture:  "idle",
			want:     domain.ScreenObservation{State: domain.ScreenIdle},
		},
		{
			name: "空行にしか一致しないパターンは blocked にならない",
			// grep の一致行が空文字だと `[[ -n "$line" ]]` が偽になり、
			// 次のパターンへ進む(evidence §1-3)。窓は空白のみの行を
			// 落としているため、この状況になるのは窓自体が空のときだけ。
			patterns: domain.ScreenPatterns{Blocked: []string{`^$`}},
			text:     "   \n\t\n",
			want:     domain.ScreenObservation{State: domain.ScreenIdle},
		},
		{
			name: "空の窓でも neutral は空文字に一致しうる",
			// `printf '%s\n' "$tail_buf"` が空行 1 行を grep へ渡すため
			// (evidence §1-4)。
			patterns: domain.ScreenPatterns{Neutral: []string{`^$`}},
			text:     "   \n\t\n",
			want:     domain.ScreenObservation{State: domain.ScreenNeutral},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			text := tt.text
			if tt.fixture != "" {
				text = screenFixture(t, tt.fixture)
			}
			if got := domain.ClassifyScreen(tt.patterns, text); got != tt.want {
				t.Errorf("ClassifyScreen() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestScreenTailWindow は分類に使う窓の作り方(空行除去 → 末尾 N 行)を固定する。
func TestScreenTailWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		n    int
		want []string
	}{
		{
			name: "空白のみの行を落とす",
			text: "a\n\n  \n\t\nb",
			n:    20,
			want: []string{"a", "b"},
		},
		{
			name: "空行を落としてから末尾 N 行を取る",
			text: "1\n\n2\n\n3\n\n4",
			n:    2,
			want: []string{"3", "4"},
		},
		{
			name: "先頭と末尾の空白は保つ(blocked の整形は分類側で行う)",
			text: "  padded  ",
			n:    20,
			want: []string{"  padded  "},
		},
		{
			name: "すべて空白なら空になる",
			text: "\n  \n\t\n",
			n:    20,
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := domain.ScreenTailWindow(tt.text, tt.n); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ScreenTailWindow() = %q, want %q", got, tt.want)
			}
		})
	}
}
