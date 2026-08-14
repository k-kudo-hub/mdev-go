package app_test

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// fakeReplacer は自バイナリの置き換えの代役である。
type fakeReplacer struct {
	calls []domain.SelfUpdatePlan
	path  string
	err   error
}

func (r *fakeReplacer) Replace(plan domain.SelfUpdatePlan) (string, error) {
	r.calls = append(r.calls, plan)
	if r.err != nil {
		return "", r.err
	}
	return r.path, nil
}

// newSelfUpdater は既定で「新しい版がある」状態の自己更新を返す。
func newSelfUpdater(version string) (*app.SelfUpdater, *fakeRemoteTags, *fakeReplacer) {
	remote := &fakeRemoteTags{tag: "v0.11.0", ok: true}
	replacer := &fakeReplacer{path: "/home/u/.claude-conductor/bin/mdev"}
	return &app.SelfUpdater{Version: version, Remote: remote, Replacer: replacer}, remote, replacer
}

// skipUnsupportedPlatform は配布対象外の環境でテストを飛ばす。
func skipUnsupportedPlatform(t *testing.T) {
	t.Helper()
	if _, ok := domain.CurrentAssetName(); !ok {
		t.Skipf("配布対象外の環境(%s/%s)", runtime.GOOS, runtime.GOARCH)
	}
}

// TestSelfUpdateReplaces は新しい版があるときに置き換えることを確かめる。
func TestSelfUpdateReplaces(t *testing.T) {
	t.Parallel()
	skipUnsupportedPlatform(t)

	updater, _, replacer := newSelfUpdater("v0.10.0")
	var out strings.Builder

	got, err := updater.Run(&out)
	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if !got.Replaced || got.Latest != "v0.11.0" {
		t.Errorf("結果 = %+v, want 置き換え済み v0.11.0", got)
	}
	if len(replacer.calls) != 1 {
		t.Fatalf("置き換えの呼び出し = %d 回, want 1", len(replacer.calls))
	}
	plan := replacer.calls[0]
	if !strings.Contains(plan.AssetURL, "/releases/download/v0.11.0/mdev_darwin_") {
		t.Errorf("取得先 = %q", plan.AssetURL)
	}
	if !strings.Contains(plan.ChecksumsURL, "checksums.txt") {
		t.Errorf("checksums の取得先 = %q", plan.ChecksumsURL)
	}
	// 置き換えたら実行し直してもらう(今のプロセスは古いままのため)。
	if !strings.Contains(out.String(), "もう一度 `mdev update` を実行") {
		t.Errorf("実行し直しの案内がありません:\n%s", out.String())
	}
}

// TestSelfUpdateSkipsDevBuild は開発中のビルドで置き換えないことを確かめる。
//
// 手元で組んだバイナリを配布物で上書きすると、検証中の変更を黙って消す。
// ただし黙って飛ばすと「更新された」と思い込まれるので、断りは出す。
func TestSelfUpdateSkipsDevBuild(t *testing.T) {
	t.Parallel()
	skipUnsupportedPlatform(t)

	for _, version := range []string{"dev", ""} {
		updater, _, replacer := newSelfUpdater(version)
		var out strings.Builder

		got, err := updater.Run(&out)
		if err != nil {
			t.Fatalf("Run() = %v", err)
		}
		if got.Replaced {
			t.Errorf("版 %q で置き換えました", version)
		}
		if len(replacer.calls) != 0 {
			t.Errorf("版 %q で取得しました", version)
		}
		if !strings.Contains(out.String(), "版が焼き込まれていない") {
			t.Errorf("断りがありません:\n%s", out.String())
		}
	}
}

