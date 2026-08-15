package app_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// allAvailable は依存がすべて揃っている状況である。
var allAvailable = map[string]bool{
	domain.ZellijCommand:   true,
	domain.ClaudeCommand:   true,
	domain.CodexCommand:    true,
	domain.NotifierCommand: true,
}

// runInstall は install を走らせて出力を返す。
func runInstall(t *testing.T, files *fakeFileStore, available map[string]bool) string {
	t.Helper()
	var out bytes.Buffer
	if err := newTestInstaller(files, available).Install(&out); err != nil {
		t.Fatalf("Install = %v (出力: %s)", err, out.String())
	}
	return out.String()
}

// TestInstallFreshEnvironment はまっさらな環境への設置を確かめる。
func TestInstallFreshEnvironment(t *testing.T) {
	t.Parallel()

	files := newFakeFileStore()
	runInstall(t, files, allAvailable)

	for _, rel := range []string{
		"config.default.json", "config.json", "init.zsh",
		"layouts/multi.kdl", "layouts/dev.kdl", "VERSION", "REPO_URL",
	} {
		if _, ok := files.files[conductorPath(rel)]; !ok {
			t.Errorf("%s が配置されていない", rel)
		}
	}
	// hooks.json はディスクへ置かない。誰も読まないうえ、置いてあると
	// 「これを編集すれば hooks が変わる」と読めてしまう。
	if _, ok := files.files[conductorPath("hooks.json")]; ok {
		t.Error("hooks.json を配置した")
	}
	if got := files.files[conductorPath("VERSION")]; got != "v1.2.3\n" {
		t.Errorf("VERSION = %q, want バイナリ自身の版", got)
	}
	if got := files.files[conductorPath("REPO_URL")]; got != domain.MdevRepoURL+"\n" {
		t.Errorf("REPO_URL = %q, want %q", got, domain.MdevRepoURL)
	}
	// hooks は mdev を指した状態で作られる(雛形は Shell 版を指している)。
	settings := files.files[testInstallPaths.Settings]
	if strings.Contains(settings, "/scripts/") {
		t.Errorf("hooks が Shell 版を指している:\n%s", settings)
	}
	// codex の notify も入る。
	if !strings.Contains(files.files[testInstallPaths.CodexConfig], "codex\", \"notify") {
		t.Errorf("codex の notify が入っていない: %q", files.files[testInstallPaths.CodexConfig])
	}
}

// TestInstallIsIdempotent は 2 回目がファイルを 1 つも書かないことを確かめる。
//
// install は更新のたびに走る。毎回書き換えると、利用者のファイルの更新時刻が
// 動き続け、バックアップや同期の差分が無意味に増える。
func TestInstallIsIdempotent(t *testing.T) {
	t.Parallel()

	files := newFakeFileStore()
	runInstall(t, files, allAvailable)

	files.writes = nil
	files.removed = nil
	runInstall(t, files, allAvailable)

	if len(files.writes) != 0 {
		t.Errorf("2 回目が書き込んだ: %v", files.writes)
	}
	if len(files.removed) != 0 {
		t.Errorf("2 回目が削除した: %v", files.removed)
	}
}

// TestInstallKeepsUserFiles は利用者が手を入れたものを消さないことを確かめる。
//
// レイアウトは「不在時のみ書く」ので、既にあるものは上書きされない。
// config.json も中身は保たれ、不足しているキーだけが補われる。
func TestInstallKeepsUserFiles(t *testing.T) {
	t.Parallel()

	files := newFakeFileStore()
	const myLayout = "layout { 利用者の版 }\n"
	files.files[conductorPath("layouts/multi.kdl")] = myLayout
	files.files[conductorPath("config.json")] =
		`{"search_dirs":["~/mywork"],"agents":{"codex":{"command":"codex"}}}` + "\n"
	files.files[conductorPath("daily/sess/2026-08-01.jsonl")] = `{"completed_at":"2026-08-01"}` + "\n"

	runInstall(t, files, allAvailable)

	if got := files.files[conductorPath("layouts/multi.kdl")]; got != myLayout {
		t.Errorf("レイアウトを上書きした: %q", got)
	}
	config := files.files[conductorPath("config.json")]
	if !strings.Contains(config, "~/mywork") {
		t.Errorf("設定を書き換えた: %s", config)
	}
	if !strings.Contains(config, "detection") {
		t.Errorf("不足していた項目が補われていない: %s", config)
	}
	if _, ok := files.files[conductorPath("daily/sess/2026-08-01.jsonl")]; !ok {
		t.Error("作業ログを消した")
	}
}

