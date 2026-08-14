package domain_test

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// shellMultiKdl は現行 Shell 版の multi.kdl のペイン部分である。
const shellMultiKdl = `layout {
    tab name="Main" focus=true {
        pane {
            name "Dashboard"
            command "bash"
            args "-c" "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/dashboard-loop.sh"
        }
        pane {
            name "Waiting"
            command "bash"
            args "-c" "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/waiting-loop.sh"
        }
    }
}
`

// shellDevKdl は現行 Shell 版の dev.kdl の Agent ペインである。
const shellDevKdl = `layout {
    pane {
        name "Agent"
        command "bash"
        args "-c" "bash \"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/agent-launch.sh\""
    }
}
`

// TestMigrateLayoutRewritesPanes はペイン呼び出しの書き換えを確かめる。
//
// パスを引用符で囲むのが要点である。囲まないと HOME に空白がある環境で
// bash が語分割し、コマンドが見つからない。
func TestMigrateLayoutRewritesPanes(t *testing.T) {
	t.Parallel()

	got, changes := domain.MigrateLayout(shellMultiKdl)

	for _, want := range []string{
		`args "-c" "\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\" pane dashboard"`,
		`args "-c" "\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\" pane waiting"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が無い:\n%s", want, got)
		}
	}
	if len(changes) != 2 {
		t.Errorf("書き換え = %d 件, want 2: %+v", len(changes), changes)
	}
	if strings.Contains(got, "/scripts/") {
		t.Errorf("Shell 呼び出しが残っている:\n%s", got)
	}
	// ペイン名や構造は 1 バイトも動かさない。
	if !strings.Contains(got, `name "Dashboard"`) || !strings.Contains(got, `command "bash"`) {
		t.Errorf("周りが壊れた:\n%s", got)
	}
}

// TestMigrateLayoutRewritesAgentLaunch は Agent ペインの書き換えを確かめる。
func TestMigrateLayoutRewritesAgentLaunch(t *testing.T) {
	t.Parallel()

	got, changes := domain.MigrateLayout(shellDevKdl)

	want := `args "-c" "\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\" agent launch"`
	if !strings.Contains(got, want) {
		t.Errorf("%q が無い:\n%s", want, got)
	}
	if len(changes) != 1 {
		t.Errorf("書き換え = %d 件, want 1: %+v", len(changes), changes)
	}
}

// TestMigrateLayoutIsIdempotent は 2 回目が何も変えないことを確かめる。
// install は繰り返し実行される。
func TestMigrateLayoutIsIdempotent(t *testing.T) {
	t.Parallel()

	for _, start := range []string{shellMultiKdl, shellDevKdl} {
		once, _ := domain.MigrateLayout(start)
		twice, changes := domain.MigrateLayout(once)
		if len(changes) != 0 {
			t.Errorf("2 回目が書き換えた: %+v", changes)
		}
		if twice != once {
			t.Errorf("2 回目が変えた\n--- 2 回目 ---\n%s\n--- 1 回目 ---\n%s", twice, once)
		}
	}
}

// TestMigrateLayoutLeavesUnknownScripts は規則に無いスクリプトを残すことを
// 確かめる。
//
// 総称パターンで巻き込むと、mdev 側に実装の無いペインまで将来自動で
// 書き換わってしまう。残せば警告として表に出る。
func TestMigrateLayoutLeavesUnknownScripts(t *testing.T) {
	t.Parallel()

	const content = `args "-c" "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/my-own-loop.sh"` + "\n"
	got, changes := domain.MigrateLayout(content)

	if got != content || len(changes) != 0 {
		t.Errorf("触ってはいけない:\n%s", got)
	}
	remaining := domain.RemainingLayoutScripts(got)
	if len(remaining) != 1 || !strings.Contains(remaining[0], "my-own-loop.sh") {
		t.Errorf("残存の報告 = %v", remaining)
	}
}

// TestRemainingLayoutScriptsEmpty は書き換え後に残存が無いことを確かめる。
func TestRemainingLayoutScriptsEmpty(t *testing.T) {
	t.Parallel()

	got, _ := domain.MigrateLayout(shellMultiKdl)
	if remaining := domain.RemainingLayoutScripts(got); len(remaining) != 0 {
		t.Errorf("残存 = %v", remaining)
	}
}

// TestMigrateLayoutQuotesBareMdev は囲まれていない mdev の呼び出しを囲むことを
// 確かめる。
//
// 6-2 より前の install が書いたレイアウトがこの形で、HOME に空白があると
// bash が語分割してペインが起動しない。
func TestMigrateLayoutQuotesBareMdev(t *testing.T) {
	t.Parallel()

	const content = `args "-c" "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev pane dashboard"` + "\n" +
		`args "-c" "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev agent launch"` + "\n"

	got, changes := domain.MigrateLayout(content)

	for _, want := range []string{
		`"\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\" pane dashboard"`,
		`"\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\" agent launch"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が無い:\n%s", want, got)
		}
	}
	if len(changes) != 2 {
		t.Errorf("書き換え = %d 件, want 2: %+v", len(changes), changes)
	}

	// 既に囲まれているものは触らない(冪等)。
	twice, again := domain.MigrateLayout(got)
	if len(again) != 0 || twice != got {
		t.Errorf("2 回目が書き換えた: %+v", again)
	}
}
