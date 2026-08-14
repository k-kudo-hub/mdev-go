package app_test

import (
	"errors"
	"path"
	"sort"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

var (
	_ app.FileStore      = (*fakeFileStore)(nil)
	_ app.AssetReader    = fakeAssetReader{}
	_ app.CommandChecker = fakeCommandChecker{}
)

// fakeFileStore はメモリ上のファイル置き場である。
//
// install が触る先は CONDUCTOR_HOME・settings.json・config.toml と散らばる
// ので、すべての書き込みをここ 1 か所で観測する。
type fakeFileStore struct {
	// files はパスと中身。ディレクトリは持たず、prefix で表す。
	files map[string]string
	// writes は書き込まれたパス(順番どおり)。
	writes []string
	// removed は消されたパス。
	removed []string
	// writeErr は書き込みで返すエラー。
	writeErr error
}

func newFakeFileStore() *fakeFileStore {
	return &fakeFileStore{files: map[string]string{}}
}

func (f *fakeFileStore) Read(p string) ([]byte, bool, error) {
	body, ok := f.files[p]
	if !ok {
		return nil, false, nil
	}
	return []byte(body), true, nil
}

func (f *fakeFileStore) Write(p string, data []byte) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.files[p] = string(data)
	f.writes = append(f.writes, p)
	return nil
}

func (f *fakeFileStore) WriteExecutable(p string, data []byte) error { return f.Write(p, data) }

func (f *fakeFileStore) Remove(p string) error {
	f.removed = append(f.removed, p)
	for name := range f.files {
		if name == p || strings.HasPrefix(name, p+"/") {
			delete(f.files, name)
		}
	}
	return nil
}

func (f *fakeFileStore) ListDir(p string) ([]string, error) {
	seen := map[string]struct{}{}
	for name := range f.files {
		if !strings.HasPrefix(name, p+"/") {
			continue
		}
		rest := strings.TrimPrefix(name, p+"/")
		seen[strings.SplitN(rest, "/", 2)[0]] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func (f *fakeFileStore) Exists(p string) bool {
	if _, ok := f.files[p]; ok {
		return true
	}
	for name := range f.files {
		if strings.HasPrefix(name, p+"/") {
			return true
		}
	}
	return false
}

// fakeAssetReader は同梱資産の代役である。
type fakeAssetReader struct {
	files map[string]string
}

func (a fakeAssetReader) Names() []string {
	out := make([]string, 0, len(a.files))
	for name := range a.files {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (a fakeAssetReader) Asset(name string) ([]byte, bool) {
	body, ok := a.files[name]
	if !ok {
		return nil, false
	}
	return []byte(body), true
}

// fakeCommandChecker は PATH の代役である。
type fakeCommandChecker struct {
	available map[string]bool
}

func (c fakeCommandChecker) Available(name string) bool { return c.available[name] }

// testAssets は install が配る最小の資産一式である。
//
// 中身は本物と同じ形にする(hooks は Shell 版を指す雛形、レイアウトは
// mdev を指す)。実物と同じ経路を通ることを確かめるのが目的で、
// 全文である必要は無い。
var testAssets = fakeAssetReader{files: map[string]string{
	"config.default.json": `{"agents":{"codex":{"command":"codex","detection":"screen","patterns":{"blocked":["^b$"]}}}}` + "\n",
	"hooks.json": `{"Stop":[{"matcher":"","hooks":[{"type":"command",` +
		`"command":"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh"}]}]}` + "\n",
	"init.zsh": "eval \"$(\"$CONDUCTOR_HOME/bin/mdev\" init zsh)\"\n",
	// 本物と同じ形にする。テスト用のレイアウトは CONDUCTOR_HOME 経由の
	// 呼び出しを絶対パスへ差し替えるので、その形でなければ経路を通らない。
	"layouts/multi.kdl": "layout { pane { args \"-c\" \"\\\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\\\" pane dashboard\" } }\n",
	"layouts/dev.kdl":   "layout { pane { args \"-c\" \"\\\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\\\" agent launch\" } }\n",
}}

// testInstallPaths はテストで使う設置先である。
var testInstallPaths = domain.InstallPaths{
	Home:          "/home/dev",
	ConductorHome: "/home/dev/.claude-conductor",
	Settings:      "/home/dev/.claude/settings.json",
	CodexConfig:   "/home/dev/.codex/config.toml",
	Zshrc:         "/home/dev/.zshrc",
}

// newTestInstaller は依存を組み合わせた Installer を返す。
func newTestInstaller(files *fakeFileStore, available map[string]bool) *app.Installer {
	return &app.Installer{
		Paths:    testInstallPaths,
		Files:    files,
		Assets:   testAssets,
		Commands: fakeCommandChecker{available: available},
		Version:  "v1.2.3",
		GOOS:     "darwin",
	}
}

// conductorPath は CONDUCTOR_HOME 配下の絶対パスを返す。
func conductorPath(rel string) string {
	return path.Join(testInstallPaths.ConductorHome, rel)
}

// errWrite は書き込みの失敗を表す。
var errWrite = errors.New("書けない")
