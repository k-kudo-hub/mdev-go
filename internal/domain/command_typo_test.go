package domain_test

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// commands はテストで使う既知のコマンド名である。
var commands = []string{"install", "uninstall", "news", "test", "dev", "attach", "update"}

// TestNearestCommandFindsTypos は 1 文字違いを打ち間違いとして拾うことを
// 確かめる。
//
// `mdev <未知の引数>` はセッション名として扱われる。そのままでは
// `mdev instal` が黙って「instal というセッションを開く」に化ける。
func TestNearestCommandFindsTypos(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "1 文字抜け", input: "instal", want: "install"},
		{name: "1 文字余り", input: "installl", want: "install"},
		{name: "1 文字取り違え", input: "instakl", want: "install"},
		{name: "短いコマンドの取り違え", input: "dee", want: "dev"},
		{name: "先頭の取り違え", input: "tews", want: "news"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := domain.NearestCommand(tt.input, commands)
			if !ok {
				t.Fatalf("NearestCommand(%q) = 見つからない, want %q", tt.input, tt.want)
			}
			if got != tt.want {
				t.Errorf("NearestCommand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestNearestCommandKeepsIntentionalNames は打ち間違いでない名前を
// そのまま通すことを確かめる。
//
// **これが本命である。** 差し戻しを広げすぎると、承認済みの「未知引数 =
// セッション名」が使えなくなる。2 文字以上違うならそれは別の語である。
func TestNearestCommandKeepsIntentionalNames(t *testing.T) {
	t.Parallel()

	tests := []string{
		"my-project",
		"api-server",
		"fix-bug",
		"レビュー",
		"",
		"newsroom",  // news + 4 文字
		"developer", // dev + 6 文字
		"devx",      // dev + 1 文字だが…
	}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := domain.NearestCommand(name, commands)
			if name == "devx" {
				// 1 文字違いなので拾う。これは仕様どおりで、`mdev attach devx`
				// で開ける旨を案内する。
				if !ok || got != "dev" {
					t.Errorf("NearestCommand(%q) = (%q, %v), want (dev, true)", name, got, ok)
				}
				return
			}
			if ok {
				t.Errorf("NearestCommand(%q) = %q, want 見つからない", name, got)
			}
		})
	}
}

// TestNearestCommandIgnoresExactMatch は完全一致を返さないことを確かめる。
// 完全一致は呼び出し側(cobra)が先に解決している。
func TestNearestCommandIgnoresExactMatch(t *testing.T) {
	t.Parallel()

	if got, ok := domain.NearestCommand("install", commands); ok {
		t.Errorf("NearestCommand(install) = %q, want 見つからない", got)
	}
}

// TestRenderCommandTypo は案内に逃げ道が書かれていることを確かめる。
//
// 差し戻すだけでは、本当にその名前のセッションを開きたい利用者が詰まる。
func TestRenderCommandTypo(t *testing.T) {
	t.Parallel()

	got := domain.RenderCommandTypo("instal", "install")
	for _, want := range []string{"instal", "install", "mdev attach instal"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が無い:\n%s", want, got)
		}
	}
}

// TestRenderOpeningSession は開く前に名前を出すことを確かめる。
//
// 未知の引数をセッション名として扱う以上、何が起きるかを先に言う。
// 打ち間違いが差し戻しをすり抜けても、画面に名前が出ていれば気づける。
func TestRenderOpeningSession(t *testing.T) {
	t.Parallel()

	got := domain.RenderOpeningSession("my-project")
	if !strings.Contains(got, "my-project") || !strings.HasSuffix(got, "\n") {
		t.Errorf("RenderOpeningSession = %q", got)
	}
}