// TestSelfUpdateQuietPaths は何もしない経路が無言で成功することを確かめる。
//
// 自分の更新が要らない・できないだけで、続けて行う conductor 資産の更新まで
// 止める理由は無い。
func TestSelfUpdateQuietPaths(t *testing.T) {
	t.Parallel()
	skipUnsupportedPlatform(t)

	tests := []struct {
		name  string
		setup func(remote *fakeRemoteTags) string
	}{
		{
			name:  "既に最新",
			setup: func(*fakeRemoteTags) string { return "v0.11.0" },
		},
		{
			name: "配布元へ到達できない",
			setup: func(remote *fakeRemoteTags) string {
				remote.ok = false
				return "v0.10.0"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			updater, remote, replacer := newSelfUpdater("v0.10.0")
			updater.Version = tt.setup(remote)
			var out strings.Builder

			got, err := updater.Run(&out)
			if err != nil {
				t.Fatalf("Run() = %v", err)
			}
			if got.Replaced {
				t.Error("置き換えました")
			}
			if len(replacer.calls) != 0 {
				t.Error("取得しました")
			}
			if out.String() != "" {
				t.Errorf("出力 = %q, want 無言", out.String())
			}
		})
	}
}

// TestSelfUpdateReportsReplaceFailure は置き換えに踏み切った後の失敗を
// error にすることを確かめる。
//
// 素性の分からないバイナリを置いた可能性を黙って流してはならない。
func TestSelfUpdateReportsReplaceFailure(t *testing.T) {
	t.Parallel()
	skipUnsupportedPlatform(t)

	updater, _, replacer := newSelfUpdater("v0.10.0")
	replacer.err = errors.New("SHA-256 が一致しません")
	var out strings.Builder

	if _, err := updater.Run(&out); err == nil {
		t.Fatal("error になりませんでした")
	}
}

// TestUpdateStopsAfterSelfReplace は自バイナリを置き換えたら conductor の
// 資産更新へ進まないことを確かめる。
//
// 今動いているプロセスは置き換える前の中身のままなので、続けると古い実装で
// 資産を入れ直すことになる。実行し直してもらえば、すべてを新しい mdev が行う。
func TestUpdateStopsAfterSelfReplace(t *testing.T) {
	t.Parallel()
	skipUnsupportedPlatform(t)

	updater, _, _, installer, _ := newUpdater()
	self, _, _ := newSelfUpdater("v0.10.0")
	updater.Self = self
	var out strings.Builder

	if err := updater.Update(&out); err != nil {
		t.Fatalf("Update() = %v", err)
	}
	if installer.calls != 0 {
		t.Errorf("conductor 資産の更新が %d 回走りました, want 0", installer.calls)
	}
	if !strings.Contains(out.String(), "もう一度 `mdev update` を実行") {
		t.Errorf("実行し直しの案内がありません:\n%s", out.String())
	}
}

// TestUpdateContinuesWhenSelfIsCurrent は自バイナリが最新なら従来どおり
// conductor の資産を更新することを確かめる。
//
// **よくある経路の動きを変えない。** 自己更新を足したせいで毎回 2 回
// 実行させられるようでは使いにくい。
func TestUpdateContinuesWhenSelfIsCurrent(t *testing.T) {
	t.Parallel()
	skipUnsupportedPlatform(t)

	updater, _, _, installer, _ := newUpdater()
	self, _, _ := newSelfUpdater("v0.11.0") // 配布元と同じ = 最新
	updater.Self = self
	var out strings.Builder

	if err := updater.Update(&out); err != nil {
		t.Fatalf("Update() = %v", err)
	}
	if installer.calls != 1 {
		t.Errorf("conductor 資産の更新 = %d 回, want 1", installer.calls)
	}
}

// TestUpdateWithoutSelfUpdater は自己更新を組み込まない構成でも動くことを
// 確かめる(従来どおりの動き)。
func TestUpdateWithoutSelfUpdater(t *testing.T) {
	t.Parallel()

	updater, _, _, installer, _ := newUpdater()
	updater.Self = nil

	if err := updater.Update(&strings.Builder{}); err != nil {
		t.Fatalf("Update() = %v", err)
	}
	if installer.calls != 1 {
		t.Errorf("conductor 資産の更新 = %d 回, want 1", installer.calls)
	}
}

