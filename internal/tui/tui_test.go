package tui_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/tui"
)

// ここでのテストは「モデルがユースケースをどう呼び分けるか」を確かめる。
// ユースケースの中身(何を消すか・何を書くか)は internal/app のテストが持つ。
//
// ユースケースは tui が定める interface で差し替える。app の port を直接
// 実装すると domain の型を扱うことになり、tui から domain への依存が
// 生まれてしまう(ADR-0002 で禁じている方向)。

// errUpload は upload-log.sh が非 0 で終わった状況を表す。
var errUpload = errors.New("upload に失敗した")

// errRefresh は一覧の読み直しが失敗した状況を表す。
var errRefresh = errors.New("pending の読み取りに失敗した")

// ---- Dashboard のスタブ ---------------------------------------------------

type stubDashboard struct {
	snapshot app.DashboardSnapshot
	// next を入れると、以降の Refresh はこちらを返す(ポーリングで一覧が
	// 入れ替わった状況を作るために使う)。
	next *app.DashboardSnapshot
	// refreshErr を入れると Refresh が失敗する(ゼロ値と一緒に返る)。
	refreshErr error
	prep       app.DeletePreparation
	prepErr    error

	calls []string
}

var _ tui.DashboardService = (*stubDashboard)(nil)

func (s *stubDashboard) Startup() { s.calls = append(s.calls, "startup") }

func (s *stubDashboard) Refresh(app.PaneEnv) (app.DashboardSnapshot, error) {
	s.calls = append(s.calls, "refresh")
	if s.refreshErr != nil {
		return app.DashboardSnapshot{}, s.refreshErr
	}
	if s.next != nil {
		return *s.next, nil
	}
	return s.snapshot, nil
}

func (s *stubDashboard) Jump(_ app.PaneEnv, _ app.DashboardSnapshot, number int) error {
	s.calls = append(s.calls, "jump "+itoa(number))
	return nil
}

func (s *stubDashboard) PrepareDelete(_ app.PaneEnv, tab string) (app.DeletePreparation, error) {
	s.calls = append(s.calls, "prepare "+tab)
	return s.prep, s.prepErr
}

func (s *stubDashboard) CommitDelete(_ app.PaneEnv, tab string) error {
	s.calls = append(s.calls, "commit "+tab)
	return nil
}

// ---- Waiting / Done / News のスタブ ---------------------------------------

type stubWaiting struct {
	text string
	// err を入れると Refresh が失敗する(空文字と一緒に返る)。
	err error
}

var _ tui.WaitingService = (*stubWaiting)(nil)

func (s *stubWaiting) Refresh(app.PaneEnv) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.text, nil
}

type stubDone struct {
	snapshot app.DoneSnapshot
	// next を入れると、以降の Refresh はこちらを返す。
	next *app.DoneSnapshot

	restored []int
	// restoredFrom は restore に渡されたスナップショットの表示内容。
	// どの世代の一覧を使ったかを見るために記録する。
	restoredFrom []string
}

var _ tui.DoneService = (*stubDone)(nil)

func (s *stubDone) Refresh() app.DoneSnapshot {
	if s.next != nil {
		return *s.next
	}
	return s.snapshot
}

func (s *stubDone) Restore(snapshot app.DoneSnapshot, number int) {
	s.restored = append(s.restored, number)
	s.restoredFrom = append(s.restoredFrom, snapshot.Text)
}

type stubNews struct {
	snapshot app.NewsSnapshot
	reloads  int
	opened   []int
}

var _ tui.NewsService = (*stubNews)(nil)

func (s *stubNews) Refresh() app.NewsSnapshot { return s.snapshot }
func (s *stubNews) Reload()                   { s.reloads++ }
func (s *stubNews) Open(_ app.NewsSnapshot, number int) {
	s.opened = append(s.opened, number)
}

// ---- ヘルパ ---------------------------------------------------------------

// itoa は 1 桁の数を文字列にする(テストの記録用)。
func itoa(n int) string { return string(rune('0' + n)) }

// run はモデルにメッセージを 1 つ渡し、返ってきたコマンドを 1 回実行して
// その結果のメッセージを返す。コマンドが無ければ nil を返す。
//
// Bubble Tea のメッセージ型は tui パッケージの非公開型なので、テストからは
// 中身を組み立てず「Init やコマンドが返したものをそのまま次へ渡す」形で
// 状態を進める。
func run(t *testing.T, m tea.Model, msg tea.Msg) (tea.Model, tea.Msg) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next, exec(t, cmd)
}

