package domain

import (
	"path/filepath"
	"strings"
)

// `mdev test <worktree>` は、worktree のソースから組んだバイナリを、隔離した
// データディレクトリで動かす(ADR D7)。
//
// 現行 init.zsh の mdev-test は CONDUCTOR_HOME を worktree そのものへ向けて
// いた。あちらは scripts/ と layouts/ が worktree に在ることが前提で、
// 「データの置き場所」と「実装の置き場所」が同じ 1 つの環境変数で決まって
// いた。Go 版では実装はバイナリの中にあるので、CONDUCTOR_HOME は
// **データの所在だけ**を指す(ADR D7 の再定義)。

// TestSessionDirName は worktree の下に作る隔離データディレクトリ名である。
//
// worktree の中に置くのは、worktree を消せばデータも一緒に消えるからである。
// 外に置くと、試したぶんだけ行き場のないディレクトリが残る。
const TestSessionDirName = ".mdev-test"

// TestSessionPrefix はテストセッションの名前の接頭辞である。
const TestSessionPrefix = "test-"

// WorktreeDirName はブランチ名から worktree を探すディレクトリ名である。
const WorktreeDirName = ".worktree"

// TestSessionSpec はテストセッションの起動内容である。
type TestSessionSpec struct {
	// Worktree は対象の worktree の絶対パス。
	Worktree string
	// ConductorHome は隔離したデータの置き場所。
	ConductorHome string
	// Binary は組んだバイナリの絶対パス。
	Binary string
	// Layout は起動に使うレイアウトの絶対パス。
	Layout string
	// Session は zellij のセッション名。
	Session string
}

// NewTestSessionSpec は worktree の絶対パスから起動内容を組み立てる。
//
// セッション名は `test-<worktree 名>` を上限まで切り詰めたものである。
// ハッシュ源に **worktree のパス全体**を使うのは、先頭を共有する worktree
// (add-foo と add-foo-bar)が同じセッションへ潰れないようにするためである。
func NewTestSessionSpec(worktree string) TestSessionSpec {
	name := filepath.Base(worktree)
	home := filepath.Join(worktree, TestSessionDirName)
	return TestSessionSpec{
		Worktree:      worktree,
		ConductorHome: home,
		Binary:        filepath.Join(home, "bin", "mdev"),
		Layout:        filepath.Join(home, "layouts", "multi.kdl"),
		Session:       ZellijSessionName(TestSessionPrefix+name, worktree),
	}
}

// LaunchCommand は新しい端末の中で走らせるコマンド行を返す。
//
// 現行版と同じく、まず同名のセッションを消してから作る。残っていると
// zellij が古い直列化レイアウトを復元してしまい、worktree の今のレイアウトで
// 始まらない。delete-session はセッションが無ければ何もしない。
func (s TestSessionSpec) LaunchCommand() string {
	return strings.Join([]string{
		"export CONDUCTOR_HOME=" + shellQuote(s.ConductorHome),
		"cd " + shellQuote(s.Worktree),
		shellQuote(s.Binary) + " news fetch",
		"zellij delete-session " + shellQuote(s.Session) + " --force 2>/dev/null",
		"zellij --new-session-with-layout " + shellQuote(s.Layout) +
			" --session " + shellQuote(s.Session),
	}, "; ")
}

// shellQuote は語を単一引用符で囲む。中の `'` は閉じて逃がして開き直す。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RenderTestLayout はレイアウトのペイン呼び出しを worktree のバイナリへ向ける。
//
// 埋め込みのレイアウトは `${CONDUCTOR_HOME:-...}/bin/mdev` を呼ぶ。テストでは
// **今から組んだバイナリの絶対パス**へ差し替える。CONDUCTOR_HOME 経由のままだと
// 隔離ディレクトリに置いたバイナリを指すことになり、置き忘れると設置済みの
// ものが動いて「切り替えたのに直っていない」になる。
func RenderTestLayout(template, binary string) string {
	return conductorBinPattern.ReplaceAllString(template, quoteForKDL(binary))
}

// quoteForKDL は KDL の文字列の中で使える引用符付きのパスにする。
func quoteForKDL(path string) string {
	return `\"` + path + `\"`
}

// ResolveWorktree は利用者の指定から worktree の場所を決める。
//
// 順に試す。
//
//   - そのままディレクトリとして在るならそれ(相対パスも受ける)
//   - 主リポジトリの `.worktree/<指定>` に在るならそれ(ブランチ名で呼べる)
//
// どちらでもなければ空を返す。
func ResolveWorktree(input, mainRoot string, isDir func(string) bool) string {
	if input == "" {
		return ""
	}
	if isDir(input) {
		return input
	}
	if mainRoot == "" {
		return ""
	}
	candidate := filepath.Join(mainRoot, WorktreeDirName, input)
	if isDir(candidate) {
		return candidate
	}
	return ""
}
