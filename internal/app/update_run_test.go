package app_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// fakeInstallRunner は設定の貼り直しの代役である。
type fakeInstallRunner struct {
	calls int
	err   error
}

func (f *fakeInstallRunner) Install(out io.Writer) error {
	f.calls++
	_, _ = io.WriteString(out, "install を実行しました\n")
	return f.err
}

// newUpdater は自バイナリが最新の Updater を組み立てる。
func newUpdater() (*app.Updater, *fakeUpdateState, *fakeRemoteTags, *fakeInstallRunner) {
	state := &fakeUpdateState{repoURL: domain.MdevRepoURL, version: "v0.2.0"}
	remote := &fakeRemoteTags{tag: "v0.2.0", ok: true}
	install := &fakeInstallRunner{}
	return &app.Updater{
		State:  state,
		Remote: remote,
		Self: &app.SelfUpdater{
			Version: "v0.2.0",
			Remote:  remote,
		},
		Install: install,
	}, state, remote, install
}

// TestUpdateDoesNothingWhenUpToDate は既に最新なら何もしないことを確かめる。
//
// 毎回 install を通すと、更新のつもりで叩いたコマンドが設置の処理を走らせる
// ことになり、「何もしなくてよかった」ことが分からない。
func TestUpdateDoesNothingWhenUpToDate(t *testing.T) {
	t.Parallel()

	updater, _, _, install := newUpdater()
	var out strings.Builder

	if err := updater.Update(&out); err != nil {
		t.Fatalf("Update = %v", err)
	}
	if install.calls != 0 {
		t.Errorf("install の呼び出し = %d 回, want 0", install.calls)
	}
	if !strings.Contains(out.String(), "既に最新です") {
		t.Errorf("出力 =\n%s", out.String())
	}
}

// TestUpdateAppliesInstallWhenNewer は版が上がっているときに設定を貼り直す
// ことを確かめる。
//
// ADR D4-2 の 2 段目である。conductor の tarball を取ってきて install.sh を
// bash で走らせる旧フローは、REPO_URL が mdev-go を指した時点で成立しない
// (その tarball には install.sh も scripts/ も無い)。
func TestUpdateAppliesInstallWhenNewer(t *testing.T) {
	t.Parallel()

	updater, state, remote, install := newUpdater()
	state.version = "v0.1.0"
	remote.tag = "v0.2.0"
	var out strings.Builder

	if err := updater.Update(&out); err != nil {
		t.Fatalf("Update = %v", err)
	}
	if install.calls != 1 {
		t.Errorf("install の呼び出し = %d 回, want 1", install.calls)
	}
	for _, want := range []string{"v0.1.0 -> v0.2.0 に更新します", "install を実行しました", "v0.2.0 に更新しました"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("出力に %q がありません:\n%s", want, out.String())
		}
	}
}

// TestUpdateFailsWhenLatestTagUnavailable は最新版を引けないときにエラーを
// 返すことを確かめる。
//
// 更新確認(check-update)はセッションの起動前に走るので黙って諦めるが、
// こちらは利用者が明示的に叩くコマンドである。黙って「何もありませんでした」と
// 答えると、更新したつもりで古いまま使い続ける。
func TestUpdateFailsWhenLatestTagUnavailable(t *testing.T) {
	t.Parallel()

	updater, _, remote, install := newUpdater()
	remote.ok = false

	var out strings.Builder
	err := updater.Update(&out)
	if err == nil {
		t.Fatal("エラーを返すはず")
	}
	if !strings.Contains(err.Error(), "最新バージョンの取得に失敗しました") {
		t.Errorf("説明 = %v", err)
	}
	if install.calls != 0 {
		t.Errorf("install を呼んだ: %d 回", install.calls)
	}
}

// TestUpdateFailsWithoutRepoURL は更新元が分からないときにエラーを返すことを
// 確かめる。
func TestUpdateFailsWithoutRepoURL(t *testing.T) {
	t.Parallel()

	updater, state, _, install := newUpdater()
	state.repoURL = ""

	if err := updater.Update(&strings.Builder{}); err == nil {
		t.Fatal("エラーを返すはず")
	}
	if install.calls != 0 {
		t.Errorf("install を呼んだ: %d 回", install.calls)
	}
}

// TestUpdateStopsAfterSelfReplace は自分を置き換えたらそこで終えることを
// 確かめる。
//
// 今動いているのは置き換える **前** の中身なので、そのまま設定を貼ると
// 古い実装で貼ることになる。実行し直しを案内して終える。
func TestUpdateStopsAfterSelfReplace(t *testing.T) {
	t.Parallel()
	skipUnsupportedPlatform(t)

	updater, _, _, install := newUpdater()
	self, _, replacer := newSelfUpdater("v0.10.0")
	updater.Self = self

	var out strings.Builder
	if err := updater.Update(&out); err != nil {
		t.Fatalf("Update = %v", err)
	}
	if install.calls != 0 {
		t.Errorf("置き換えた後に install を呼んだ: %d 回", install.calls)
	}
	if len(replacer.calls) != 1 {
		t.Errorf("自己置換 = %d 回, want 1", len(replacer.calls))
	}
}

// TestUpdateReportsSelfUpdateFailure は自己置換の失敗を返すことを確かめる。
//
// 失敗したまま設定を貼ると、新しい設定と古いバイナリが混ざる。
func TestUpdateReportsSelfUpdateFailure(t *testing.T) {
	t.Parallel()
	skipUnsupportedPlatform(t)

	updater, _, _, install := newUpdater()
	self, _, replacer := newSelfUpdater("v0.10.0")
	replacer.err = errors.New("置き換えられない")
	updater.Self = self

	var out strings.Builder
	if err := updater.Update(&out); err == nil {
		t.Fatal("エラーを返すはず")
	}
	if install.calls != 0 {
		t.Errorf("失敗したのに install を呼んだ: %d 回", install.calls)
	}
}

// TestUpdateReportsInstallFailure は設定の貼り直しの失敗を返すことを確かめる。
//
// 利用者が明示的に叩くコマンドなので、黙って何もしないと更新したつもりで
// 古いまま使い続けることになる。
func TestUpdateReportsInstallFailure(t *testing.T) {
	t.Parallel()

	updater, state, remote, install := newUpdater()
	state.version = "v0.1.0"
	remote.tag = "v0.2.0"
	install.err = errors.New("書けない")

	var out strings.Builder
	if err := updater.Update(&out); err == nil {
		t.Fatal("エラーを返すはず")
	}
}

// TestUpdateWithoutSelfUpdater は自バイナリの更新を持たない構成を確かめる。
func TestUpdateWithoutSelfUpdater(t *testing.T) {
	t.Parallel()

	updater, state, remote, install := newUpdater()
	state.version = "v0.1.0"
	remote.tag = "v0.2.0"
	updater.Self = nil

	var out strings.Builder
	if err := updater.Update(&out); err != nil {
		t.Fatalf("Update = %v", err)
	}
	if install.calls != 1 {
		t.Errorf("install の呼び出し = %d 回, want 1", install.calls)
	}
}