// exec はコマンドを 1 回実行してメッセージを返す。
//
// Init が返す tea.Batch は「最初の読み直し」と「ポーリングの開始」を束ねて
// いる。後者は tea.Tick のタイマーで、実行すると間隔ぶん実時間を待つことに
// なるため、束のうち先頭(読み直し)だけを実行する。
func exec(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	if len(batch) == 0 {
		return nil
	}
	return exec(t, batch[0])
}

// load は Init のコマンドを 1 回実行し、その結果を反映したモデルを返す
// (= 最初の描画が済んだ状態)。
func load(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	msg := exec(t, m.Init())
	if msg == nil {
		t.Fatal("Init が最初の読み直しを返していない")
	}
	next, _ := m.Update(msg)
	return next
}

// key は 1 文字のキー押下メッセージを作る。
func key(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// content はモデルの描画結果を文字列で返す。
func content(m tea.Model) string { return m.View().Content }

// specialKey は名前付きのキー押下メッセージを作る(esc / enter / ↑ など)。
func specialKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// ctrlKey は Ctrl 修飾つきのキー押下メッセージを作る。
func ctrlKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

// spaceKey はスペースキーの押下メッセージを作る。
//
// Bubble Tea v2 はスペースだけ String() が "space" になるため、他の
// 印字可能文字と同じ扱いにできない。取りこぼしを固定するために分けて作る。
func spaceKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "} }

var testEnv = app.PaneEnv{ZellijSession: "s1"}

// ---- 共通 -----------------------------------------------------------------

func TestPaneIntervalsMatchShellVersion(t *testing.T) {
	t.Parallel()

	// 現行の sleep / read -t に合わせた再描画間隔。
	if tui.DashboardInterval != 2*time.Second || tui.WaitingInterval != 2*time.Second {
		t.Error("Dashboard / Waiting の間隔が 2 秒ではない")
	}
	if tui.DoneInterval != 5*time.Second || tui.NewsInterval != 5*time.Second {
		t.Error("Done / News の間隔が 5 秒ではない")
	}
	// 2 打鍵目の待ち時間(現行版の read -t 3)。
	if tui.PromptTimeout != 3*time.Second {
		t.Error("2 打鍵目の待ち時間が 3 秒ではない")
	}
}

// ---- ポーリングの張り直し -------------------------------------------------

// ポーリングは完了起点で回る。次の合図を張るのは「ポーリングで出した読み直しの
// 着弾」だけで、チェーンは常にちょうど 1 本(未着弾の読み直しか予約済みの
// タイマーのどちらか一方)である。
//
// キー操作で出した読み直し(ジャンプ / restore / reload / 削除の完了 /
// 通知の期限切れ)の着弾で張り直すと、その生成元のぶんだけチェーンが増える。
// キー操作のたびに 1 本ずつ恒久的に増えていき、bash と zellij のプロセス生成が
// その本数ぶん多重に走ることになる。
//
// チェーンの入れ替わり(着弾 → タイマー → 読み直し)は tickMsg が非公開型の
// ため内部テスト(tick_internal_test.go)が確かめる。ここでは外から起こせる
// キー操作の側を見る。

// paneModel は 4 ペインを同じ形で回すためのテーブルの 1 行である。
type paneModel struct {
	name  string
	model tea.Model
}

// paneModels は 4 ペインのモデルを作って返す。
func paneModels() []paneModel {
	return []paneModel{
		{"dashboard", tui.NewDashboardModel(&stubDashboard{
			snapshot: app.DashboardSnapshot{Text: "画面", Tabs: []string{"alpha"}},
		}, testEnv)},
		{"waiting", tui.NewWaitingModel(&stubWaiting{text: "待ち画面"}, testEnv)},
		{"done", tui.NewDoneModel(&stubDone{
			snapshot: app.DoneSnapshot{Text: "完了画面", Count: 1},
		})},
		{"news", tui.NewNewsModel(&stubNews{
			snapshot: app.NewsSnapshot{Text: "ニュース画面", Count: 1},
		})},
	}
}

func TestPaneInitStartsChainWithRefreshOnly(t *testing.T) {
	t.Parallel()

	for _, tt := range paneModels() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := tt.model.Init()
			if cmd == nil {
				t.Fatal("Init がコマンドを返していない")
			}
			// Init が張るのは最初の読み直しだけである。ここでタイマーも
			// 一緒に張るとチェーンが 2 本になり、以後ずっと 2 本で回る。
			msg := cmd()
			if batch, ok := msg.(tea.BatchMsg); ok {
				t.Fatalf("Init が %d 個のコマンドを束ねている(次の合図は着弾で張る)", len(batch))
			}
			if msg == nil {
				t.Error("Init が最初の読み直しを出していない")
			}
		})
	}
}

