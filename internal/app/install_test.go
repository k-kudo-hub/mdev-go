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
		"config.default.json", "config.json", "hooks.json", "init.zsh",
		"layouts/multi.kdl", "layouts/dev.kdl", "VERSION", "REPO_URL",
	} {
		if _, ok := files.files[conductorPath(rel)]; !ok {
			t.Errorf("%s が配置されていない", rel)
		}
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
	if files.Exists(conductorPath("FLAVOR")) {
		t.Error("FLAVOR が残っている")
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

// codexHooksPath はテストで使う codex の hooks コピーの場所である。
var codexHooksPath = testInstallPaths.CodexHooksPath()

// codexHooksCopy は Codex アプリが写した形の hooks.json を返す。
func codexHooksCopy(commands ...string) string {
	hooks := make([]string, 0, len(commands))
	for _, command := range commands {
		hooks = append(hooks, `{"type":"command","command":"`+command+`"}`)
	}
	return `{"Stop":[{"matcher":"","hooks":[` + strings.Join(hooks, ",") + `]}]}` + "\n"
}

// removedScriptHook は 6-3 で撤去済みのスクリプトを指す hook である。
const removedScriptHook = "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh"

// workingMdevHook は Go 版を指す hook である(exit 0 で動く)。
const workingMdevHook = "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify"

// TestInstallWarnsAboutBrokenCodexHooks は壊れた hooks を知らせることを
// 確かめる。
//
// codex 0.147 は Claude Code 互換の hooks エンジンを内蔵しており、Codex アプリの
// import が settings.json の hooks を CODEX_HOME/hooks.json へコピーする。
// これを見落としたまま scripts/ を消したため、写された hook がすべて exit 127 に
// なり codex の会話にエラーが出続けた。
//
// **知らせるだけで直さない。** このファイルは利用者が意図して置いた設定かも
// しれず、消すか直すかを決められるのは利用者だけである。
func TestInstallWarnsAboutBrokenCodexHooks(t *testing.T) {
	t.Parallel()

	content := codexHooksCopy(removedScriptHook)
	files := newFakeFileStore()
	files.files[codexHooksPath] = content

	out := runInstall(t, files, allAvailable)

	// **1 バイトも触らない。**
	if files.files[codexHooksPath] != content {
		t.Errorf("書き換えた: %q", files.files[codexHooksPath])
	}
	for _, removed := range files.removed {
		if removed == codexHooksPath {
			t.Fatal("削除した")
		}
	}
	// 事実と選択肢の両方を出す。
	for _, want := range []string{"127", "rm ", "bin/mdev hook", "再信頼の確認"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出ていない:\n%s", want, out)
		}
	}
}

// TestInstallSilentOnWorkingCodexHooks は動いている hooks で黙ることを
// 確かめる。
//
// Go 版は exit 0 で動く。動いているものに警告を出すと、利用者は毎回の
// install で意味の無い赤を見ることになる。
func TestInstallSilentOnWorkingCodexHooks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{name: "Go 版だけ", content: codexHooksCopy(workingMdevHook)},
		{name: "conductor と無関係", content: codexHooksCopy("my-own-linter --fix")},
		{name: "読めない形", content: `{"Stop":` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := newFakeFileStore()
			files.files[codexHooksPath] = tt.content

			out := runInstall(t, files, allAvailable)

			if files.files[codexHooksPath] != tt.content {
				t.Errorf("触った: %q", files.files[codexHooksPath])
			}
			if strings.Contains(out, "hooks") && strings.Contains(out, "127") {
				t.Errorf("要らない警告を出した:\n%s", out)
			}
		})
	}
}

// TestInstallWarnsAboutMixedCodexHooks は壊れたものが 1 つでもあれば知らせる
// ことを確かめる。動いているものが混ざっていても、壊れている事実は変わらない。
func TestInstallWarnsAboutMixedCodexHooks(t *testing.T) {
	t.Parallel()

	content := codexHooksCopy(workingMdevHook, removedScriptHook)
	files := newFakeFileStore()
	files.files[codexHooksPath] = content

	out := runInstall(t, files, allAvailable)

	if files.files[codexHooksPath] != content {
		t.Errorf("触った: %q", files.files[codexHooksPath])
	}
	if !strings.Contains(out, "127") {
		t.Errorf("警告が出ていない:\n%s", out)
	}
}

// TestInstallWithoutCodexHooksCopy は無ければ何も言わないことを確かめる。
func TestInstallWithoutCodexHooksCopy(t *testing.T) {
	t.Parallel()

	files := newFakeFileStore()
	out := runInstall(t, files, allAvailable)

	if _, ok := files.files[codexHooksPath]; ok {
		t.Error("hooks のコピーを作った")
	}
	if strings.Contains(out, "hooks.json の hooks") {
		t.Errorf("要らない報告を出した:\n%s", out)
	}
}

// TestInstallCodexHooksWarningIsIdempotent は 2 回目も同じ結果になることを
// 確かめる。
//
// 直さないので警告は繰り返し出る(それが正しい)。**ファイルが変わらないこと**
// と、警告が増えも減りもしないことを見る。
func TestInstallCodexHooksWarningIsIdempotent(t *testing.T) {
	t.Parallel()

	content := codexHooksCopy(removedScriptHook)
	files := newFakeFileStore()
	files.files[codexHooksPath] = content

	first := runInstall(t, files, allAvailable)
	files.writes, files.removed = nil, nil
	second := runInstall(t, files, allAvailable)

	if files.files[codexHooksPath] != content {
		t.Errorf("2 回目が触った: %q", files.files[codexHooksPath])
	}
	if len(files.removed) != 0 {
		t.Errorf("2 回目が削除した: %v", files.removed)
	}
	if strings.Count(first, "127") != strings.Count(second, "127") {
		t.Errorf("警告の回数が変わった:\n1 回目:\n%s\n2 回目:\n%s", first, second)
	}
	if strings.Count(second, "127") != 1 {
		t.Errorf("警告が %d 回, want 1:\n%s", strings.Count(second, "127"), second)
	}
}
