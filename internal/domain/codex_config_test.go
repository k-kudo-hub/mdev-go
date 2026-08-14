package domain_test

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// mdevBin はテストで使う mdev のパス。
const mdevBin = "/Users/dev/.claude-conductor/bin/mdev"

// TestRewriteCodexNotifyAdds は notify が無い設定への追記を確かめる。
//
// 行は必ず先頭へ置く。TOML では `[table]` より後ろのキーはその table の
// ものになり、codex は読まない(現行 install.sh の注記と同じ)。
func TestRewriteCodexNotifyAdds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "空の設定",
			content: "",
			want:    `notify = ["` + mdevBin + `", "codex", "notify"] # claude-conductor` + "\n",
		},
		{
			name:    "table のある設定は先頭へ置く",
			content: "[projects.\"/w/repo\"]\ntrust_level = \"trusted\"\n",
			want: `notify = ["` + mdevBin + `", "codex", "notify"] # claude-conductor` + "\n\n" +
				"[projects.\"/w/repo\"]\ntrust_level = \"trusted\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, status := domain.RewriteCodexNotify(tt.content, mdevBin)
			if status != domain.CodexNotifyAdded {
				t.Errorf("status = %v, want Added", status)
			}
			if got != tt.want {
				t.Errorf("結果 =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// TestRewriteCodexNotifyMigrates は Shell 版からの差し替えを確かめる。
func TestRewriteCodexNotifyMigrates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "現行 install.sh が書く形",
			content: `notify = ["bash", "/Users/dev/.claude-conductor/scripts/codex-notify.sh"] # claude-conductor` + "\n",
			want:    `notify = ["` + mdevBin + `", "codex", "notify"] # claude-conductor` + "\n",
		},
		{
			name:    "bash を挟まない形",
			content: `notify = ["/Users/dev/.claude-conductor/scripts/codex-notify.sh"]` + "\n",
			want:    `notify = ["` + mdevBin + `", "codex", "notify"]` + "\n",
		},
		{
			name:    "他の設定は触らない",
			content: "notify = [\"bash\", \"/c/scripts/codex-notify.sh\"]\n\n[projects.\"/w\"]\ntrust_level = \"trusted\"\n",
			want:    "notify = [\"" + mdevBin + "\", \"codex\", \"notify\"]\n\n[projects.\"/w\"]\ntrust_level = \"trusted\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, status := domain.RewriteCodexNotify(tt.content, mdevBin)
			if status != domain.CodexNotifyMigrated {
				t.Errorf("status = %v, want Migrated", status)
			}
			if got != tt.want {
				t.Errorf("結果 =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// nestedNotify は Codex Computer Use が conductor を包んだ実物の形である。
//
// 別ツールが `--previous-notify` の後ろへ「元の notify」を JSON の文字列と
// して畳んで置く。外側だけを見ていると conductor を見つけられず、入れ子の
// 中で Shell 版が呼ばれ続ける(6-2 の申し送り)。スラッシュが `\/` で
// 逃がしてあるのも実物どおり。
const nestedNotify = `notify = [
    "/Users/dev/.codex/computer-use/Codex Computer Use.app/Contents/MacOS/SkyComputerUseClient",
    "turn-ended",
    "--previous-notify",
    '["bash","\/Users\/dev\/.claude-conductor\/scripts\/codex-notify.sh"]',
]

[projects."/w/repo"]
trust_level = "trusted"
`

// TestRewriteCodexNotifyUnwrapsNested は入れ子の中の差し替えを確かめる。
func TestRewriteCodexNotifyUnwrapsNested(t *testing.T) {
	t.Parallel()

	got, status := domain.RewriteCodexNotify(nestedNotify, mdevBin)
	if status != domain.CodexNotifyMigrated {
		t.Fatalf("status = %v, want Migrated", status)
	}

	// 入れ子の中だけが変わる。
	want := `'["\/Users\/dev\/.claude-conductor\/bin\/mdev","codex","notify"]'`
	if !strings.Contains(got, want) {
		t.Errorf("入れ子が差し替わっていない:\n%s", got)
	}
	if strings.Contains(got, domain.CodexNotifyMarker) {
		t.Errorf("Shell 版の呼び出しが残っている:\n%s", got)
	}
	// 包んでいる側の要素は 1 つも変わらない。
	for _, keep := range []string{"SkyComputerUseClient", "turn-ended", "--previous-notify"} {
		if !strings.Contains(got, keep) {
			t.Errorf("%q が消えた:\n%s", keep, got)
		}
	}
	if !strings.Contains(got, `[projects."/w/repo"]`) {
		t.Errorf("他の設定が消えた:\n%s", got)
	}
}

// TestRewriteCodexNotifyIsIdempotent は 2 回目が何も変えないことを確かめる。
// install は繰り返し実行される。
func TestRewriteCodexNotifyIsIdempotent(t *testing.T) {
	t.Parallel()

	for _, start := range []string{"", nestedNotify,
		`notify = ["bash", "/c/scripts/codex-notify.sh"]` + "\n"} {
		once, _ := domain.RewriteCodexNotify(start, mdevBin)
		twice, status := domain.RewriteCodexNotify(once, mdevBin)
		if status != domain.CodexNotifyUnchanged {
			t.Errorf("2 回目の status = %v, want Unchanged\n%s", status, once)
		}
		if twice != once {
			t.Errorf("2 回目が書き換えた\n--- 2 回目 ---\n%s\n--- 1 回目 ---\n%s", twice, once)
		}
	}
}

// TestRewriteCodexNotifyLeavesForeign は他ツールの notify を触らないことを
// 確かめる。現行 install.sh も案内だけ出して手を入れない。
func TestRewriteCodexNotifyLeavesForeign(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{name: "別のプログラム", content: "notify = [\"/opt/other/notify\"]\n"},
		{
			name:    "入れ子だが conductor はいない",
			content: "notify = [\"/opt/wrapper\", \"--previous-notify\", '[\"/opt/other\"]']\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, status := domain.RewriteCodexNotify(tt.content, mdevBin)
			if status != domain.CodexNotifyForeign {
				t.Errorf("status = %v, want Foreign", status)
			}
			if got != tt.content {
				t.Errorf("書き換えた:\n%s", got)
			}
		})
	}
}

// TestRemoveCodexNotify は uninstall での取り除きを確かめる。
func TestRemoveCodexNotify(t *testing.T) {
	t.Parallel()

	installed, _ := domain.RewriteCodexNotify("[projects.\"/w\"]\ntrust_level = \"trusted\"\n", mdevBin)
	got, removed := domain.RemoveCodexNotify(installed, mdevBin)
	if !removed {
		t.Fatal("取り除かれていない")
	}
	if want := "[projects.\"/w\"]\ntrust_level = \"trusted\"\n"; got != want {
		t.Errorf("結果 =\n%q\nwant\n%q", got, want)
	}
}

// TestRemoveCodexNotifyLeavesForeign は他ツールの notify を残すことを確かめる。
func TestRemoveCodexNotifyLeavesForeign(t *testing.T) {
	t.Parallel()

	const content = "notify = [\"/opt/other/notify\"]\n"
	got, removed := domain.RemoveCodexNotify(content, mdevBin)
	if removed || got != content {
		t.Errorf("触ってはいけない: removed=%v\n%s", removed, got)
	}
}

// TestRewriteCodexNotifyIgnoresTableScopedNotify は table 配下の notify を
// 見ないことを確かめる。
//
// TOML では `[table]` より後ろに書いたキーはその table のものになり、codex は
// それを読まない。ここを見てしまうと 2 通りに壊れる。別ツールが自分の table で
// 使っている notify を conductor のものと取り違えて書き換えるか、table 配下に
// しか notify が無い設定を「他ツールが使っている」と誤判定して、トップレベルへ
// 足すべきときに何もしないかである。
func TestRewriteCodexNotifyIgnoresTableScopedNotify(t *testing.T) {
	t.Parallel()

	t.Run("table 配下の conductor は書き換えない", func(t *testing.T) {
		t.Parallel()
		const content = "[some.tool]\n" +
			`notify = ["bash", "/c/scripts/codex-notify.sh"]` + "\n"

		got, status := domain.RewriteCodexNotify(content, mdevBin)

		// トップレベルには notify が無いので、先頭へ足すのが正しい。
		if status != domain.CodexNotifyAdded {
			t.Errorf("status = %v, want Added", status)
		}
		if !strings.HasPrefix(got, "notify = [\""+mdevBin+"\"") {
			t.Errorf("先頭へ足していない:\n%s", got)
		}
		// table 配下は 1 バイトも変えない。
		if !strings.Contains(got, `notify = ["bash", "/c/scripts/codex-notify.sh"]`) {
			t.Errorf("table 配下を書き換えた:\n%s", got)
		}
	})

	t.Run("table 配下の他ツールを Foreign と誤判定しない", func(t *testing.T) {
		t.Parallel()
		const content = "[some.tool]\nnotify = [\"/opt/other/notify\"]\n"

		got, status := domain.RewriteCodexNotify(content, mdevBin)

		if status != domain.CodexNotifyAdded {
			t.Errorf("status = %v, want Added", status)
		}
		if !strings.Contains(got, `notify = ["`+mdevBin+`", "codex", "notify"]`) {
			t.Errorf("トップレベルへ足していない:\n%s", got)
		}
		if !strings.Contains(got, `notify = ["/opt/other/notify"]`) {
			t.Errorf("table 配下を書き換えた:\n%s", got)
		}
	})

	t.Run("トップレベルにあれば table 配下があっても書き換える", func(t *testing.T) {
		t.Parallel()
		const content = `notify = ["bash", "/c/scripts/codex-notify.sh"]` + "\n\n" +
			"[some.tool]\nnotify = [\"/opt/other/notify\"]\n"

		got, status := domain.RewriteCodexNotify(content, mdevBin)

		if status != domain.CodexNotifyMigrated {
			t.Errorf("status = %v, want Migrated", status)
		}
		if strings.Contains(got, domain.CodexNotifyMarker) {
			t.Errorf("トップレベルが書き換わっていない:\n%s", got)
		}
		if !strings.Contains(got, `notify = ["/opt/other/notify"]`) {
			t.Errorf("table 配下を書き換えた:\n%s", got)
		}
	})
}

// TestRemoveCodexNotifyIgnoresTableScoped は取り除きでも table 配下を
// 見ないことを確かめる。別ツールの設定を壊さない。
func TestRemoveCodexNotifyIgnoresTableScoped(t *testing.T) {
	t.Parallel()

	content := "[some.tool]\nnotify = [\"" + mdevBin + "\", \"codex\", \"notify\"]\n"
	got, removed := domain.RemoveCodexNotify(content, mdevBin)
	if removed || got != content {
		t.Errorf("table 配下を消した: removed=%v\n%s", removed, got)
	}
}
