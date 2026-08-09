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

// ---- Dashboard のスタブ ---------------------------------------------------

type stubDashboard struct {
	snapshot app.DashboardSnapshot
	prep     app.DeletePreparation
	prepErr  error

	calls []string
}

var _ tui.DashboardService = (*stubDashboard)(nil)

func (s *stubDashboard) Startup() { s.calls = append(s.calls, "startup") }

func (s *stubDashboard) Refresh(app.PaneEnv) (app.DashboardSnapshot, error) {
	s.calls = append(s.calls, "refresh")
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

type stubWaiting struct{ text string }

var _ tui.WaitingService = (*stubWaiting)(nil)

func (s *stubWaiting) Refresh(app.PaneEnv) (string, error) { return s.text, nil }

type stubDone struct {
	snapshot app.DoneSnapshot
	restored []int
}

var _ tui.DoneService = (*stubDone)(nil)

func (s *stubDone) Refresh() app.DoneSnapshot { return s.snapshot }
func (s *stubDone) Restore(_ app.DoneSnapshot, number int) {
	s.restored = append(s.restored, number)
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
	if cmd == nil {
		return next, nil
	}
	return next, cmd()
}

// load は Init のコマンドを 1 回実行し、その結果を反映したモデルを返す
// (= 最初の描画が済んだ状態)。
func load(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init がコマンドを返していない")
	}
	next, _ := m.Update(cmd())
	return next
}

// key は 1 文字のキー押下メッセージを作る。
func key(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// content はモデルの描画結果を文字列で返す。
func content(m tea.Model) string { return m.View().Content }

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
	if _, _ = after.Update(prepared); contains(service.calls, "commit alpha") {
		t.Errorf("エラーなのに削除している: %v", service.calls)
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
