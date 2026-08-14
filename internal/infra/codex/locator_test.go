package codex

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeFile はテスト用のファイルを親ごと作る。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("ディレクトリを作れない: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("ファイルを作れない: %v", err)
	}
}

// TestLocateFromStateDB は状態 DB から引く経路を確かめる。
func TestLocateFromStateDB(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	rollout := filepath.Join(home, "sessions", "2026", "08", "rollout-th-1.jsonl")
	writeFile(t, rollout, "{}\n")
	writeFile(t, filepath.Join(home, "state_5.sqlite"), "")

	var askedDB, askedQuery string
	locator := NewLocator(home, "")
	locator.runSQLite = func(db, query string) (string, error) {
		askedDB, askedQuery = db, query
		// sqlite3 は結果の後ろに改行を付ける。
		return rollout + "\n", nil
	}

	if got := locator.Locate("th-1"); got != rollout {
		t.Errorf("Locate = %q, want %q", got, rollout)
	}
	if askedDB != filepath.Join(home, "state_5.sqlite") {
		t.Errorf("引いた DB = %q", askedDB)
	}
	if want := "SELECT rollout_path FROM threads WHERE id='th-1' LIMIT 1"; askedQuery != want {
		t.Errorf("問い合わせ = %q, want %q", askedQuery, want)
	}
}

// TestLocateUsesLatestStateDB は版番号が最も大きい DB を引くことを確かめる。
//
// 辞書順で選ぶと state_10 より state_5 が後に来て、古い DB を引いてしまう。
func TestLocateUsesLatestStateDB(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	for _, name := range []string{"state_2.sqlite", "state_5.sqlite", "state_10.sqlite"} {
		writeFile(t, filepath.Join(home, name), "")
	}
	// 形の違うものは候補にしない。
	writeFile(t, filepath.Join(home, "state_x.sqlite"), "")
	writeFile(t, filepath.Join(home, "goals_1.sqlite"), "")

	var askedDB string
	locator := NewLocator(home, "")
	locator.runSQLite = func(db, _ string) (string, error) { askedDB = db; return "", nil }
	locator.Locate("th-1")

	if want := filepath.Join(home, "state_10.sqlite"); askedDB != want {
		t.Errorf("引いた DB = %q, want %q", askedDB, want)
	}
}

// TestLocateFallsBackToSessionsDir は名前で探す経路を確かめる。
//
// 状態 DB を持たない古い codex と、DB を引けなかった場合の両方を通る。
func TestLocateFallsBackToSessionsDir(t *testing.T) {
	t.Parallel()

	rolloutName := "rollout-2026-08-14T10-00-00-th-1.jsonl"

	tests := []struct {
		name string
		// dbResult / dbErr は状態 DB の引き結果。
		dbResult string
		dbErr    error
		// withDB は状態 DB のファイルを置くか。
		withDB bool
	}{
		{name: "状態 DB が無い", withDB: false},
		{name: "状態 DB に記録が無い", withDB: true, dbResult: "\n"},
		{name: "sqlite3 が失敗した", withDB: true, dbErr: errors.New("no sqlite3")},
		{name: "記録された場所が消えている", withDB: true, dbResult: "/nowhere/gone.jsonl\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			rollout := filepath.Join(home, "sessions", "2026", "08", "14", rolloutName)
			writeFile(t, rollout, "{}\n")
			if tt.withDB {
				writeFile(t, filepath.Join(home, "state_5.sqlite"), "")
			}

			locator := NewLocator(home, "")
			locator.runSQLite = func(string, string) (string, error) { return tt.dbResult, tt.dbErr }

			if got := locator.Locate("th-1"); got != rollout {
				t.Errorf("Locate = %q, want %q", got, rollout)
			}
		})
	}
}

// TestLocateNotFound は見つからないときに空文字を返すことを確かめる。
//
// 会話ログが無くても done の記録は成り立つため、ここで止まってはいけない。
func TestLocateNotFound(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeFile(t, filepath.Join(home, "sessions", "rollout-th-2.jsonl"), "{}\n")

	locator := NewLocator(home, "")
	locator.runSQLite = func(string, string) (string, error) { return "", nil }

	if got := locator.Locate("th-1"); got != "" {
		t.Errorf("Locate = %q, want 空", got)
	}
}

// TestLocateWithoutCodexHome は CODEX_HOME が無い場合を確かめる。
// 初めて codex を使う環境がこの状態になる。
func TestLocateWithoutCodexHome(t *testing.T) {
	t.Parallel()

	locator := NewLocator(filepath.Join(t.TempDir(), "missing"), "")
	locator.runSQLite = func(string, string) (string, error) {
		t.Error("DB が無いのに引きに行った")
		return "", nil
	}
	if got := locator.Locate("th-1"); got != "" {
		t.Errorf("Locate = %q, want 空", got)
	}
}

// TestNewLocatorDefaultsToHome は CODEX_HOME が空のとき HOME/.codex を見る
// ことを確かめる(現行版の `${CODEX_HOME:-$HOME/.codex}`)。
func TestNewLocatorDefaultsToHome(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	rollout := filepath.Join(home, ".codex", "sessions", "rollout-th-1.jsonl")
	writeFile(t, rollout, "{}\n")

	locator := NewLocator("", home)
	locator.runSQLite = func(string, string) (string, error) { return "", nil }

	if got := locator.Locate("th-1"); got != rollout {
		t.Errorf("Locate = %q, want %q", got, rollout)
	}
}

// TestSelectRolloutPathQuotesThreadID は引用符を閉じられないことを確かめる。
// payload は外から来る値なので、SQL を組み立てる前に閉じておく。
func TestSelectRolloutPathQuotesThreadID(t *testing.T) {
	t.Parallel()

	got := selectRolloutPath("a' OR '1'='1")
	want := "SELECT rollout_path FROM threads WHERE id='a'' OR ''1''=''1' LIMIT 1"
	if got != want {
		t.Errorf("selectRolloutPath = %q, want %q", got, want)
	}
}

// TestLocateIgnoresNonRegularFiles は通常ファイル以外を採らないことを
// 確かめる。
//
// 現行版の `find -type f` に合わせている(find は既定で symlink を辿らない
// ため、symlink は -type f に当たらない)。リンク切れの symlink を採ると、
// 実体の無いパスを transcript_path として記録してしまう。
func TestLocateIgnoresNonRegularFiles(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	sessions := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("ディレクトリを作れない: %v", err)
	}
	// リンク切れの symlink。名前は探索の条件に当てはまる。
	link := filepath.Join(sessions, "rollout-2026-08-14T10-00-00-th-1.jsonl")
	if err := os.Symlink(filepath.Join(home, "gone.jsonl"), link); err != nil {
		t.Fatalf("symlink を作れない: %v", err)
	}
	// 同じ条件に当てはまるディレクトリ。
	if err := os.Mkdir(filepath.Join(sessions, "dir-th-1.jsonl"), 0o755); err != nil {
		t.Fatalf("ディレクトリを作れない: %v", err)
	}

	locator := NewLocator(home, "")
	locator.runSQLite = func(string, string) (string, error) { return "", nil }

	if got := locator.Locate("th-1"); got != "" {
		t.Errorf("Locate = %q, want 空", got)
	}
}