// TestInstallRefreshesShippedFiles は配布物そのものは更新することを確かめる。
//
// **init.zsh の更新が特に効く。** 旧版は消したばかりの scripts/ を呼び、
// さらに mdev の関数を定義してバイナリを横取りする。
func TestInstallRefreshesShippedFiles(t *testing.T) {
	t.Parallel()

	files := newFakeFileStore()
	files.files[conductorPath("init.zsh")] = "mdev() { bash \"$CONDUCTOR_HOME/scripts/update.sh\"; }\n"
	files.files[conductorPath("config.default.json")] = `{"agents":{}}` + "\n"

	runInstall(t, files, allAvailable)

	if got := files.files[conductorPath("init.zsh")]; !strings.Contains(got, "init zsh") {
		t.Errorf("init.zsh が更新されていない: %q", got)
	}
	if got := files.files[conductorPath("config.default.json")]; !strings.Contains(got, "detection") {
		t.Errorf("既定値が更新されていない: %q", got)
	}
}

// TestInstallMigratesShellEnvironment は既存 Shell 環境からの移行を確かめる。
func TestInstallMigratesShellEnvironment(t *testing.T) {
	t.Parallel()

	files := newFakeFileStore()
	files.files[conductorPath("scripts/pending-notify.sh")] = "#!/bin/bash\n"
	files.files[conductorPath("scripts/fetch-news.sh")] = "#!/bin/bash\n"
	files.files[conductorPath("FLAVOR")] = "go\n"
	// 配られたままの雛形(編集されていれば残る。別のテストで確かめている)。
	if template, ok := testAssets.Asset("hooks.json"); ok {
		files.files[conductorPath("hooks.json")] = string(template)
	}
	files.files[conductorPath("layouts/multi.kdl")] =
		`args "-c" "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/dashboard-loop.sh"` + "\n"
	files.files[testInstallPaths.Settings] = `{"permissions":{"allow":["Bash(ls:*)"]},"hooks":{"Stop":[` +
		`{"matcher":"","hooks":[{"type":"command",` +
		`"command":"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh"}]}]}}` + "\n"
	files.files[testInstallPaths.CodexConfig] =
		`notify = ["bash", "/home/dev/.claude-conductor/scripts/codex-notify.sh"]` + "\n"

	out := runInstall(t, files, allAvailable)

	if files.Exists(conductorPath("scripts")) {
		t.Error("scripts/ が残っている")
	}
	// 消す前に一覧を出す(何が失われたか後から辿れるように)。
	for _, name := range []string{"pending-notify.sh", "fetch-news.sh"} {
		if !strings.Contains(out, name) {
			t.Errorf("撤去した %q が出力に無い:\n%s", name, out)
		}
	}
	for _, name := range []string{"FLAVOR", "hooks.json"} {
		if files.Exists(conductorPath(name)) {
			t.Errorf("%s が残っている", name)
		}
	}
	for _, path := range []string{
		conductorPath("layouts/multi.kdl"),
		testInstallPaths.Settings,
		testInstallPaths.CodexConfig,
	} {
		if strings.Contains(files.files[path], "/scripts/") {
			t.Errorf("%s に Shell 版の呼び出しが残っている:\n%s", path, files.files[path])
		}
	}
	// 利用者の設定は残す。
	if !strings.Contains(files.files[testInstallPaths.Settings], "Bash(ls:*)") {
		t.Errorf("settings.json の他の設定が消えた:\n%s", files.files[testInstallPaths.Settings])
	}
}

