package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// fakeInstaller は再インストールの代役である。
type fakeInstaller struct {
	calls                     int
	tarballURL, version, repo string
	err                       error
}

func (f *fakeInstaller) Install(tarballURL, version, repoURL string) error {
	f.calls++
	f.tarballURL, f.version, f.repo = tarballURL, version, repoURL
	return f.err
}

// newUpdater は既定で「更新あり」になる Updater を組み立てる。
func newUpdater() (*app.Updater, *fakeUpdateState, *fakeRemoteTags, *fakeInstaller, map[string]string) {
	state := &fakeUpdateState{repoURL: "https://github.com/o/r.git", version: "v0.1.0"}
	remote := &fakeRemoteTags{tag: "v0.2.0", ok: true}
	installer := &fakeInstaller{}
	env := map[string]string{}
	return &app.Updater{
		State:     state,
		Remote:    remote,
		Installer: installer,
		Getenv:    func(k string) string { return env[k] },
	}, state, remote, installer, env
}

// TestUpdateInstallsLatest は更新の成功経路を確かめる。
// test.sh「53. update.sh」の 1 つ目に対応する。
func TestUpdateInstallsLatest(t *testing.T) {
	updater, _, _, installer, _ := newUpdater()
	var out strings.Builder

	if err := updater.Update(&out); err != nil {
		t.Fatalf("Update が失敗しました: %v", err)
	}
	// tarball の URL は REPO_URL から起こした slug で組み立てる。
	if want := "https://github.com/o/r/archive/refs/tags/v0.2.0.tar.gz"; installer.tarballURL != want {
		t.Errorf("tarball URL = %q, want %q", installer.tarballURL, want)
	}
	// tarball には .git が無いため、版と更新元は env で渡すしかない。
	if installer.version != "v0.2.0" || installer.repo != "https://github.com/o/r.git" {
		t.Errorf("install へ渡した値 = (%q, %q)", installer.version, installer.repo)
	}
	for _, want := range []string{"最新バージョンを確認しています", "v0.1.0 -> v0.2.0 に更新します", "v0.2.0 に更新しました"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("出力に %q がありません:\n%q", want, out.String())
		}
	}
}

// TestUpdateHonorsTarballOverride は取得元の差し替えを確かめる。
func TestUpdateHonorsTarballOverride(t *testing.T) {
	updater, _, _, installer, env := newUpdater()
	env[app.TarballURLEnv] = "file:///tmp/release.tar.gz"

	if err := updater.Update(&strings.Builder{}); err != nil {
		t.Fatalf("Update が失敗しました: %v", err)
	}
	if installer.tarballURL != "file:///tmp/release.tar.gz" {
		t.Errorf("tarball URL = %q", installer.tarballURL)
	}
}

// TestUpdateUpToDate は既に最新のときに何も取りに行かないことを確かめる。
func TestUpdateUpToDate(t *testing.T) {
	updater, state, _, installer, _ := newUpdater()
	state.version = "v0.2.0"
	var out strings.Builder

	if err := updater.Update(&out); err != nil {
		t.Fatalf("Update が失敗しました: %v", err)
	}
	if installer.calls != 0 {
		t.Errorf("install が %d 回呼ばれました, want 0", installer.calls)
	}
	if want := "既に最新です（v0.2.0）。"; !strings.Contains(out.String(), want) {
		t.Errorf("出力 = %q, want %q を含む", out.String(), want)
	}
}

// TestUpdateFailures は失敗が必ず error になることを確かめる。
//
// 更新確認(黙って諦める)と違い、こちらは利用者が明示的に叩くコマンドなので、
// 黙って何もしないと古いまま使い続けることになる。
func TestUpdateFailures(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(state *fakeUpdateState, remote *fakeRemoteTags, installer *fakeInstaller)
		wantMsg string
	}{
		{
			name: "REPO_URL が無い",
			setup: func(state *fakeUpdateState, _ *fakeRemoteTags, _ *fakeInstaller) {
				state.repoURL = ""
			},
			wantMsg: "更新元リポジトリが不明です",
		},
		{
			name: "REPO_URL を解釈できない",
			setup: func(state *fakeUpdateState, _ *fakeRemoteTags, _ *fakeInstaller) {
				state.repoURL = "notaurl"
			},
			wantMsg: "リポジトリURLを解釈できません",
		},
		{
			name: "最新版を取れない",
			setup: func(_ *fakeUpdateState, remote *fakeRemoteTags, _ *fakeInstaller) {
				remote.ok = false
			},
			wantMsg: "最新バージョンの取得に失敗しました",
		},
		{
			name: "再インストールに失敗",
			setup: func(_ *fakeUpdateState, _ *fakeRemoteTags, installer *fakeInstaller) {
				installer.err = errors.New("ダウンロードに失敗しました")
			},
			wantMsg: "ダウンロードに失敗しました",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updater, state, remote, installer, _ := newUpdater()
			tt.setup(state, remote, installer)
			var out strings.Builder

			err := updater.Update(&out)
			if err == nil {
				t.Fatalf("error になりませんでした(出力 = %q)", out.String())
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %v, want %q を含む", err, tt.wantMsg)
			}
			// 失敗したのに「更新しました」と出してはならない。
			if strings.Contains(out.String(), "更新しました") {
				t.Errorf("失敗したのに完了と出ています: %q", out.String())
			}
		})
	}
}

// TestUpdateNormalizesBrokenVersion は VERSION が壊れていても更新できる
// ことを確かめる。現行版はここで算術エラーになる。
func TestUpdateNormalizesBrokenVersion(t *testing.T) {
	updater, state, _, installer, _ := newUpdater()
	state.version = ""
	var out strings.Builder

	if err := updater.Update(&out); err != nil {
		t.Fatalf("Update が失敗しました: %v", err)
	}
	if installer.calls != 1 {
		t.Errorf("install の呼び出し = %d, want 1", installer.calls)
	}
	if want := "v0.0.0 -> v0.2.0 に更新します"; !strings.Contains(out.String(), want) {
		t.Errorf("出力 = %q, want %q を含む", out.String(), want)
	}
}
