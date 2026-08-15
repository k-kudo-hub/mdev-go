// Package assets は conductor の同梱資産を実行ファイルへ埋め込む。
//
// # 出所
//
// ここにあるファイルは claude-conductor(Shell 版)から写したものである。
// **移設後の正本はこちら**で、conductor 側のファイルは 6-4 の撤去まで
// 現状維持のまま残す(触らない)。両方に同じファイルが在る間の扱いは次の
// とおりである。
//
//   - config.default.json / hooks.json: conductor と同一。hooks.json が
//     scripts/ を指したままなのは、これが Claude Code の settings.json へ
//     写す雛形で、mdev の形への書き換えを install が行うためである
//     (domain.InstallHooks が置換規則を通す。雛形の側を書き換えると、
//     その置換規則と二重に持つことになる)
//   - init.zsh: **mdev-go で新規に書いたもの**(conductor 側の 259 行の関数
//     定義とは別物)。PATH を通して `mdev init zsh` を eval するだけの入口で、
//     関数の中身はバイナリが出力する
//   - layouts/multi.kdl: 5 つのペインを `bin/mdev pane <名前>` へ書き換え済み
//   - layouts/dev.kdl: Agent ペインを `bin/mdev agent launch` へ書き換え済み
//
// 実環境への適用は 6-3 の `mdev install` が行う。このパッケージは受け皿を
// 用意するだけで、既存の設置物には触れない。
//
// # 置き場所
//
// domain からは参照しない(ADR-0002 の依存方向)。埋め込みは配布の都合で
// あって業務上の概念ではないため、domain がここを知る理由が無い。
package assets

import (
	"embed"
	"io/fs"
	"sort"
)

// files は埋め込む資産である。
//
// 明示的に並べるのは、埋め込む物を増やしたときに必ずこの行が変わるように
// するためである。ディレクトリごと埋めると、置き忘れや消し忘れが
// 実行ファイルの中身を黙って変える。
//
//go:embed config.default.json hooks.json init.zsh layouts/multi.kdl layouts/dev.kdl
var files embed.FS

// Names は埋め込まれている資産の名前を昇順で返す。
//
// 名前は CONDUCTOR_HOME からの相対パスで、区切りは常に `/` である
// (埋め込みファイルシステムの規約)。
func Names() []string {
	var names []string
	// 埋め込みは必ず読めるため、歩き回りが失敗する経路は無い。
	_ = fs.WalkDir(files, ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			names = append(names, path)
		}
		return nil
	})
	sort.Strings(names)
	return names
}

// Read は name の資産を返す。埋め込まれていなければ ok=false を返す。
func Read(name string) ([]byte, bool) {
	b, err := files.ReadFile(name)
	if err != nil {
		return nil, false
	}
	return b, true
}