// TestInstallMigratesCodexWithoutCodexInstalled は codex が入っていなくても
// 既存の参照は移行することを確かめる。
//
// scripts/ を消した後に Shell 版への参照が残ると、そこだけ壊れたまま
// 気づけない。
func TestInstallMigratesCodexWithoutCodexInstalled(t *testing.T) {
	t.Parallel()

	files := newFakeFileStore()
	files.files[testInstallPaths.CodexConfig] =
		`notify = ["bash", "/home/dev/.claude-conductor/scripts/codex-notify.sh"]` + "\n"

	available := map[string]bool{domain.ZellijCommand: true, domain.ClaudeCommand: true}
	runInstall(t, files, available)

	if strings.Contains(files.files[testInstallPaths.CodexConfig], domain.CodexNotifyMarker) {
		t.Errorf("Shell 版の参照が残っている: %q", files.files[testInstallPaths.CodexConfig])
	}
}

// TestInstallSkipsCodexWhenAbsent は codex が無く設定にも出てこないときに
// 何もしないことを確かめる(任意の連携である)。
func TestInstallSkipsCodexWhenAbsent(t *testing.T) {
	t.Parallel()

	files := newFakeFileStore()
	available := map[string]bool{domain.ZellijCommand: true, domain.ClaudeCommand: true}
	runInstall(t, files, available)

	if _, ok := files.files[testInstallPaths.CodexConfig]; ok {
		t.Errorf("codex の設定を作った: %q", files.files[testInstallPaths.CodexConfig])
	}
}

// TestInstallLeavesForeignCodexNotify は他ツールの notify を触らず案内を
// 出すことを確かめる。
func TestInstallLeavesForeignCodexNotify(t *testing.T) {
	t.Parallel()

	files := newFakeFileStore()
	const foreign = `notify = ["/opt/other/notify"]` + "\n"
	files.files[testInstallPaths.CodexConfig] = foreign

	out := runInstall(t, files, allAvailable)

	if files.files[testInstallPaths.CodexConfig] != foreign {
		t.Errorf("書き換えた: %q", files.files[testInstallPaths.CodexConfig])
	}
	if !strings.Contains(out, "codex notify") {
		t.Errorf("案内が出ていない:\n%s", out)
	}
}

// TestInstallRequiresDependencies は依存が足りないときに止まることを確かめる。
func TestInstallRequiresDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		available map[string]bool
		want      string
	}{
		{
			name:      "zellij が無い",
			available: map[string]bool{domain.ClaudeCommand: true},
			want:      domain.ZellijCommand,
		},
		{
			name:      "エージェントが 1 つも無い",
			available: map[string]bool{domain.ZellijCommand: true},
			want:      "エージェント",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := newFakeFileStore()
			var out bytes.Buffer
			err := newTestInstaller(files, tt.available).Install(&out)
			if err == nil {
				t.Fatal("エラーを返すはず")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("説明 = %q, want %q を含む", err, tt.want)
			}
			// 止めるときは 1 つも書かない。
			if len(files.writes) != 0 {
				t.Errorf("書き込んだ: %v", files.writes)
			}
		})
	}
}

// TestInstallReportsZshrc は .zshrc の状況を伝え、書き換えないことを確かめる。
func TestInstallReportsZshrc(t *testing.T) {
	t.Parallel()

	t.Run("設定済み", func(t *testing.T) {
		t.Parallel()
		files := newFakeFileStore()
		files.files[testInstallPaths.Zshrc] = domain.ZshrcSourceLine + "\n"
		out := runInstall(t, files, allAvailable)

		if !strings.Contains(out, "設定済み") {
			t.Errorf("出力 =\n%s", out)
		}
		if files.files[testInstallPaths.Zshrc] != domain.ZshrcSourceLine+"\n" {
			t.Error(".zshrc を書き換えた")
		}
	})

	t.Run("未設定なら案内だけ出す", func(t *testing.T) {
		t.Parallel()
		files := newFakeFileStore()
		out := runInstall(t, files, allAvailable)

		if !strings.Contains(out, domain.ZshrcSourceLine) {
			t.Errorf("案内が出ていない:\n%s", out)
		}
		if _, ok := files.files[testInstallPaths.Zshrc]; ok {
			t.Error(".zshrc を作った")
		}
	})
}

