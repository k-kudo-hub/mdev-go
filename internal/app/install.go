package app

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// 資産の配り方。
//
//   - layouts/*.kdl と hooks.json は **不在のときだけ書く**。利用者が手を
//     入れたものを install のたびに消さないための退避路である(ADR D4 の
//     「カスタマイズの退避路」)。既にあるものが Shell を呼んでいる場合は、
//     上書きせず migrateLayouts が壊れる箇所だけを書き換える
//   - config.default.json と init.zsh は中身が違えば書き直す。どちらも
//     配布物そのもので、利用者が手を入れる先ではない(設定は config.json、
//     シェルの追加設定は .zshrc)。**init.zsh を更新しないのが特に危険で**、
//     旧版は消したばかりの scripts/ を呼ぶうえに mdev の関数を定義して
//     バイナリを横取りする
//
// どれも「同じ内容なら書かない」ので、2 回目の install はファイルを
// 1 つも触らない。
var installOnlyIfAbsent = []string{
	"hooks.json",
	"layouts/multi.kdl",
	"layouts/dev.kdl",
}

// installAlwaysRefresh は中身が違えば書き直す資産である。
var installAlwaysRefresh = []string{"config.default.json", "init.zsh"}

// configDefaultName は既定値のファイル名である。
const configDefaultName = "config.default.json"

// configName は利用者の設定ファイル名である。
const configName = "config.json"

// versionFileName / repoURLFileName は状態ファイルである。
const (
	versionFileName = "VERSION"
	repoURLFileName = "REPO_URL"
)

// Installer は `mdev install` のユースケースである(現行 install.sh 相当)。
//
// 各手順は冪等である。2 回続けて実行しても同じ状態になり、2 回目は
// ファイルを 1 つも書き換えない。**利用者のデータ(config.json の中身・
// daily・tasks・news・pending・registry)は消さない。**
type Installer struct {
	Paths    domain.InstallPaths
	Files    FileStore
	Assets   AssetReader
	Commands CommandChecker
	// Version は書き込む mdev の版(ADR D3-2 の版の単一化)。
	Version string
	// GOOS はテストで差し替える実行環境。空なら runtime.GOOS。
	GOOS string
}

// Install は設置と移行を行い、経過を out へ書く。
//
// 出力の書き込み失敗は無視する。設置そのものは進んでおり、経過を書けない
// ことを理由に途中で止めるほうが害が大きい(追加の報告先も無い)。
func (i *Installer) Install(out io.Writer) error {
	_, _ = fmt.Fprintln(out, "mdev をインストールします")
	_, _ = fmt.Fprintln(out)

	if err := i.checkDependencies(out); err != nil {
		return err
	}

	var errs []error
	for _, step := range []func(io.Writer) error{
		i.placeAssets,
		i.mergeConfig,
		i.writeState,
		i.configureHooks,
		i.configureCodex,
		i.migrateLayouts,
		i.removeShellScripts,
		i.removeFlavor,
	} {
		if err := step(out); err != nil {
			errs = append(errs, err)
		}
	}
	i.reportShell(out)

	if err := errors.Join(errs...); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "インストールが終わりました。シェルを開き直すか `source ~/.zshrc` を実行してください。")
	return nil
}

// checkDependencies は依存コマンドを調べる。足りなければ止める。
func (i *Installer) checkDependencies(out io.Writer) error {
	report := domain.CheckDependencies(i.Commands.Available, i.goos() == "darwin")
	if !report.OK() {
		_, _ = fmt.Fprintln(out, "  ✗", report.Problem())
		return errors.New(report.Problem())
	}
	_, _ = fmt.Fprintf(out, "  ✓ 依存: %s / エージェント: %s\n",
		domain.ZellijCommand, strings.Join(report.Agents, ", "))
	for _, name := range report.Optional {
		_, _ = fmt.Fprintf(out, "  ✓ 任意: %s\n", name)
	}
	return nil
}

// goos は実行環境を返す。
func (i *Installer) goos() string {
	if i.GOOS != "" {
		return i.GOOS
	}
	return runtime.GOOS
}

