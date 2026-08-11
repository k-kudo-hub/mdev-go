package app

// 作業ログのアップロード(現行 upload-log.sh)が必要とする port。

// LogPusher は作業ログをログ用リポジトリへ書き込んで push する。
//
// repo は設定の `upload.repo`(URL・ローカルパス・"owner/name" のいずれか)、
// branch は `upload.branch`、relPath はリポジトリ内の相対パス、content は
// 書き込む markdown である。
//
// 戻り値の reference は「どこに置いたか」を利用者へ見せる文字列で、
// GitHub の blob URL か「相対パス @ sha」になる。失敗したら error を返し、
// 呼び出し側はタブの削除を中止しなければならない。
type LogPusher interface {
	Push(repo, branch, relPath, content string) (reference string, err error)
}
