package domain_test

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// セッション名切り詰めのゴールデンテスト。
//
// testdata/session-names.tsv は現行 init.zsh の `_conductor_session_name` へ
// 同じ入力を流して得た出力である(作り方は scripts/gen-golden-session-name.sh)。
// **1 件でも違えば、利用者が今 attach しているセッションへ戻れなくなる。**
const sessionNameGolden = "testdata/session-names.tsv"

func TestZellijSessionNameMatchesShellVersion(t *testing.T) {
	t.Parallel()

	b, err := os.ReadFile(sessionNameGolden)
	if err != nil {
		t.Fatalf("golden 表が読めない(scripts/gen-golden-session-name.sh で生成する): %v", err)
	}

	rows := 0
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 3 {
			t.Fatalf("列数が違う: %q", line)
		}
		name, hashSrc, want := cols[0], cols[1], cols[2]
		rows++

		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := domain.ZellijSessionName(name, hashSrc); got != want {
				t.Errorf("ZellijSessionName(%q, %q) = %q, want %q", name, hashSrc, got, want)
			}
		})
	}
	if rows == 0 {
		t.Fatal("golden 表が空")
	}
}

// TestZellijSessionNameLength は結果が zellij の上限を超えないことを確かめる。
//
// golden 表は現行版と同じかどうかしか見ない。長さの不変条件は別に固定する
// (現行版が上限を超える入力を持っていなかった、という取りこぼしを防ぐ)。
func TestZellijSessionNameLength(t *testing.T) {
	t.Parallel()

	tests := []string{
		strings.Repeat("a", 25),
		strings.Repeat("a", 200),
		strings.Repeat("あ", 100),
		strings.Repeat("-", 30),
		"a-" + strings.Repeat("b", 30),
	}
	for _, name := range tests {
		t.Run(name[:min(len(name), 12)], func(t *testing.T) {
			t.Parallel()
			got := domain.ZellijSessionName(name, "")
			if n := utf8.RuneCountInString(got); n > domain.SessionNameLimit {
				t.Errorf("ZellijSessionName = %q (%d 文字), 上限は %d",
					got, n, domain.SessionNameLimit)
			}
		})
	}
}

// TestZellijSessionNameIsDeterministic は同じ入力が同じ名前になることを
// 確かめる。attach-or-create はこの性質だけで成り立っている。
func TestZellijSessionNameIsDeterministic(t *testing.T) {
	t.Parallel()

	const name = "this-is-a-very-long-session-name"
	first := domain.ZellijSessionName(name, "/w/repo")
	for range 5 {
		if got := domain.ZellijSessionName(name, "/w/repo"); got != first {
			t.Fatalf("結果が揺れた: %q と %q", got, first)
		}
	}
}

// TestZellijSessionNameSeparatesByHashSource は名前が同じでもハッシュ源が
// 違えば別のセッションになることを確かめる。
//
// 先頭 19 文字を共有する worktree が同じセッションへ潰れると、片方の作業が
// もう片方の画面に出る。
func TestZellijSessionNameSeparatesByHashSource(t *testing.T) {
	t.Parallel()

	const name = "test-add-embedded-assets-and-cli-ports"
	a := domain.ZellijSessionName(name, "/w/alpha")
	b := domain.ZellijSessionName(name, "/w/beta")
	if a == b {
		t.Errorf("ハッシュ源が違うのに同じ名前になった: %q", a)
	}
}

// TestZellijSessionNameEmptyHashSource は第 2 引数が空のとき名前自身を
// ハッシュ源に使うことを確かめる(現行版の `${2:-$1}`)。
func TestZellijSessionNameEmptyHashSource(t *testing.T) {
	t.Parallel()

	const name = "this-is-a-very-long-session-name"
	if got, want := domain.ZellijSessionName(name, ""), domain.ZellijSessionName(name, name); got != want {
		t.Errorf("ZellijSessionName(name, \"\") = %q, want %q", got, want)
	}
}
