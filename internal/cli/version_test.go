package cli

import (
	"strings"
	"testing"
)

// TestVersionCommand は `mdev version` の出力を固定する。
//
// install がバイナリの自己申告と VERSION ファイルの一致を確かめるため
// (ADR-0004 D3)、機械が読める 1 行だけを出す。
func TestVersionCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "焼き込まれた版", version: "v0.11.0", want: "v0.11.0\n"},
		// 焼き込まれていないビルドは dev になる。自己更新はこれを見て
		// 何もしない(手元のビルドを配布物で上書きしないため)。
		{name: "焼き込み無し", version: "", want: "dev\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			code, stdout, stderr := runCLIWithOut(t, Deps{Version: tt.version}, "version")
			if code != exitOK {
				t.Fatalf("終了コード = %d, want %d (stderr=%q)", code, exitOK, stderr)
			}
			if stdout != tt.want {
				t.Errorf("出力 = %q, want %q", stdout, tt.want)
			}
		})
	}
}

// TestVersionOrDev は版の既定値の扱いを固定する。
func TestVersionOrDev(t *testing.T) {
	t.Parallel()

	if got := (Deps{}).VersionOrDev(); got != DevVersion {
		t.Errorf("VersionOrDev() = %q, want %q", got, DevVersion)
	}
	if got := (Deps{Version: "v1.2.3"}).VersionOrDev(); got != "v1.2.3" {
		t.Errorf("VersionOrDev() = %q, want v1.2.3", got)
	}
}

// TestVersionCommandTakesNoArgs は引数を受け付けないことを確かめる。
func TestVersionCommandTakesNoArgs(t *testing.T) {
	t.Parallel()

	code, _, stderr := runCLIWithOut(t, Deps{Version: "v1.0.0"}, "version", "extra")
	if code != exitError {
		t.Errorf("終了コード = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "extra") {
		t.Errorf("stderr = %q, want 余分な引数を伝える", stderr)
	}
}
