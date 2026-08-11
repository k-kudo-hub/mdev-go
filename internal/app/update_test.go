package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// fakeUpdateState は CONDUCTOR_HOME 直下の状態ファイルの代役である。
type fakeUpdateState struct {
	repoURL   string
	version   string
	cacheDate string
	cacheTag  string
	writeErr  error

	written []string
}

func (f *fakeUpdateState) RepoURL() string                   { return f.repoURL }
func (f *fakeUpdateState) InstalledVersion() string          { return f.version }
func (f *fakeUpdateState) ReadUpdateCache() (string, string) { return f.cacheDate, f.cacheTag }

func (f *fakeUpdateState) WriteUpdateCache(date, tag string) error {
	f.written = append(f.written, date+" "+tag)
	if f.writeErr != nil {
		return f.writeErr
	}
	f.cacheDate, f.cacheTag = date, tag
	return nil
}

// fakeRemoteTags はリモートのタグ引きの代役である。
type fakeRemoteTags struct {
	tag   string
	ok    bool
	calls int
	urls  []string
}

func (f *fakeRemoteTags) LatestTag(url string) (string, bool) {
	f.calls++
	f.urls = append(f.urls, url)
	return f.tag, f.ok
}

// newUpdateChecker は既定で「更新あり」になる更新確認を組み立てる。
func newUpdateChecker() (*app.UpdateChecker, *fakeConfigLoader, *fakeUpdateState, *fakeRemoteTags) {
	config := &fakeConfigLoader{}
	state := &fakeUpdateState{repoURL: "git@github.com:o/r.git", version: "v0.1.0"}
	remote := &fakeRemoteTags{tag: "v0.2.0", ok: true}
	return &app.UpdateChecker{
		Config: config,
		State:  state,
		Remote: remote,
		Clock:  testClock,
	}, config, state, remote
}

// notice は testClock の日付での期待される案内である。
const wantNotice = "\n  📦 新しいバージョン v0.2.0 があります（現在: v0.1.0）。\n" +
	"     'mdev update' で更新できます。\n\n"

// TestUpdateCheckShowsNotice は更新がある場合に案内が出ることを確かめる。
// test.sh「52. check-update.sh」の 1 つ目に対応する。
func TestUpdateCheckShowsNotice(t *testing.T) {
	checker, _, state, remote := newUpdateChecker()

	if got := checker.Check(true); got != wantNotice {
		t.Errorf("案内 = %q, want %q", got, wantNotice)
	}
	if got := remote.urls; len(got) != 1 || got[0] != "git@github.com:o/r.git" {
		t.Errorf("引いた URL = %v", got)
	}
	// 引いた結果は日付付きで残す(1 日 1 回に絞るため)。
	if want := []string{"2026-08-08 v0.2.0"}; len(state.written) != 1 || state.written[0] != want[0] {
		t.Errorf("キャッシュ = %v, want %v", state.written, want)
	}
}

// TestUpdateCheckSilentCases は「黙って何もしない」経路を確かめる。
// ここで何かを出すと、セッションの起動のたびに邪魔になる。
func TestUpdateCheckSilentCases(t *testing.T) {
	tests := []struct {
		name  string
		setup func(config *fakeConfigLoader, state *fakeUpdateState, remote *fakeRemoteTags)
	}{
		{
			name: "設定で明示的に無効",
			setup: func(config *fakeConfigLoader, _ *fakeUpdateState, _ *fakeRemoteTags) {
				config.config.UpdateCheck.Disabled = true
			},
		},
		{
			name: "REPO_URL が空",
			setup: func(_ *fakeConfigLoader, state *fakeUpdateState, _ *fakeRemoteTags) {
				state.repoURL = ""
			},
		},
		{
			name: "リモートへ到達できない",
			setup: func(_ *fakeConfigLoader, _ *fakeUpdateState, remote *fakeRemoteTags) {
				remote.ok = false
			},
		},
		{
			name: "既に最新",
			setup: func(_ *fakeConfigLoader, state *fakeUpdateState, _ *fakeRemoteTags) {
				state.version = "v0.2.0"
			},
		},
		{
			name: "インストール済みのほうが新しい",
			setup: func(_ *fakeConfigLoader, state *fakeUpdateState, _ *fakeRemoteTags) {
				state.version = "v0.3.0"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, config, state, remote := newUpdateChecker()
			tt.setup(config, state, remote)

			if got := checker.Check(true); got != "" {
				t.Errorf("案内 = %q, want 空", got)
			}
		})
	}
}

