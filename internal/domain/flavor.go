package domain

// FlavorGo は「Go 版(mdev)の設定を使う」ことを表す印である。
//
// conductor の install.sh は $CONDUCTOR_HOME/FLAVOR の 1 行目を前後の空白を
// 落として読み、この値と一致したときだけ Go 版向けの設定を保つ。
//
//   - layouts の 5 ペインを `bin/mdev pane <name>` へ向け直す
//   - hooks のマージ後に `mdev hooks switch` を呼んで Go 版へ戻す
//
// install.sh も `mdev update` も layouts と hooks を **無条件に上書きする**
// ため、この印が無いと Go 版へ寄せた設定が再実行のたびに黙って巻き戻る。
// 印を書くのは「Go 版を採用する」という意思表示をした時点、つまり
// `mdev hooks switch` が成功したときである。
const FlavorGo = "go"

// FlavorFileName は印を置くファイル名(CONDUCTOR_HOME 直下)。
const FlavorFileName = "FLAVOR"

// FlavorFileContent は印のファイルに書く内容を返す。
//
// 末尾に改行を付けるのは、install.sh が `head -n 1` で読むためである。
// 改行が無くても読めるが、テキストファイルの慣習に従っておくと
// cat や head で覗いたときに表示が崩れない。
func FlavorFileContent(flavor string) string {
	return flavor + "\n"
}