// placeAssets は同梱資産を配る。
func (i *Installer) placeAssets(out io.Writer) error {
	var errs []error
	var placed []string

	for _, name := range installOnlyIfAbsent {
		path := i.Paths.ConductorPath(name)
		if i.Files.Exists(path) {
			continue
		}
		body, ok := i.Assets.Asset(name)
		if !ok {
			errs = append(errs, fmt.Errorf("同梱されていない資産です: %s", name))
			continue
		}
		if err := i.Files.Write(path, body); err != nil {
			errs = append(errs, err)
			continue
		}
		placed = append(placed, name)
	}

	for _, name := range installAlwaysRefresh {
		changed, err := i.refreshAsset(name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if changed {
			placed = append(placed, name)
		}
	}

	if len(placed) > 0 {
		sort.Strings(placed)
		_, _ = fmt.Fprintf(out, "  ✓ 資産を配置: %s\n", strings.Join(placed, ", "))
	}
	return errors.Join(errs...)
}

// refreshAsset は資産を同梱の中身へ揃える。既に同じなら何もしない。
func (i *Installer) refreshAsset(name string) (bool, error) {
	body, ok := i.Assets.Asset(name)
	if !ok {
		return false, fmt.Errorf("同梱されていない資産です: %s", name)
	}
	path := i.Paths.ConductorPath(name)
	current, found, err := i.Files.Read(path)
	if err != nil {
		return false, err
	}
	if found && string(current) == string(body) {
		return false, nil
	}
	return true, i.Files.Write(path, body)
}

// mergeConfig は config.json を用意する。
//
// 無ければ既定値をそのまま置き、あれば足りないエージェント項目だけを補う。
// **利用者が書いた値は 1 つも書き換えない。**
func (i *Installer) mergeConfig(out io.Writer) error {
	defaults, ok := i.Assets.Asset(configDefaultName)
	if !ok {
		return fmt.Errorf("同梱されていない資産です: %s", configDefaultName)
	}
	path := i.Paths.ConductorPath(configName)
	current, found, err := i.Files.Read(path)
	if err != nil {
		return err
	}
	if !found {
		if err := i.Files.Write(path, defaults); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(out, "  ✓ config.json を作成しました")
		return nil
	}

	merged, additions, err := domain.MergeAgentDefaults(current, defaults)
	if err != nil {
		// 現行版も jq が落ちたらマージを飛ばし、既存の設定をそのまま残して
		// 警告だけ出していた。設定は利用者のものなので勝手に作り直さない。
		_, _ = fmt.Fprintf(out, "  ! config.json のマージを飛ばしました(既存の設定はそのままです): %v\n", err)
		return nil
	}
	if len(additions) == 0 {
		return nil
	}
	if err := i.Files.Write(path, merged); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "  ✓ config.json に不足していた項目を補いました: %s\n",
		domain.RenderAgentDefaultAdditions(additions))
	return nil
}

// writeState は VERSION と REPO_URL を書く。
//
// VERSION には **バイナリ自身の版**を書く(ADR D3-2)。以前は conductor の
// タグを書いており、バイナリと資産で版が二重に存在していた。REPO_URL は
// mdev-go を指す(ADR D8 の移行の要)。
func (i *Installer) writeState(out io.Writer) error {
	var errs []error
	for _, f := range []struct {
		name  string
		value string
	}{
		{name: versionFileName, value: i.Version},
		{name: repoURLFileName, value: domain.MdevRepoURL},
	} {
		path := i.Paths.ConductorPath(f.name)
		want := f.value + "\n"
		current, found, err := i.Files.Read(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if found && string(current) == want {
			continue
		}
		if err := i.Files.Write(path, []byte(want)); err != nil {
			errs = append(errs, err)
			continue
		}
		_, _ = fmt.Fprintf(out, "  ✓ %s を %s にしました\n", f.name, f.value)
	}
	return errors.Join(errs...)
}

// configureHooks は settings.json の hooks を整える。
func (i *Installer) configureHooks(out io.Writer) error {
	template, ok := i.Assets.Asset("hooks.json")
	if !ok {
		return errors.New("同梱されていない資産です: hooks.json")
	}
	current, found, err := i.Files.Read(i.Paths.Settings)
	if err != nil {
		return err
	}
	if !found {
		created, err := domain.NewHookSettings(template)
		if err != nil {
			return err
		}
		if err := i.Files.Write(i.Paths.Settings, created); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "  ✓ %s を作成しました\n", i.Paths.Settings)
		return nil
	}

	result, err := domain.InstallHooks(current, template)
	if err != nil {
		return fmt.Errorf("hooks を設定できません: %w", err)
	}
	for _, remaining := range result.RemainingScripts {
		_, _ = fmt.Fprintf(out, "  ! %s の hook が Shell 版のまま残りました: %s\n",
			remaining.Event, remaining.Command)
	}
	if !result.Changed() {
		return nil
	}
	if err := i.Files.Write(i.Paths.Settings, result.Settings); err != nil {
		return err
	}
	if len(result.AddedEvents) > 0 {
		_, _ = fmt.Fprintf(out, "  ✓ hooks を追加: %s\n", strings.Join(result.AddedEvents, ", "))
	}
	if len(result.Changes) > 0 {
		_, _ = fmt.Fprintf(out, "  ✓ hooks を mdev へ切り替えました(%d 件)\n", len(result.Changes))
	}
	return nil
}