// TestInstallContinuesAfterFailure は途中の失敗で残りを飛ばさないことを
// 確かめる。
//
// 手順は互いに独立している。1 つ書けなかったからといって hooks の切り替えを
// 飛ばすと、直せる部分まで直らないまま終わる。
func TestInstallContinuesAfterFailure(t *testing.T) {
	t.Parallel()

	files := newFakeFileStore()
	files.writeErr = errWrite

	var out bytes.Buffer
	err := newTestInstaller(files, allAvailable).Install(&out)
	if err == nil {
		t.Fatal("エラーを返すはず")
	}
	// 依存チェックは通っているので、最後の案内まで出し切る。
	if !strings.Contains(out.String(), domain.ZshrcSourceLine) {
		t.Errorf("最後まで進んでいない:\n%s", out.String())
	}
}

// TestInstallRefusesToRemoveScriptsFromDangerousHome は CONDUCTOR_HOME が
// おかしいときに scripts/ を消しに行かないことを確かめる。
//
// 消す相手は CONDUCTOR_HOME 配下だが、その CONDUCTOR_HOME 自体がおかしければ
// `/scripts` のような場所を消しに行く。
func TestInstallRefusesToRemoveScriptsFromDangerousHome(t *testing.T) {
	t.Parallel()

	files := newFakeFileStore()
	paths := testInstallPaths
	paths.ConductorHome = "/"
	files.files["/scripts/pending-notify.sh"] = "#!/bin/bash\n"

	var out bytes.Buffer
	err := (&app.Installer{
		Paths:    paths,
		Files:    files,
		Assets:   testAssets,
		Commands: fakeCommandChecker{available: allAvailable},
		Version:  "v1.2.3",
		GOOS:     "darwin",
	}).Install(&out)

	if err == nil {
		t.Fatal("エラーを返すはず")
	}
	for _, removed := range files.removed {
		if removed == "/scripts" {
			t.Fatal("ルート直下の scripts を消した")
		}
	}
	if _, ok := files.files["/scripts/pending-notify.sh"]; !ok {
		t.Error("消してはいけないものが消えた")
	}
}

// fakeSettingsBackup は settings.json の退避の代役である。
type fakeSettingsBackup struct {
	saved [][]byte
	err   error
}

func (b *fakeSettingsBackup) Backup(data []byte) (string, error) {
	if b.err != nil {
		return "", b.err
	}
	b.saved = append(b.saved, data)
	return "/home/dev/.claude/settings.json.mdev-backup-20260814T000000Z", nil
}

// TestInstallBacksUpSettingsBeforeWriting は settings.json を書き換える前に
// 退避することを確かめる。
//
// hooks の書き換えは利用者の設定ファイルへの破壊的な操作である。
// `mdev hooks switch` は退避してから書いていたので、その仕事を引き取った
// install も同じようにする。
func TestInstallBacksUpSettingsBeforeWriting(t *testing.T) {
	t.Parallel()

	const existing = `{"permissions":{"allow":[]},"hooks":{"Stop":[{"matcher":"","hooks":[` +
		`{"type":"command","command":"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh"}]}]}}` + "\n"

	files := newFakeFileStore()
	files.files[testInstallPaths.Settings] = existing
	backup := &fakeSettingsBackup{}

	installer := newTestInstaller(files, allAvailable)
	installer.Backup = backup

	var out bytes.Buffer
	if err := installer.Install(&out); err != nil {
		t.Fatalf("Install = %v", err)
	}

	if len(backup.saved) != 1 {
		t.Fatalf("退避 = %d 回, want 1", len(backup.saved))
	}
	// 退避されるのは **書き換える前** の中身である。
	if string(backup.saved[0]) != existing {
		t.Errorf("退避した中身が書き換え後になっている:\n%s", backup.saved[0])
	}
	if !strings.Contains(out.String(), "退避しました") {
		t.Errorf("退避したことを伝えていない:\n%s", out.String())
	}
}

// TestInstallSkipsBackupWhenNothingChanges は書き換えないなら退避もしない
// ことを確かめる。install は繰り返し実行されるので、毎回退避すると
// バックアップが際限なく増える。
func TestInstallSkipsBackupWhenNothingChanges(t *testing.T) {
	t.Parallel()

	files := newFakeFileStore()
	backup := &fakeSettingsBackup{}
	installer := newTestInstaller(files, allAvailable)
	installer.Backup = backup

	var out bytes.Buffer
	if err := installer.Install(&out); err != nil {
		t.Fatalf("1 回目 = %v", err)
	}
	backup.saved = nil

	if err := installer.Install(&out); err != nil {
		t.Fatalf("2 回目 = %v", err)
	}
	if len(backup.saved) != 0 {
		t.Errorf("2 回目に退避した: %d 回", len(backup.saved))
	}
}

