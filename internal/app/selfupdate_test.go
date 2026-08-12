package app_test

import (
	"errors"
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
