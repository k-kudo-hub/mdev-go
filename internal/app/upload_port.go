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

// SummaryGenerator は会話テキストから作業要約を作る。
//
// 失敗(コマンドが無い・異常終了・出力が空)は必ず error になる。中身の無い
// 要約でログを残すと、失敗したことに気づけないまま会話が失われるためで、
// 呼び出し側はこの error でタブの削除を中止する。
type SummaryGenerator interface {
	Summarize(conversation string) (summary string, err error)
}

// LogUploadRunner は作業ログのアップロードを 1 回実行する。
// 実体は LogUploader である。
//
// 戻り値は現行 upload-log.sh の終了コードの契約をそのまま写している。
// ("", nil) は意図的に飛ばした、(message, nil) はアップロードした、
// ("", err) は失敗である。**失敗したときタブを削除してはならない。**
type LogUploadRunner interface {
	UploadLog(env PaneEnv, tab string) (output string, err error)
}