// keyDrivenRefresh はキー操作で出した読み直しの着弾を、そのペインの
// モデルと一緒に返す。
type keyDrivenRefresh struct {
	name  string
	model tea.Model
	// landed はキー操作で出した読み直しの着弾メッセージ。
	landed tea.Msg
}

// keyDrivenRefreshes は 3 ペインぶんの「キー操作起源の着弾」を作る。
//
// Waiting は終了キー以外を受け付けない(この経路を持たない)ため入らない。
func keyDrivenRefreshes(t *testing.T) []keyDrivenRefresh {
	t.Helper()

	// Dashboard: 番号キーでジャンプしてから読み直す。
	dashboard := load(t, tui.NewDashboardModel(&stubDashboard{
		snapshot: app.DashboardSnapshot{Text: "画面", Tabs: []string{"alpha"}},
	}, testEnv))
	_, jumped := run(t, dashboard, key('1'))

	// Done: r + 番号で restore してから集計し直す。
	done := load(t, tui.NewDoneModel(&stubDone{
		snapshot: app.DoneSnapshot{Text: "完了画面", Count: 1},
	}))
	donePrompted, _ := done.Update(key('r'))
	_, restored := run(t, donePrompted, key('1'))

	// News: r で取得してから読み直す。
	news := load(t, tui.NewNewsModel(&stubNews{
		snapshot: app.NewsSnapshot{Text: "ニュース画面", FetchingText: "取得中", Count: 1},
	}))
	fetching, reloadMsg := run(t, news, key('r'))
	_, reloaded := run(t, fetching, reloadMsg)

	return []keyDrivenRefresh{
		{"dashboard", dashboard, jumped},
		{"done", donePrompted, restored},
		{"news", fetching, reloaded},
	}
}

func TestPaneKeyDrivenRefreshDoesNotReschedulePolling(t *testing.T) {
	t.Parallel()

	for _, tt := range keyDrivenRefreshes(t) {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.landed == nil {
				t.Fatal("キー操作の読み直しが着弾していない")
			}
			// キー操作起源の着弾を何度流してもコマンドは返らない。返して
			// しまうと、押した回数だけポーリングのチェーンが増える。
			model := tt.model
			for i := range 3 {
				next, cmd := model.Update(tt.landed)
				if cmd != nil {
					t.Fatalf("%d 回目の着弾でポーリングを張り直している", i+1)
				}
				model = next
			}
		})
	}
}

// ---- Dashboard ------------------------------------------------------------

func TestDashboardModelOnceRunsStartupAndRendersSnapshot(t *testing.T) {
	t.Parallel()

	service := &stubDashboard{snapshot: app.DashboardSnapshot{Text: "画面", Tabs: []string{"alpha"}}}
	m := tui.NewDashboardModel(service, testEnv)

	out, err := m.Once()
	if err != nil {
		t.Fatalf("Once() = %v", err)
	}

	// 単発描画でも起動時の復元は走る(現行の ONCE 経路と同じ)。
	if len(service.calls) != 2 || service.calls[0] != "startup" || service.calls[1] != "refresh" {
		t.Errorf("呼び出し = %v, want [startup refresh]", service.calls)
	}
	// 画面はユースケースが返した描画結果そのものである。
	if out != "画面" {
		t.Errorf("Once() = %q, want 画面", out)
	}
}

func TestDashboardModelJump(t *testing.T) {
	t.Parallel()

	service := &stubDashboard{snapshot: app.DashboardSnapshot{
		Text: "画面", Tabs: []string{"alpha", "beta"},
	}}
	m := tui.NewDashboardModel(service, testEnv)
	loaded := load(t, m)

	if _, _ = run(t, loaded, key('2')); !contains(service.calls, "jump 2") {
		t.Errorf("ジャンプが呼ばれていない: %v", service.calls)
	}
}

