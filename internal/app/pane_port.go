package app

import "github.com/k-kudo-hub/mdev-go/internal/domain"

// ダッシュボード系 4 ペインのユースケースが必要とする port。
//
// 移行期の方針(ADR-0002 の「ユースケースが必要とする操作単位で定義する」)に
// 従い、zellij のコマンド体系や Shell スクリプトの引数をそのまま interface に
// せず、ペインが行いたいことの単位で切っている。ただし TabLister だけは例外で、
// `list-tabs` の生の標準出力を返す。タブ名の取り出しと id の解決は現行の awk の
// 癖(スペース入りタブ名の扱いが一覧と id 解決で違う)ごと再現する必要があり、
// その解釈は domain の純粋関数(ParseTabNames / ResolveTabID)が持つためである。

// TabLister は zellij のタブ一覧の生出力を返す。
type TabLister interface {
	// ListTabs は `zellij action list-tabs` の標準出力をそのまま返す。
	// 失敗した場合は空文字を返す(現行版も 2>/dev/null で握り潰している)。
	ListTabs() string
}

// TabCloser は id を指定してタブを閉じる。
//
// 現行 Dashboard は close-tab のフォールバックを持たず、id が解決できなければ
// 何もしない。その非対称(task-control 側にはフォールバックがある)も含めて
// 呼び出し側で再現するため、この port は id を受ける形だけを持つ。
type TabCloser interface {
	CloseTabByID(id string)
}

// PendingLister は 1 セッションの pending をまとめて読む。
type PendingLister interface {
	// List は session の pending をファイル名の昇順で返す。
	// ディレクトリが無い場合は空を返す(エラーにしない)。
	List(session string) ([]domain.PendingView, error)
}

// PendingRemover は pending を削除する。
type PendingRemover interface {
	// DeleteByTab は tab に一致する pending をすべて削除する(タブの削除時)。
	DeleteByTab(session, tab string) error
	// DeleteByName は pending をファイル名で 1 件削除する(ジャンプ時のクリア)。
	DeleteByName(session, name string) error
}

// RegistryRemover はタスクレジストリからエントリを取り除く。
//
// 削除が確定したタスクを次回のセッション復元で蘇らせないために使う(issue #36)。
type RegistryRemover interface {
	RemoveByTab(session, tab string) error
}

// ScreenStateRemover はスクリーン検出の状態ファイルを削除する。
//
// 同じ名前のタブが後から作られたときに、前のタスクの状態を引き継がせない
// ためのもの。ファイル名は domain.ScreenTabSlug が決める。
type ScreenStateRemover interface {
	Remove(session, slug string) error
}

// DailyReader は当日の daily log を全セッション横断で読む。
type DailyReader interface {
	// ReadToday は date(YYYY-MM-DD)の daily ファイルを全セッションぶん探し、
	// その中身を行の並びとして返す。1 件も無ければ空を返す。
	ReadToday(date string) [][]byte
}

// NewsReader は当日のニュースファイルを読む。
type NewsReader interface {
	// Read は date のニュースファイルの中身を返す。
	// ファイルが無い場合は nil を返す(domain 側で「ニュース無し」に潰される)。
	Read(date string) []byte
}

// ConfigLoader は conductor の設定を読む。
//
// 読めなかった場合もエラーを返さずゼロ値を返す。現行版が
// `jq ... 2>/dev/null` で失敗を握り潰し、検出方式を既定の "hooks" に
// 落としているのに合わせている。
type ConfigLoader interface {
	Load() domain.Config
}

// URLOpener は既定のブラウザで URL を開く。
type URLOpener interface {
	// Open は URL を開く。失敗しても何も返さない(現行版も 2>/dev/null)。
	Open(url string)
}

// ShellRunner は移行期に Shell のまま残るスクリプトを呼ぶ。
//
// いずれもフェーズ 4 以降で Go 化する予定で、それまでは env
// (ZELLIJ_SESSION_NAME / CONDUCTOR_HOME)を引き継いだ同期呼び出しにする。
type ShellRunner interface {
	// UploadLog は upload-log.sh を呼ぶ。
	//
	// 終了コード 0 は「アップロードした」または「意図的に飛ばした」で、
	// 非 0 は失敗である。呼び出し側は非 0 のときタブの削除を中止しなければ
	// ならない(作業ログを失わないための契約)。output は標準出力の 1 行目から
	// `upload-log: ` を取り除いたもので、空なら表示するものが無い。
	UploadLog(tab string) (output string, err error)

	// RestoreTask は restore-task.sh を呼ぶ。終了コードは見ない(現行と同じ)。
	RestoreTask(tab, session, completedAt string)

	// FetchNews は fetch-news.sh --force を同期で呼ぶ。
	FetchNews()

	// RestoreSession は restore-session.sh を呼ぶ(Dashboard の起動時)。
	RestoreSession()

	// ScreenDetectTick は screen-detect-lib.sh の screen_detect_tick を呼ぶ。
	// Dashboard の毎ポーリングの先頭で走らせる。省略すると screen 方式の
	// エージェント(codex)のタスクが一覧に出てこない。
	ScreenDetectTick(session string)
}
