package git

import (
	"errors"
	"strings"
	"testing"
)

// TestPushFailsWhenGitMissing は git が入っていない環境で error になることを
// 確かめる。現行版の `command -v git >/dev/null || return 1` に対応する。
func TestPushFailsWhenGitMissing(t *testing.T) {
	repo := NewLogRepository(t.TempDir())
	repo.lookGit = func() error { return errors.New("not found") }
	if _, err := repo.Push("owner/name", "main", "a.md", "a"); err == nil {
		t.Error("git が無いのに error になりませんでした")
	}
}

// TestPushCommandSequence は git へ渡す引数の並びを固定する。
//
// 実際のリポジトリを使うテスト(logrepo_test.go)では確かめられない
// 「同じ内容なら commit を飛ばす」「fetch が成功したら FETCH_HEAD を土台にする」
// といった分岐を、呼び出しの記録で見る。
func TestPushCommandSequence(t *testing.T) {
	tests := []struct {
		name string
		// fetchFails は fetch を失敗させるかどうか。
		fetchFails bool
		// noDiff は diff --cached --quiet を成功(差分なし)にするかどうか。
		noDiff bool
		// wantContains は呼び出し記録に含まれていてほしい行。
		wantContains []string
		// wantMissing は含まれていてはいけない行。
		wantMissing []string
	}{
		{
			name:         "fetch 成功なら FETCH_HEAD を土台にする",
			wantContains: []string{"checkout --quiet -B main FETCH_HEAD", "commit --quiet"},
		},
		{
			name:         "fetch 失敗なら今の HEAD からブランチを作る",
			fetchFails:   true,
			wantContains: []string{"checkout --quiet -B main"},
			wantMissing:  []string{"checkout --quiet -B main FETCH_HEAD"},
		},
		{
			name:         "差分が無ければ commit を飛ばして push する",
			noDiff:       true,
			wantContains: []string{"push --quiet -u origin main"},
			wantMissing:  []string{"commit --quiet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			repo := NewLogRepository(t.TempDir())
			repo.lookGit = func() error { return nil }
			repo.run = func(_ string, args ...string) (string, error) {
				line := strings.Join(args, " ")
				calls = append(calls, line)
				switch {
				case args[0] == "fetch" && tt.fetchFails:
					return "", errors.New("fetch failed")
				case args[0] == "diff" && !tt.noDiff:
					// 差分があるときは非 0 で戻る。
					return "", errors.New("differences")
				}
				return "sha123\n", nil
			}

			// git はすべて差し替えてあるので、clone も含めて記録されるだけになる。
			if _, err := repo.Push("owner/name", "main", "work-log/a.md", "body"); err != nil {
				t.Fatalf("Push が失敗しました: %v", err)
			}

			joined := strings.Join(calls, "\n")
			for _, want := range tt.wantContains {
				if !containsLine(calls, want) {
					t.Errorf("呼び出し記録に %q がありません:\n%s", want, joined)
				}
			}
			for _, missing := range tt.wantMissing {
				if containsLine(calls, missing) {
					t.Errorf("呼び出し記録に %q が含まれています:\n%s", missing, joined)
				}
			}
		})
	}
}

// containsLine は記録の中に want を含む行があるかを返す。
// commit は -c user.email=... が前に付くため、前方一致では拾えない。
func containsLine(calls []string, want string) bool {
	for _, call := range calls {
		if strings.Contains(call, want) {
			return true
		}
	}
	return false
}

// TestPushReturnsReferenceWithSha は rev-parse の出力から参照文字列を
// 組み立てることを確かめる。
func TestPushReturnsReferenceWithSha(t *testing.T) {
	repo := NewLogRepository(t.TempDir())
	repo.lookGit = func() error { return nil }
	repo.run = func(_ string, args ...string) (string, error) {
		if args[0] == "diff" {
			return "", errors.New("differences")
		}
		return "deadbeef\n", nil
	}
	got, err := repo.Push("/tmp/local.git", "main", "work-log/a.md", "body")
	if err != nil {
		t.Fatalf("Push が失敗しました: %v", err)
	}
	if want := "work-log/a.md @ deadbeef"; got != want {
		t.Errorf("参照文字列 = %q, want %q", got, want)
	}
}

// TestPushIgnoresRevParseFailure は push が済んだ後の rev-parse の失敗を
// 握り潰して成功として返すことを確かめる。
//
// ここで error を返すと、ログは既にリポジトリへ残っているのに呼び出し側が
// アップロードを失敗と見なし、タブの削除を中止してしまう(次の dd でまた
// 同じログを push しにいく)。現行版も
// `sha=$(git rev-parse HEAD 2>/dev/null)` の失敗を無視し、空の sha を
// 埋めた参照文字列を出して正常終了する。
func TestPushIgnoresRevParseFailure(t *testing.T) {
	tests := []struct {
		name string
		repo string
		want string
	}{
		{
			// ローカルパスは sha を使う形なので、空のまま埋まる。
			// 現行版の `printf '%s @ %s\n'` と同じく末尾に空白が残る。
			name: "ローカルパスは空の sha が埋まる",
			repo: "/tmp/local.git",
			want: "work-log/a.md @ ",
		},
		{
			// owner/name は sha を使わないので、そもそも影響を受けない。
			name: "owner/name は sha を使わないので影響しない",
			repo: "owner/name",
			want: "https://github.com/owner/name/blob/main/work-log/a.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := NewLogRepository(t.TempDir())
			repo.lookGit = func() error { return nil }
			repo.run = func(_ string, args ...string) (string, error) {
				switch args[0] {
				case "diff":
					return "", errors.New("differences")
				case "rev-parse":
					return "", errors.New("HEAD を読めない")
				}
				return "", nil
			}

			got, err := repo.Push(tt.repo, "main", "work-log/a.md", "body")
			if err != nil {
				t.Fatalf("rev-parse の失敗で error になりました: %v", err)
			}
			if got != tt.want {
				t.Errorf("参照文字列 = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPushStillFailsBeforeReference は push までの失敗は error のままである
// ことを確かめる。rev-parse だけを握り潰したつもりが、その手前の失敗まで
// 通してしまうと作業ログを失ったままタブが消える。
func TestPushStillFailsBeforeReference(t *testing.T) {
	for _, failing := range []string{"clone", "fetch", "checkout", "add", "commit", "push"} {
		t.Run(failing, func(t *testing.T) {
			repo := NewLogRepository(t.TempDir())
			repo.lookGit = func() error { return nil }
			repo.run = func(_ string, args ...string) (string, error) {
				// commit は -c ... の後ろに来るため、並び全体で見る。
				line := strings.Join(args, " ")
				switch {
				case args[0] == failing || strings.Contains(line, " "+failing+" "):
					return "", errors.New("失敗")
				case args[0] == "diff":
					return "", errors.New("differences")
				}
				return "", nil
			}

			// fetch の失敗はブランチを作り直す正常な分岐なので、error にならない。
			_, err := repo.Push("owner/name", "main", "work-log/a.md", "body")
			if failing == "fetch" {
				if err != nil {
					t.Errorf("fetch の失敗は分岐であって error ではない: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("%s が失敗したのに error になりませんでした", failing)
			}
		})
	}
}