func TestDashboardModelIgnoresOutOfRangeNumber(t *testing.T) {
	t.Parallel()

	service := &stubDashboard{snapshot: app.DashboardSnapshot{Text: "画面", Tabs: []string{"alpha"}}}
	m := tui.NewDashboardModel(service, testEnv)
	loaded := load(t, m)

	if _, _ = run(t, loaded, key('5')); contains(service.calls, "jump 5") {
		t.Errorf("範囲外の番号でジャンプしている: %v", service.calls)
	}
}

func TestDashboardModelDeletePromptAndTimeout(t *testing.T) {
	t.Parallel()

	service := &stubDashboard{snapshot: app.DashboardSnapshot{Text: "画面", Tabs: []string{"alpha"}}}
	m := tui.NewDashboardModel(service, testEnv)
	loaded := load(t, m)

	// d を押すと 2 打鍵目の待ち受けに入り、案内が出る。
	prompted, timeoutMsg := run(t, loaded, key('d'))
	if !strings.Contains(content(prompted), "Delete tab number...") {
		t.Fatalf("削除の案内が出ていない: %q", content(prompted))
	}

	// 時間切れで待ち受けが解除され、案内も消える。
	expired, _ := prompted.Update(timeoutMsg)
	if strings.Contains(content(expired), "Delete tab number...") {
		t.Errorf("時間切れ後も案内が残っている: %q", content(expired))
	}
	// 時間切れでは削除に進まない。
	if contains(service.calls, "prepare alpha") {
		t.Errorf("時間切れなのに削除に進んでいる: %v", service.calls)
	}
}

func TestDashboardModelFreezesTabsWhileAwaiting(t *testing.T) {
	t.Parallel()

	// 2 打鍵目を待っている間に一覧が入れ替わると、押した番号が別のタブを
	// 指してしまい、消すつもりのないタブを消してしまう。現行 Shell 版は
	// `read -t 3` がループを止めるためこの隙間が無い。待ち受け中は表示も
	// 番号の対応も凍結する。
	service := &stubDashboard{snapshot: app.DashboardSnapshot{
		Text: "元の画面", Tabs: []string{"alpha", "beta"},
	}}
	m := tui.NewDashboardModel(service, testEnv)
	loaded := load(t, m)

	// ポーリングが一覧を入れ替えた状況の refreshedMsg を作る。メッセージ型は
	// 非公開なので、同じ経路(別のモデルの Init)から取り出す。
	service.next = &app.DashboardSnapshot{Text: "入れ替わった画面", Tabs: []string{"gamma"}}
	inflight := exec(t, tui.NewDashboardModel(service, testEnv).Init())

	// d を押した後に、発行済みの読み直しが着弾する。
	prompted, _ := loaded.Update(key('d'))
	stale, _ := prompted.Update(inflight)

	// 表示は待ち受けに入ったときのままである。
	if !strings.Contains(content(stale), "元の画面") {
		t.Errorf("待ち受け中に表示が入れ替わっている: %q", content(stale))
	}
	if strings.Contains(content(stale), "入れ替わった画面") {
		t.Errorf("待ち受け中に表示が入れ替わっている: %q", content(stale))
	}

	// 1 番は待ち受けに入ったときの 1 番(alpha)のままでなければならない。
	if _, _ = run(t, stale, key('1')); contains(service.calls, "prepare gamma") {
		t.Fatalf("入れ替わった一覧の側を消している: %v", service.calls)
	}
	if !contains(service.calls, "prepare alpha") {
		t.Errorf("凍結した一覧の 1 番を消していない: %v", service.calls)
	}
}