// TestSelfUpdateContinuesWhenNotStarted は **置き換えに踏み切る前の失敗で
// 全体を止めない** ことを確かめる(契約)。
//
// アセットが 404、checksums に自分の環境の値が無い、SHA-256 が合わない、
// といった失敗では実行ファイルに一切触れていない。自分を新しくできないことと、
// conductor の資産を新しくできることは別の話である。
func TestSelfUpdateContinuesWhenNotStarted(t *testing.T) {
	t.Parallel()
	skipUnsupportedPlatform(t)

	updater, _, replacer := newSelfUpdater("v0.10.0")
	replacer.err = fmt.Errorf("%w: 状態コード 404", app.ErrSelfUpdateNotStarted)
	var out strings.Builder

	got, err := updater.Run(&out)
	if err != nil {
		t.Fatalf("踏み切る前の失敗で止まりました: %v", err)
	}
	if got.Replaced {
		t.Error("置き換えたことになっています")
	}
	// 黙って飛ばすと「更新された」と思い込まれる。
	if !strings.Contains(out.String(), "更新できませんでした") {
		t.Errorf("断りがありません:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "更新は続けます") {
		t.Errorf("続行することが伝わりません:\n%s", out.String())
	}
}

// TestUpdateContinuesWhenSelfUpdateUnavailable は 404 のときに conductor の
// 資産更新まで進むことを確かめる(契約の全体像)。
func TestUpdateContinuesWhenSelfUpdateUnavailable(t *testing.T) {
	t.Parallel()
	skipUnsupportedPlatform(t)

	updater, _, _, installer, _ := newUpdater()
	self, _, replacer := newSelfUpdater("v0.10.0")
	replacer.err = fmt.Errorf("%w: 状態コード 404", app.ErrSelfUpdateNotStarted)
	updater.Self = self
	var out strings.Builder

	if err := updater.Update(&out); err != nil {
		t.Fatalf("Update() = %v", err)
	}
	if installer.calls != 1 {
		t.Errorf("conductor 資産の更新 = %d 回, want 1", installer.calls)
	}
}

// TestSelfUpdateStopsWhenReplaceFailed は **踏み切った後の失敗は止める**
// ことを確かめる。
//
// rename の失敗がこれに当たる。実行ファイルがどうなったか分からない状態で
// 資産の更新へ進むわけにはいかない。
func TestSelfUpdateStopsWhenReplaceFailed(t *testing.T) {
	t.Parallel()
	skipUnsupportedPlatform(t)

	updater, _, _, installer, _ := newUpdater()
	self, _, replacer := newSelfUpdater("v0.10.0")
	replacer.err = errors.New("バイナリの置き換えに失敗しました")
	updater.Self = self

	if err := updater.Update(&strings.Builder{}); err == nil {
		t.Fatal("踏み切った後の失敗で止まりませんでした")
	}
	if installer.calls != 0 {
		t.Errorf("conductor 資産の更新が %d 回走りました, want 0", installer.calls)
	}
}

// TestSelfUpdateSkipsNetworkForDevBuild は dev ビルドで配布元へ問い合わせ
// ないことを確かめる。
//
// 手元で組んだバイナリのために ls-remote を撃つ意味は無い(セッションの
// 起動前に走る経路でもある)。
func TestSelfUpdateSkipsNetworkForDevBuild(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"dev", "", "v0.10.1-1-gabc1234"} {
		updater, remote, replacer := newSelfUpdater(version)
		var out strings.Builder

		if _, err := updater.Run(&out); err != nil {
			t.Fatalf("Run() = %v", err)
		}
		if remote.calls != 0 {
			t.Errorf("版 %q で配布元へ %d 回問い合わせました", version, remote.calls)
		}
		if len(replacer.calls) != 0 {
			t.Errorf("版 %q で取得しました", version)
		}
		if !strings.Contains(out.String(), "版が焼き込まれていない") {
			t.Errorf("版 %q の断りがありません:\n%s", version, out.String())
		}
	}
}
