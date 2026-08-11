package app

import (
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

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

// TabCloser はタブを閉じる。
//
// id で閉じるのが本筋である。削除フローは同期のアップロードを挟むため、その間に
// 別のタブへ移っていることがあり、`close-tab`(今のタブを閉じる)は無関係な
// タブを巻き込みうる。
//
// フォールバックへ落ちるかどうかは呼び出し側が決める(TaskDeleter の
// CloseActiveOnMissingID)。現行 Dashboard は id を引けなければ何もせず、
// task-control だけが close-tab へ落ちるという非対称をそのまま再現している。
type TabCloser interface {
	CloseTabByID(id string)
	// CloseActiveTab は今フォーカスしているタブを閉じる。
	CloseActiveTab()
}

// TabActor はタスク作成とレイアウト適用が行う zellij 操作である。
//
// どのメソッドも「この 1 回を諦めるまでの時間」を先頭に受ける。劣化した
// zellij サーバでは `zellij action` が返らないことがあり、35 回近い操作を
// 積み上げるタスク作成は、全体の予算から逆算した上限を 1 回ごとに渡さないと
// 数分固まる(現行 task-lib.sh の `_zj_budget_cap` に対応する)。
// 実装側は渡された値を自分の上限(10 秒)で頭打ちにする。
type TabActor interface {
	// QueryTabNames は今あるタブの名前を返す。
	//
	// 失敗は空の一覧と区別して返す。上限で打ち切られたときに空を返すと、
	// 呼び出し側が「タブが 1 つも無い」と読んでしまう(TabNameQuerier の
	// コメントを参照)。タスク作成はこの error を捨て、期限まで再試行する。
	QueryTabNames(timeout time.Duration) ([]string, error)
	// FocusTabVerified は名前でタブへフォーカスを移し、実際に移ったかを返す。
	// `go-to-tab-name` は存在しない名前でも rc=0 で戻るため、成否は実装が
	// stdout の有無で判定する。
	FocusTabVerified(timeout time.Duration, name string) bool
	// NewTab はタブを作る。戻り値でタブ作成の成否が分かる。
	NewTab(timeout time.Duration, name, cwd string, command []string) error
	// NewPane は今のタブへペインを足す。command が空なら素のシェルになる。
	NewPane(timeout time.Duration, direction, cwd string, command []string) error
	// MoveFocus は方向を指定してペインのフォーカスを移す。
	MoveFocus(timeout time.Duration, direction string) error
	// FocusPreviousPane は 1 つ前のペインへフォーカスを戻す。
	FocusPreviousPane(timeout time.Duration) error
	// Resize はペインの大きさを変える。引数の数は呼び出し元で違う。
	Resize(timeout time.Duration, args ...string) error
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

// ScreenStateStore はスクリーン検出の状態ファイルを読み書きする。
//
// タブごとに 1 ファイル、中身は 1 行(domain.ScreenState)である。
// 検出はこれを「前回どう見えていたか」として使う。
type ScreenStateStore interface {
	// ReadScreenState は状態ファイルの中身をそのまま返す。
	// ファイルが無い・読めない場合は空文字を返す(初回観測として扱われる)。
	ReadScreenState(session, slug string) string
	// WriteScreenState は line を 1 行として書く(実装が末尾へ改行を足す)。
	WriteScreenState(session, slug, line string) error
}

// AgentPane はスクリーン検出の対象になりうるペイン 1 枚である。
//
// タスクタブのエージェントペインは create_task がコマンド行へ入れる
// `TASK_AGENT=<name>` で見分ける。レジストリを引かずに済むので、まだ 1 度も
// 完了していない(= エントリの無い)タブでも最初のターンから走査できる。
type AgentPane struct {
	// Tab はペインが属するタブ名。
	Tab string
	// ID はペインの id(zellij の数値 id を文字列にしたもの)。
	ID string
	// Agent は TASK_AGENT の値。
	Agent string
}

// PaneLister はセッションのエージェントペインを列挙する。
type PaneLister interface {
	// ListAgentPanes は `TASK_AGENT=` を持つ端末ペインを返す。
	// 失敗した場合は空を返す(その回は何も検出しなかった扱いになる)。
	ListAgentPanes() []AgentPane
}

// ScreenDumper はペインの画面を文字列として取り出す。
type ScreenDumper interface {
	// DumpScreen はペインの見えている内容を返す。
	// 失敗した場合と空だった場合はいずれも空文字を返し、呼び出し側は
	// そのペインを飛ばす(現行版の `|| true` + `[[ -n "$text" ]]`)。
	DumpScreen(paneID string) string
}

// RegistryTabLookup はタブに対応するレジストリのエントリを 1 件引く。
//
// スクリーン検出が書く pending は、レジストリから dir / task_type /
// transcript_path を借りる。これらが無いと、そのタブの唯一の pending が
// screen 由来になったときに削除時のログ収集や Done からの復元が壊れる。
type RegistryTabLookup interface {
	// LatestByTabMtime はタブ名が一致するエントリのうち、**ファイルの更新時刻が
	// 最も新しい** 1 件を返す。
	//
	// 復元(domain.LatestPerTab)が updated_at で選ぶのとはキーが違う。
	// 現行版の非対称をそのまま維持している(evidence §2-6)。
	LatestByTabMtime(session, tab string) (domain.RegistryEntry, bool)
}

// PendingSaver は pending を 1 件書き込む。
//
// スクリーン検出は自分が所有する 1 ファイル(screen-<slug>.json)だけを書く。
type PendingSaver interface {
	// Save は pending を書き込む。既存の内容は完全に置き換える。
	Save(session, sessionID string, pending domain.Pending) error
}

// RegistryLister はセッションのレジストリエントリを読む。
//
// 復元は「登録済みのタスクを全部見る」ので、1 件引きの
// RegistryTabLookup とは別の操作になる。
type RegistryLister interface {
	// List は session のエントリをファイル名の昇順で返す。
	// 壊れているエントリは読み飛ばす(1 件の破損で復元全体を止めない)。
	List(session string) ([]domain.RegistryEntry, error)
	// RemoveByTab はタブ名が一致するエントリをすべて削除する。
	RemoveByTab(session, tab string) error
}

// TabNameQuerier は今あるタブの名前を返す。
//
// TabActor も同じシグネチャを持つ(タスク作成が登録待ちのポーリングに使う)。
// 復元は「既に在るタブを飛ばす」ためだけに要るので、必要な操作 1 つに絞った
// port を別に置いている(ADR-0002)。
type TabNameQuerier interface {
	// QueryTabNames は今あるタブの名前を返す。
	//
	// 失敗は空の一覧と区別して返す。上限で打ち切られたときに空を返すと、
	// 呼び出し側が「タブが 1 つも無い」と読んでしまう(TabNameQuerier の
	// コメントを参照)。タスク作成はこの error を捨て、期限まで再試行する。
	QueryTabNames(timeout time.Duration) ([]string, error)
}

// PathChecker はパスの実在を確かめる。
//
// 復元は「作業ディレクトリがまだあるか」と「transcript がまだあるか」を見て、
// 作り直せるか・会話を再開できるかを決める。
type PathChecker interface {
	// IsDir は path が実在するディレクトリかを返す。
	IsDir(path string) bool
	// IsFile は path が実在する通常ファイルかを返す。
	IsFile(path string) bool
}

// TaskMaker はタスクタブを作る。実体は TaskCreator である。
//
// 復元の 2 経路(起動時のセッション復元と Done からの復元)はどちらも
// タスク作成をそのまま再利用する。作成の失敗の種類(ErrTabNotRegistered /
// ErrFocusNotConfirmed)で数え方が変わるため、error はそのまま渡ってくる。
type TaskMaker interface {
	Execute(env PaneEnv, spec TaskSpec) (TaskCreateResult, error)
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
//
// ok は設定を実際に読めたかどうかである。ゼロ値の設定は「エージェントが 1 つも
// 設定されていない」とも「設定が読めなかった」とも取れるが、この 2 つは判断が
// 逆になる場面がある(DashboardPane.Refresh のスクリーン検出)。読めなかった
// ことを黙って「無い」と扱わないよう、区別して返す。
type ConfigLoader interface {
	Load() (config domain.Config, ok bool)
}

// URLOpener は既定のブラウザで URL を開く。
type URLOpener interface {
	// Open は URL を開く。失敗しても何も返さない(現行版も 2>/dev/null)。
	Open(url string)
}

// ShellRunner は移行期に Shell のまま残るスクリプトを呼ぶ。
//
// フェーズ 5 で Go 化する予定で、それまでは env
// (ZELLIJ_SESSION_NAME / CONDUCTOR_HOME)を引き継いだ同期呼び出しにする。
type ShellRunner interface {
	// FetchNews は fetch-news.sh --force を同期で呼ぶ。
	FetchNews()
}