func TestDashboardModelResumesPollingAfterPromptTimeout(t *testing.T) {
	t.Parallel()

	// 待ち受けが時間切れになったら、凍結していた表示を追いつかせる。
	service := &stubDashboard{snapshot: app.DashboardSnapshot{
		Text: "元の画面", Tabs: []string{"alpha"},
	}}
	m := tui.NewDashboardModel(service, testEnv)
	loaded := load(t, m)

	prompted, timeoutMsg := run(t, loaded, key('d'))
	service.next = &app.DashboardSnapshot{Text: "新しい画面"}

	expired, refreshMsg := run(t, prompted, timeoutMsg)
	if refreshMsg == nil {
		t.Fatal("時間切れの後に読み直していない")
	}
	if caught, _ := expired.Update(refreshMsg); !strings.Contains(content(caught), "新しい画面") {
		t.Errorf("時間切れの後に表示が追いついていない: %q", content(caught))
	}
}

func TestDashboardModelDeleteRunsPrepareThenCommit(t *testing.T) {
	t.Parallel()

	// アップロード結果が空なら待たずに削除まで進む。
	service := &stubDashboard{snapshot: app.DashboardSnapshot{Text: "画面", Tabs: []string{"alpha"}}}
	m := tui.NewDashboardModel(service, testEnv)
	loaded := load(t, m)

	prompted, _ := loaded.Update(key('d'))
	// 1 打鍵目で prepare が走る。
	after, prepared := run(t, prompted, key('1'))
	// prepare の結果を受けて後半へ進む合図が返る。
	next, commitMsg := run(t, after, prepared)
	if commitMsg == nil {
		t.Fatal("削除の後半へ進んでいない")
	}
	// その合図で commit が走る。
	if _, finished := run(t, next, commitMsg); finished == nil {
		t.Fatal("削除の後半が終わっていない")
	}

	if !contains(service.calls, "prepare alpha") {
		t.Fatalf("prepare が呼ばれていない: %v", service.calls)
	}
	if !contains(service.calls, "commit alpha") {
		t.Errorf("commit が呼ばれていない: %v", service.calls)
	}
	// 順序も固定する。record と upload の後にしか消してはいけない。
	if indexOf(service.calls, "prepare alpha") > indexOf(service.calls, "commit alpha") {
		t.Errorf("prepare より先に commit している: %v", service.calls)
	}
}

func TestDashboardModelDeleteCancelledOnUploadFailure(t *testing.T) {
	t.Parallel()

	// upload-log.sh が失敗したときは削除の後半へ進んではいけない。
	service := &stubDashboard{
		snapshot: app.DashboardSnapshot{Text: "画面", Tabs: []string{"alpha"}},
		prep:     app.DeletePreparation{Cancelled: true},
	}
	m := tui.NewDashboardModel(service, testEnv)
	loaded := load(t, m)

	prompted, _ := loaded.Update(key('d'))
	after, prepared := run(t, prompted, key('1'))
	shown, _ := after.Update(prepared)

	if !strings.Contains(content(shown), "Upload failed. Deletion cancelled.") {
		t.Errorf("中止の表示が出ていない: %q", content(shown))
	}
	if contains(service.calls, "commit alpha") {
		t.Errorf("中止したのに削除している: %v", service.calls)
	}
}

func TestDashboardModelDeleteSurfacesPrepareError(t *testing.T) {
	t.Parallel()

	service := &stubDashboard{
		snapshot: app.DashboardSnapshot{Text: "画面", Tabs: []string{"alpha"}},
		prepErr:  errUpload,
	}
	m := tui.NewDashboardModel(service, testEnv)
	loaded := load(t, m)

	prompted, _ := loaded.Update(key('d'))
	after, prepared := run(t, prompted, key('1'))
	shown, _ := after.Update(prepared)

	if contains(service.calls, "commit alpha") {
		t.Errorf("エラーなのに削除している: %v", service.calls)
	}
	// 何も消さないことを黙って行うと、押した本人には何が起きたのか分からない。
	// 中止の表示(Cancelled)と同じく理由を画面に出す。
	if !strings.Contains(content(shown), errUpload.Error()) {
		t.Errorf("失敗の理由が表示されていない: %q", content(shown))
	}
}

// ---- エラーの表示 ---------------------------------------------------------

// Refresh の失敗を握り潰すと、一覧がゼロ値で上書きされて無言の白画面になる。
// 直前の内容を残したまま、理由を 1 行足す。

