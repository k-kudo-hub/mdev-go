package domain

import (
	"fmt"
	"strings"
)

// MdevRepoURL は移行後の更新元である(ADR D8 の移行の要)。
//
// conductor ではなく mdev-go を指す。ここを切り替えた瞬間に「tarball を
// 落として install.sh を bash で走らせる」旧フローは成立しなくなるため、
// 更新は自己置換 + 自分自身の install(冪等再適用)へ移る。
const MdevRepoURL = "https://github.com/" + MdevRepoSlug

// IsMdevRepoURL は更新元が mdev-go を指しているかを返す。
//
// 指しているなら移行は済んでいる。conductor の資産と mdev 本体という 2 本
// 立ての版はもう存在せず、VERSION にはバイナリ自身の版が入っている
// (ADR D3-2)。更新確認はこれを見て 1 本の比較に畳む。
//
// 末尾の `.git` とスラッシュは無視する。git remote の書き方はどちらもある。
func IsMdevRepoURL(url string) bool {
	slug, ok := RepoSlug(url)
	return ok && slug == MdevRepoSlug
}

// InstallPaths は install が触る場所である。
type InstallPaths struct {
	// Home は利用者のホーム。
	Home string
	// ConductorHome はデータと状態ファイルの置き場所。
	ConductorHome string
	// Settings は Claude Code の settings.json。
	Settings string
	// CodexConfig は codex の config.toml。
	CodexConfig string
	// Zshrc は .zshrc。中身は読むだけで、書き換えはしない。
	Zshrc string
}

// MdevBinaryPath は配置される mdev のパスを返す。
func (p InstallPaths) MdevBinaryPath() string {
	return p.ConductorHome + "/bin/mdev"
}

// ScriptsDir は撤去対象の Shell スクリプト置き場を返す。
func (p InstallPaths) ScriptsDir() string {
	return p.ConductorHome + "/scripts"
}

// InitZshPath は .zshrc から source される入口を返す。
func (p InstallPaths) InitZshPath() string {
	return p.ConductorHome + "/init.zsh"
}

// FlavorPath は廃止された切り替えフラグの場所を返す(見つけたら消す)。
func (p InstallPaths) FlavorPath() string {
	return p.ConductorHome + "/FLAVOR"
}

// ConductorPath は CONDUCTOR_HOME からの相対パスを絶対パスにする。
func (p InstallPaths) ConductorPath(rel string) string {
	return p.ConductorHome + "/" + rel
}

// 依存コマンド。
const (
	// ZellijCommand はセッションを作る本体。無いと何も動かない。
	ZellijCommand = "zellij"
	// ClaudeCommand / CodexCommand はエージェント CLI。どちらか 1 つあればよい。
	ClaudeCommand = "claude"
	CodexCommand  = "codex"
	// NotifierCommand は macOS の通知(任意)。
	NotifierCommand = "terminal-notifier"
)

// DependencyReport は依存チェックの結果である。
type DependencyReport struct {
	// MissingRequired は無いと動かないコマンド。
	MissingRequired []string
	// Agents は見つかったエージェント CLI。
	Agents []string
	// Optional は見つかった任意のコマンド。
	Optional []string
}

// OK は install を続けてよいかを返す。
func (r DependencyReport) OK() bool {
	return len(r.MissingRequired) == 0 && len(r.Agents) > 0
}

// Problem は続けられない理由を返す。OK なら空文字。
func (r DependencyReport) Problem() string {
	if len(r.MissingRequired) > 0 {
		return "必要なコマンドがありません: " + strings.Join(r.MissingRequired, ", ")
	}
	if len(r.Agents) == 0 {
		return fmt.Sprintf("エージェント CLI がありません(%s か %s のどちらかが要ります)",
			ClaudeCommand, CodexCommand)
	}
	return ""
}

// CheckDependencies は available(コマンドが使えるか)から依存の状況を組み立てる。
//
// 現行 install.sh は zellij / jq / fzf / claude の 4 つを必須にしていた。
// Go 版では **jq と fzf を外す**。設定の加工は Go が行い、絞り込みの画面は
// mdev 自身が描くようになったため、どちらも一度も呼ばない。
//
// claude も単体では必須にしない。エージェントは設定で選べるようになっており、
// codex だけを使う利用者に claude を求める理由が無い。どちらも無いときだけ
// 止める。
func CheckDependencies(available func(string) bool, darwin bool) DependencyReport {
	var report DependencyReport
	if !available(ZellijCommand) {
		report.MissingRequired = append(report.MissingRequired, ZellijCommand)
	}
	for _, agent := range []string{ClaudeCommand, CodexCommand} {
		if available(agent) {
			report.Agents = append(report.Agents, agent)
		}
	}
	if darwin && available(NotifierCommand) {
		report.Optional = append(report.Optional, NotifierCommand)
	}
	return report
}

// ZshrcSourceMarker は .zshrc が入口を読み込んでいるかを見る目印である。
const ZshrcSourceMarker = "claude-conductor/init.zsh"

// ZshrcSourceLine は .zshrc へ足してもらう行である。
const ZshrcSourceLine = `source "$HOME/.claude-conductor/init.zsh"`

// ZshrcConfigured は .zshrc が入口を読み込んでいるかを返す。
//
// **書き換えはしない。** 現行 install.sh は同意を取って追記していたが、
// 移行では既存の行がそのまま使える(入口の中身がシムに変わるだけ)ので、
// 触る理由が無い。新規の利用者には案内だけ出す。
func ZshrcConfigured(zshrc string) bool {
	return strings.Contains(zshrc, ZshrcSourceMarker)
}