// TestInstallStopsWhenBackupFails は退避に失敗したら書き換えないことを
// 確かめる。
//
// 戻せない状態で利用者の設定を書き換えるより、hooks が古いまま残るほうが
// 害が小さい。
func TestInstallStopsWhenBackupFails(t *testing.T) {
	t.Parallel()

	const existing = `{"hooks":{"Stop":[{"matcher":"","hooks":[` +
		`{"type":"command","command":"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh"}]}]}}` + "\n"

	files := newFakeFileStore()
	files.files[testInstallPaths.Settings] = existing
	installer := newTestInstaller(files, allAvailable)
	installer.Backup = &fakeSettingsBackup{err: errWrite}

	var out bytes.Buffer
	if err := installer.Install(&out); err == nil {
		t.Fatal("エラーを返すはず")
	}
	if files.files[testInstallPaths.Settings] != existing {
		t.Errorf("退避に失敗したのに書き換えた:\n%s", files.files[testInstallPaths.Settings])
	}
}

// TestInstallRemovesUntouchedHooksTemplate は配られたままの hooks.json を
// 撤去することを確かめる。
//
// install が hooks を組み立てるときに見るのは埋め込みのほうで、ディスクの
// 写しは誰も読まない。置いてあると「これを編集すれば hooks が変わる」と
// 読めてしまう。
func TestInstallRemovesUntouchedHooksTemplate(t *testing.T) {
	t.Parallel()

	template, ok := testAssets.Asset("hooks.json")
	if !ok {
		t.Fatal("テスト用の資産に hooks.json が無い")
	}
	files := newFakeFileStore()
	files.files[conductorPath("hooks.json")] = string(template)

	runInstall(t, files, allAvailable)

	if _, ok := files.files[conductorPath("hooks.json")]; ok {
		t.Errorf("撤去されていない: %q", files.files[conductorPath("hooks.json")])
	}
}

// TestInstallKeepsEditedHooksTemplate は編集された hooks.json を残すことを
// 確かめる。
//
// **読まれないファイルであっても、利用者が手を入れたものを黙って捨ててよい
// 理由にはならない。** 消してよいかどうかは、中身を見た本人が決める。
func TestInstallKeepsEditedHooksTemplate(t *testing.T) {
	t.Parallel()

	const edited = `{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"my-own"}]}]}` + "\n"
	files := newFakeFileStore()
	files.files[conductorPath("hooks.json")] = edited

	out := runInstall(t, files, allAvailable)

	if files.files[conductorPath("hooks.json")] != edited {
		t.Errorf("編集されたものを触った: %q", files.files[conductorPath("hooks.json")])
	}
	for _, removed := range files.removed {
		if removed == conductorPath("hooks.json") {
			t.Fatal("編集されたものを削除した")
		}
	}
	if !strings.Contains(out, "編集されているため残しました") {
		t.Errorf("残した理由が出ていない:\n%s", out)
	}
	// 読まれないことも伝える(残す判断をするのに要る)。
	if !strings.Contains(out, "読みません") {
		t.Errorf("読まれないことを伝えていない:\n%s", out)
	}
}

// TestInstallRemovesFlavorRegardlessOfContent は FLAVOR を中身に関わらず
// 消すことを確かめる。
//
// hooks.json と違い、これは印として置いてあるだけで、書き換えて別の意味を
// 持たせられるものではない。仕組みごと廃止されている。
func TestInstallRemovesFlavorRegardlessOfContent(t *testing.T) {
	t.Parallel()

	for _, content := range []string{"go\n", "shell\n", "利用者が書いた何か\n"} {
		t.Run(content, func(t *testing.T) {
			t.Parallel()
			files := newFakeFileStore()
			files.files[conductorPath("FLAVOR")] = content

			runInstall(t, files, allAvailable)

			if files.Exists(conductorPath("FLAVOR")) {
				t.Error("FLAVOR が残っている")
			}
		})
	}
}