// configureCodex は codex の notify を mdev へ向ける。
//
// codex が入っておらず、設定にも conductor が出てこなければ何もしない
// (任意の連携である)。他ツールが notify を使っている場合も触らず、案内だけ出す。
func (i *Installer) configureCodex(out io.Writer) error {
	current, found, err := i.Files.Read(i.Paths.CodexConfig)
	if err != nil {
		return err
	}
	// **既に conductor を指しているなら codex の有無に関わらず書き換える。**
	// scripts/ を消した後に Shell 版への参照が残ると、そこだけ壊れたまま
	// 気づけない。新しく足すときだけ codex が入っていることを求める。
	migrating := found && strings.Contains(string(current), domain.CodexNotifyMarker)
	if !migrating && !i.Commands.Available(domain.CodexCommand) {
		return nil
	}

	mdevPath := i.Paths.MdevBinaryPath()
	rewritten, status := domain.RewriteCodexNotify(string(current), mdevPath)
	switch status {
	case domain.CodexNotifyUnchanged:
		return nil
	case domain.CodexNotifyForeign:
		_, _ = fmt.Fprintf(out, "  ? %s は別のツールが notify を使っています(触っていません)\n",
			i.Paths.CodexConfig)
		_, _ = fmt.Fprintf(out, "    codex のタスクを画面へ出すには、その notify から次を呼んでください:\n")
		_, _ = fmt.Fprintf(out, "      %s codex notify '<payload-json>'\n", mdevPath)
		return nil
	case domain.CodexNotifyAdded, domain.CodexNotifyMigrated:
		if err := i.Files.Write(i.Paths.CodexConfig, []byte(rewritten)); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "  ✓ codex の notify を mdev へ向けました(%s)\n", i.Paths.CodexConfig)
	}
	return nil
}

// migrateLayouts はレイアウトに残った Shell 呼び出しを書き換える。
//
// 資産の配置は「不在時のみ書く」ので、既にあるレイアウトの中身はここでしか
// 直らない。scripts/ を消す前に書き換えておかないと、そのペインが起動
// しなくなる。
func (i *Installer) migrateLayouts(out io.Writer) error {
	var errs []error
	for _, name := range []string{"layouts/multi.kdl", "layouts/dev.kdl"} {
		path := i.Paths.ConductorPath(name)
		current, found, err := i.Files.Read(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !found {
			continue
		}
		migrated, changes := domain.MigrateLayout(string(current))
		for _, remaining := range domain.RemainingLayoutScripts(migrated) {
			_, _ = fmt.Fprintf(out, "  ! %s に Shell スクリプトの呼び出しが残りました: %s\n", name, remaining)
		}
		if len(changes) == 0 {
			continue
		}
		if err := i.Files.Write(path, []byte(migrated)); err != nil {
			errs = append(errs, err)
			continue
		}
		_, _ = fmt.Fprintf(out, "  ✓ %s の呼び出しを mdev へ書き換えました(%d 件)\n", name, len(changes))
	}
	return errors.Join(errs...)
}

// removeShellScripts は残っている scripts/ を消す。
//
// 残すと「どちらが動いているか」が分からなくなる(ADR D5)。消す前に必ず
// 一覧を出す。利用者が自分で置いたファイルが混ざっていることがあるためで、
// 出力を見れば何が失われたか後から辿れる。
func (i *Installer) removeShellScripts(out io.Writer) error {
	dir := i.Paths.ScriptsDir()
	names, err := i.Files.ListDir(dir)
	if err != nil {
		return err
	}
	if len(names) == 0 && !i.Files.Exists(dir) {
		return nil
	}
	_, _ = fmt.Fprintf(out, "  ✓ Shell スクリプトを撤去します(%s)\n", dir)
	for _, name := range names {
		_, _ = fmt.Fprintf(out, "      - %s\n", name)
	}
	return i.Files.Remove(dir)
}

// removeFlavor は廃止された切り替えフラグを消す(ADR D4)。
func (i *Installer) removeFlavor(out io.Writer) error {
	path := i.Paths.FlavorPath()
	if !i.Files.Exists(path) {
		return nil
	}
	if err := i.Files.Remove(path); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, "  ✓ FLAVOR を削除しました(切り替えの仕組みは廃止されました)")
	return nil
}

// reportShell は .zshrc の状況を伝える。**書き換えはしない。**
func (i *Installer) reportShell(out io.Writer) {
	zshrc, _, err := i.Files.Read(i.Paths.Zshrc)
	if err == nil && domain.ZshrcConfigured(string(zshrc)) {
		_, _ = fmt.Fprintln(out, "  ✓ .zshrc は設定済みです")
		return
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "次の行を .zshrc へ足してください:")
	_, _ = fmt.Fprintln(out, "  "+domain.ZshrcSourceLine)
}