func TestDashboardModelKeepsLastSnapshotOnRefreshError(t *testing.T) {
	t.Parallel()

	service := &stubDashboard{snapshot: app.DashboardSnapshot{
		Text: "元の画面", Tabs: []string{"alpha"},
	}}
	m := tui.NewDashboardModel(service, testEnv)
	loaded := load(t, m)

	// 次の読み直しが失敗する。
	service.refreshErr = errRefresh
	failed, _ := loaded.Update(exec(t, tui.NewDashboardModel(service, testEnv).Init()))

	if !strings.Contains(content(failed), "元の画面") {
		t.Errorf("直前の一覧が消えている: %q", content(failed))
	}
	if !strings.Contains(content(failed), errRefresh.Error()) {
		t.Errorf("エラーが表示されていない: %q", content(failed))
	}

	// 読み直しに成功したらエラー行は消える。
	service.refreshErr = nil
	service.next = &app.DashboardSnapshot{Text: "新しい画面"}
	recovered, _ := failed.Update(exec(t, tui.NewDashboardModel(service, testEnv).Init()))
	if strings.Contains(content(recovered), errRefresh.Error()) {
		t.Errorf("復帰したのにエラーが残っている: %q", content(recovered))
	}
}

func TestWaitingModelKeepsLastTextOnRefreshError(t *testing.T) {
	t.Parallel()

	service := &stubWaiting{text: "待ち画面"}
	m := tui.NewWaitingModel(service, testEnv)
	loaded := load(t, m)

	service.err = errRefresh
	failed, _ := loaded.Update(exec(t, tui.NewWaitingModel(service, testEnv).Init()))

	if !strings.Contains(content(failed), "待ち画面") {
		t.Errorf("直前の一覧が消えている: %q", content(failed))
	}
	if !strings.Contains(content(failed), errRefresh.Error()) {
		t.Errorf("エラーが表示されていない: %q", content(failed))
	}
}

// ---- Waiting --------------------------------------------------------------

func TestWaitingModelOnce(t *testing.T) {
	t.Parallel()

	m := tui.NewWaitingModel(&stubWaiting{text: "待ち画面"}, testEnv)

	out, err := m.Once()
	if err != nil {
		t.Fatalf("Once() = %v", err)
	}
	if out != "待ち画面" {
		t.Errorf("Once() = %q, want 待ち画面", out)
	}
}

func TestWaitingModelIgnoresKeys(t *testing.T) {
	t.Parallel()

	// Waiting はキー入力を受け付けない。表示は変わらない。
	m := tui.NewWaitingModel(&stubWaiting{text: "待ち画面"}, testEnv)
	loaded := load(t, m)

	after, cmd := loaded.Update(key('r'))
	if cmd != nil {
		t.Error("キーに反応してコマンドを返している")
	}
	if content(after) != "待ち画面" {
		t.Errorf("表示が変わっている: %q", content(after))
	}
}

// ---- Done -----------------------------------------------------------------

func TestDoneModelOnce(t *testing.T) {
	t.Parallel()

	m := tui.NewDoneModel(&stubDone{snapshot: app.DoneSnapshot{Text: "完了画面", Count: 1}})

	out, err := m.Once()
	if err != nil {
		t.Fatalf("Once() = %v", err)
	}
	if out != "完了画面" {
		t.Errorf("Once() = %q, want 完了画面", out)
	}
}

func TestDoneModelRestore(t *testing.T) {
	t.Parallel()

	service := &stubDone{snapshot: app.DoneSnapshot{Text: "完了画面", Count: 2}}
	m := tui.NewDoneModel(service)
	loaded := load(t, m)

	// r を押すと案内が出る。
	prompted, _ := run(t, loaded, key('r'))
	if !strings.Contains(content(prompted), "Restore number...") {
		t.Fatalf("restore の案内が出ていない: %q", content(prompted))
	}

	// 番号で restore が呼ばれる。
	if _, _ = run(t, prompted, key('2')); len(service.restored) != 1 || service.restored[0] != 2 {
		t.Errorf("restore の番号 = %v, want [2]", service.restored)
	}
}

