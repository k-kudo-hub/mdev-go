package shell

import (
	"errors"
	"reflect"
	"testing"
)

// TestExecerReplacesProcess は解決したパスと argv の組み立てを確かめる。
//
// argv[0] を絶対パスにしてしまうと ps の表示が変わり、エージェント自身が
// argv[0] を見て振る舞いを変える場合にも影響する。
func TestExecerReplacesProcess(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotArgv []string
	execer := &Execer{
		lookPath: func(name string) (string, error) { return "/usr/local/bin/" + name, nil },
		exec: func(path string, argv, env []string) error {
			gotPath, gotArgv = path, argv
			if len(env) == 0 {
				t.Error("環境変数を引き継いでいない")
			}
			return nil
		},
	}

	if err := execer.Exec([]string{"codex", "--model", "gpt-5"}); err != nil {
		t.Fatalf("Exec = %v", err)
	}
	if want := "/usr/local/bin/codex"; gotPath != want {
		t.Errorf("パス = %q, want %q", gotPath, want)
	}
	if want := []string{"codex", "--model", "gpt-5"}; !reflect.DeepEqual(gotArgv, want) {
		t.Errorf("argv = %q, want %q", gotArgv, want)
	}
}

// TestExecerReportsMissingCommand は見つからないコマンドの報告を確かめる。
// 設定に書いたコマンド名が出ないと、どこが悪いのか分からない。
func TestExecerReportsMissingCommand(t *testing.T) {
	t.Parallel()

	execer := &Execer{
		lookPath: func(string) (string, error) { return "", errors.New("not found") },
		exec: func(string, []string, []string) error {
			t.Error("見つからないのに置き換えようとした")
			return nil
		},
	}

	err := execer.Exec([]string{"nosuchagent"})
	if err == nil {
		t.Fatal("失敗を返すはず")
	}
	if want := "nosuchagent が見つかりません"; !contains(err.Error(), want) {
		t.Errorf("説明 = %q, want %q を含む", err, want)
	}
}

// TestExecerRejectsEmptyCommand は空のコマンドを弾くことを確かめる。
// execve に空の argv を渡すと何が起きるかは実装依存なので、手前で止める。
func TestExecerRejectsEmptyCommand(t *testing.T) {
	t.Parallel()

	execer := &Execer{
		lookPath: func(string) (string, error) { t.Error("引きに行ってはいけない"); return "", nil },
		exec:     func(string, []string, []string) error { t.Error("置き換えてはいけない"); return nil },
	}
	if err := execer.Exec(nil); err == nil {
		t.Error("失敗を返すはず")
	}
}

// contains は s に substr が含まれるかを返す。
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