// TestUpdateCheckNormalizesBrokenVersion は VERSION が壊れていても
// 比較が働くことを確かめる。現行版はここで算術エラーになる。
func TestUpdateCheckNormalizesBrokenVersion(t *testing.T) {
	for _, version := range []string{"", "not-a-version", "1.2"} {
		checker, _, state, _ := newUpdateChecker()
		state.version = version

		want := "\n  📦 新しいバージョン v0.2.0 があります（現在: v0.0.0）。\n" +
			"     'mdev update' で更新できます。\n\n"
		if got := checker.Check(true); got != want {
			t.Errorf("VERSION=%q の案内 = %q, want %q", version, got, want)
		}
	}
}

// TestUpdateCheckUsesCache は今日ぶんのキャッシュがあればリモートへ出ない
// ことを確かめる。セッションを開くたびに引くと起動が目に見えて遅くなる。
func TestUpdateCheckUsesCache(t *testing.T) {
	checker, _, state, remote := newUpdateChecker()
	state.cacheDate, state.cacheTag = "2026-08-08", "v0.9.0"

	got := checker.Check(false)
	if remote.calls != 0 {
		t.Errorf("リモートへ %d 回出ました, want 0", remote.calls)
	}
	if want := "v0.9.0"; !strings.Contains(got, want) {
		t.Errorf("案内 = %q, want %q を含む", got, want)
	}
	if len(state.written) != 0 {
		t.Errorf("キャッシュを書き直しています: %v", state.written)
	}
}

// TestUpdateCheckRefetchesStaleCache は昨日のキャッシュを使わないことを
// 確かめる。
func TestUpdateCheckRefetchesStaleCache(t *testing.T) {
	checker, _, state, remote := newUpdateChecker()
	state.cacheDate, state.cacheTag = "2026-08-07", "v0.9.0"

	if got := checker.Check(false); got != wantNotice {
		t.Errorf("案内 = %q, want %q", got, wantNotice)
	}
	if remote.calls != 1 {
		t.Errorf("リモートへ %d 回出ました, want 1", remote.calls)
	}
}

// TestUpdateCheckForceIgnoresCache は force で今日のキャッシュも無視する
// ことを確かめる(現行版の --force)。
func TestUpdateCheckForceIgnoresCache(t *testing.T) {
	checker, _, state, remote := newUpdateChecker()
	state.cacheDate, state.cacheTag = "2026-08-08", "v0.9.0"

	if got := checker.Check(true); got != wantNotice {
		t.Errorf("案内 = %q, want %q", got, wantNotice)
	}
	if remote.calls != 1 {
		t.Errorf("リモートへ %d 回出ました, want 1", remote.calls)
	}
}

// TestUpdateCheckShowsNoticeWhenCacheWriteFails はキャッシュを書けなくても
// 案内は出ることを確かめる(次回また引き直すだけ)。
func TestUpdateCheckShowsNoticeWhenCacheWriteFails(t *testing.T) {
	checker, _, state, _ := newUpdateChecker()
	state.writeErr = errors.New("書けない")

	if got := checker.Check(true); got != wantNotice {
		t.Errorf("案内 = %q, want %q", got, wantNotice)
	}
}

// TestUpdateCheckIgnoresUnreadableConfig は設定を読めなくても確認を続ける
// ことを確かめる。設定が無い環境は「既定で有効」である。
func TestUpdateCheckIgnoresUnreadableConfig(t *testing.T) {
	checker, config, _, _ := newUpdateChecker()
	config.config = domain.Config{}
	config.failed = true

	if got := checker.Check(true); got != wantNotice {
		t.Errorf("案内 = %q, want %q", got, wantNotice)
	}
}