func TestDoneModelFreezesRowsWhileAwaiting(t *testing.T) {
	t.Parallel()

	// Dashboard と同じく、2 打鍵目を待っている間に一覧が入れ替わると
	// 押した番号が別の行を指してしまう。待ち受け中は凍結する。
	service := &stubDone{snapshot: app.DoneSnapshot{Text: "元の完了画面", Count: 3}}
	m := tui.NewDoneModel(service)
	loaded := load(t, m)

	service.next = &app.DoneSnapshot{Text: "入れ替わった完了画面", Count: 1}
	inflight := exec(t, tui.NewDoneModel(service).Init())

	prompted, _ := loaded.Update(key('r'))
	stale, _ := prompted.Update(inflight)

	if !strings.Contains(content(stale), "元の完了画面") {
		t.Errorf("待ち受け中に表示が入れ替わっている: %q", content(stale))
	}

	// 3 番は凍結した一覧(3 件)の 3 番である。入れ替わった一覧(1 件)を
	// 使うと範囲外になり、restore が呼ばれない。
	if _, _ = run(t, stale, key('3')); len(service.restored) != 1 || service.restored[0] != 3 {
		t.Fatalf("restore の番号 = %v, want [3]", service.restored)
	}
	if service.restoredFrom[0] != "元の完了画面" {
		t.Errorf("restore に渡した一覧 = %q, want 元の完了画面", service.restoredFrom[0])
	}
}

func TestDoneModelIgnoresRestoreWhenEmpty(t *testing.T) {
	t.Parallel()

	// 0 件のときは r を押しても案内を出さない。
	service := &stubDone{snapshot: app.DoneSnapshot{Text: "完了画面"}}
	m := tui.NewDoneModel(service)
	loaded := load(t, m)

	after, _ := loaded.Update(key('r'))
	if strings.Contains(content(after), "Restore number...") {
		t.Errorf("0 件なのに案内が出ている: %q", content(after))
	}
}

// ---- News -----------------------------------------------------------------

func TestNewsModelOnce(t *testing.T) {
	t.Parallel()

	m := tui.NewNewsModel(&stubNews{snapshot: app.NewsSnapshot{Text: "ニュース画面", Count: 1}})

	out, err := m.Once()
	if err != nil {
		t.Fatalf("Once() = %v", err)
	}
	if out != "ニュース画面" {
		t.Errorf("Once() = %q, want ニュース画面", out)
	}
}

func TestNewsModelReloadShowsFetchingScreen(t *testing.T) {
	t.Parallel()

	service := &stubNews{snapshot: app.NewsSnapshot{
		Text: "ニュース画面", FetchingText: "⟳ Fetching news...", Count: 1,
	}}
	m := tui.NewNewsModel(service)
	loaded := load(t, m)

	// r を押すと先に取得中の画面が出る(取得は同期で走るため)。
	fetching, reloadMsg := run(t, loaded, key('r'))
	if !strings.Contains(content(fetching), "Fetching news...") {
		t.Fatalf("取得中の画面が出ていない: %q", content(fetching))
	}
	if service.reloads != 0 {
		t.Error("画面を出す前に取得を始めている")
	}

	// その後に fetch-news.sh が走り、読み直した内容で通常の画面へ戻る。
	_, refreshed := run(t, fetching, reloadMsg)
	if service.reloads != 1 {
		t.Errorf("取得の呼び出し = %d, want 1", service.reloads)
	}
	done, _ := fetching.Update(refreshed)
	if content(done) != "ニュース画面" {
		t.Errorf("通常の画面へ戻っていない: %q", content(done))
	}
}

func TestNewsModelOpensURL(t *testing.T) {
	t.Parallel()

	service := &stubNews{snapshot: app.NewsSnapshot{Text: "ニュース画面", Count: 2}}
	m := tui.NewNewsModel(service)
	loaded := load(t, m)

	if _, _ = run(t, loaded, key('2')); len(service.opened) != 1 || service.opened[0] != 2 {
		t.Errorf("開いた番号 = %v, want [2]", service.opened)
	}
}

func TestNewsModelIgnoresOutOfRangeNumber(t *testing.T) {
	t.Parallel()

	service := &stubNews{snapshot: app.NewsSnapshot{Text: "ニュース画面", Count: 1}}
	m := tui.NewNewsModel(service)
	loaded := load(t, m)

	if _, _ = run(t, loaded, key('5')); len(service.opened) != 0 {
		t.Errorf("範囲外の番号で開いている: %v", service.opened)
	}
}

// ---- 小さなヘルパ ---------------------------------------------------------

func contains(list []string, want string) bool { return indexOf(list, want) >= 0 }

func indexOf(list []string, want string) int {
	for i, item := range list {
		if item == want {
			return i
		}
	}
	return -1
}
