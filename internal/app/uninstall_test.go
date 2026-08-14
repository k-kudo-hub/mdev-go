package app_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// pendingRootForTest は pending の置き場所(CONDUCTOR_HOME の外)。
const pendingRootForTest = "/home/dev/.claude-pending"

// newTestUninstaller は依存を組み合わせた Uninstaller を返す。
func newTestUninstaller(files *fakeFileStore) *app.Uninstaller {
	return &app.Uninstaller{
		Paths:       testInstallPaths,
		Files:       files,
		PendingRoot: pendingRootForTest,
	}
}

// installedFiles は install 済みの状態を作る。
func installedFiles(t *testing.T) *fakeFileStore {
	t.Helper()
	files := newFakeFileStore()
	runInstall(t, files, allAvailable)
	files.files[conductorPath("daily/sess/2026-08-01.jsonl")] = "{}\n"
	files.files[pendingRootForTest+"/sess/x.json"] = "{}\n"
	return files
}

// TestUninstallRemovesEverything は設定とデータを外すことを確かめる。
func TestUninstallRemovesEverything(t *testing.T) {
	t.Parallel()

	files := installedFiles(t)
	var out bytes.Buffer
	if err := newTestUninstaller(files).Uninstall(&out, false); err != nil {
		t.Fatalf("Uninstall = %v", err)
	}

	if files.Exists(testInstallPaths.ConductorHome) {
		t.Error("CONDUCTOR_HOME が残っている")
	}
	if files.Exists(pendingRootForTest) {
		t.Error("pending が残っている")
	}
	if strings.Contains(files.files[testInstallPaths.Settings], "mdev hook") {
		t.Errorf("hooks が残っている:\n%s", files.files[testInstallPaths.Settings])
	}
	if strings.Contains(files.files[testInstallPaths.CodexConfig], "codex\", \"notify") {
		t.Errorf("codex の notify が残っている: %q", files.files[testInstallPaths.CodexConfig])
	}
	// 消す前に何が失われるかを出す。作業ログはここにしか無い。
	if !strings.Contains(out.String(), "daily") {
		t.Errorf("削除するものの一覧が出ていない:\n%s", out.String())
	}
	if !strings.Contains(out.String(), domain.ZshrcSourceLine) {
		t.Errorf(".zshrc の案内が出ていない:\n%s", out.String())
	}
}

// TestUninstallKeepsData は --keep-data でデータを残すことを確かめる。
func TestUninstallKeepsData(t *testing.T) {
	t.Parallel()

	files := installedFiles(t)
	var out bytes.Buffer
	if err := newTestUninstaller(files).Uninstall(&out, true); err != nil {
		t.Fatalf("Uninstall = %v", err)
	}

	if !files.Exists(conductorPath("daily/sess/2026-08-01.jsonl")) {
		t.Error("作業ログを消した")
	}
	if !files.Exists(pendingRootForTest) {
		t.Error("pending を消した")
	}
	// 設定の解除は行う。
	if strings.Contains(files.files[testInstallPaths.Settings], "mdev hook") {
		t.Error("hooks が残っている")
	}
}

// TestUninstallKeepsForeignHooks は利用者が足した hook を残すことを確かめる。
//
// 現行 uninstall.sh は conductor に触れるイベントを丸ごと落としていたため、
// 同じイベントに足した hook まで一緒に消えていた。
func TestUninstallKeepsForeignHooks(t *testing.T) {
	t.Parallel()

	files := installedFiles(t)
	files.files[testInstallPaths.Settings] = `{
  "hooks": {
    "Stop": [{"matcher": "", "hooks": [
      {"type": "command", "command": "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify"},
      {"type": "command", "command": "my-own-notify"}
    ]}]
  }
}
`
	var out bytes.Buffer
	if err := newTestUninstaller(files).Uninstall(&out, true); err != nil {
		t.Fatalf("Uninstall = %v", err)
	}

	settings := files.files[testInstallPaths.Settings]
	if !strings.Contains(settings, "my-own-notify") {
		t.Errorf("利用者の hook を消した:\n%s", settings)
	}
	if strings.Contains(settings, "mdev hook") {
		t.Errorf("mdev の hook が残っている:\n%s", settings)
	}
}

// TestUninstallOnCleanSystem は何も入っていない環境でも失敗しないことを
// 確かめる。
func TestUninstallOnCleanSystem(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := newTestUninstaller(newFakeFileStore()).Uninstall(&out, false); err != nil {
		t.Fatalf("Uninstall = %v", err)
	}
}
