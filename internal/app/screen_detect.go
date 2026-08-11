package app

import (
	"fmt"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// ScreenTicker は 1 回ぶんのスクリーン検出を走らせる。実体は ScreenDetector。
//
// Dashboard から実体を直接持たずに挟むのは、ペインの読み直しの先頭で
// 検出が走ることをテストで固定できるようにするためである。
type ScreenTicker interface {
	Tick(env PaneEnv) error
}

// ScreenDetector は hook を持たないエージェントの状態を画面から判定する
// ユースケースである(現行 screen-detect-lib.sh の screen_detect_tick 相当)。
//
// 1 回の Tick でセッションのエージェントペインを一巡し、ペインごとに
//
//	画面を取る → 分類する → 状態機械にかける → 返った副作用を実行する
//
// を行う。判断はすべて domain(ClassifyScreen / DecideScreen)が持ち、
// ここは入出力の接続だけを担う。
type ScreenDetector struct {
	Panes    PaneLister
	Dumper   ScreenDumper
	Config   ConfigLoader
	State    ScreenStateStore
	Pending  PendingLister
	Remover  PendingRemover
	Writer   PendingSaver
	Registry RegistryTabLookup
	Focuser  Focuser
	Clock    Clock
}

var _ ScreenTicker = (*ScreenDetector)(nil)

// Tick はセッションのエージェントペインを一巡して状態を反映する。
//
// 走査するのは検出方式が screen のエージェントのペインだけである。設定を
// 読めなかった場合は現行 task-lib.sh の agent_detection と同じく既定の hooks へ
// 落ちるため、結果として 1 枚も走査されない。
//
// **飛ばす条件は現行版と同じで、いずれもエラーにしない。**
//
//   - ペインを列挙できない(zellij が落ちている・打ち切られた)
//   - タブ名かペイン id が空
//   - 検出方式が screen ではない
//   - 画面のダンプが空(打ち切られた・まだ描画されていない)
//
// 最後のものが特に重要で、空のダンプから状態を決めると「何も映っていない
// 画面 = idle」になり、動いているタスクが done として一覧に並ぶ。
//
// 一方でファイルの読み書きに失敗した場合はエラーを返す。現行版はこれも
// 黙って捨てるため、pending を書けなくなってもダッシュボードは古い一覧を
// 出し続け、利用者は原因に気づけない。観測を記録できないのは pending を
// 読めないのと同じ種類の問題なので、呼び出し側へ渡して表示させる
// (意図的な差異)。
func (d *ScreenDetector) Tick(env PaneEnv) error {
	panes := d.Panes.ListAgentPanes()
	if len(panes) == 0 {
		return nil
	}

	config, _ := d.Config.Load()
	session := env.Session()
	for _, pane := range panes {
		if pane.Tab == "" || pane.ID == "" {
			continue
		}
		if config.AgentDetection(pane.Agent) != domain.DetectionScreen {
			continue
		}
		text := d.Dumper.DumpScreen(pane.ID)
		if text == "" {
			continue
		}
		observed := domain.ClassifyScreen(config.AgentPatterns(pane.Agent), text)
		if err := d.apply(session, pane, observed); err != nil {
			return err
		}
	}
	return nil
}

// apply は 1 枚のペインの観測を状態機械にかけ、返った副作用を順に実行する。
//
// pending はペインごとに読み直す。同じ tick の中で前のペインが消した pending を
// 見てしまうと判断が変わるためで、現行版も screen_update_pending の呼び出し
// ごとにディレクトリを走査している。
func (d *ScreenDetector) apply(session string, pane AgentPane, observed domain.ScreenObservation) error {
	slug := domain.ScreenTabSlug(pane.Tab)
	prev := domain.ParseScreenState(d.State.ReadScreenState(session, slug))

	views, err := d.Pending.List(session)
	if err != nil {
		return fmt.Errorf("pending の読み取りに失敗しました: %w", err)
	}
	entries := make([]domain.ScreenPendingEntry, 0, len(views))
	for _, view := range views {
		entries = append(entries, domain.ScreenPendingEntry{
			Name: view.Name, Tab: view.Tab, Event: view.Event,
		})
	}

	effects := domain.DecideScreen(domain.ScreenDecisionInput{
		Tab:      pane.Tab,
		Observed: observed,
		Prev:     prev,
		Now:      d.Clock.Now().Unix(),
		Pendings: entries,
	})
	for _, effect := range effects {
		if err := d.runEffect(session, slug, pane, effect); err != nil {
			return err
		}
	}
	return nil
}

// runEffect は副作用を 1 つ実行する。
func (d *ScreenDetector) runEffect(session, slug string, pane AgentPane, effect domain.ScreenEffect) error {
	switch effect.Kind {
	case domain.ScreenEffectWriteState:
		if err := d.State.WriteScreenState(session, slug, effect.Line); err != nil {
			return fmt.Errorf("スクリーン検出の状態の書き込みに失敗しました: %w", err)
		}
	case domain.ScreenEffectDeletePending:
		if err := d.Remover.DeleteByName(session, effect.Name); err != nil {
			return fmt.Errorf("pending の削除に失敗しました: %w", err)
		}
	case domain.ScreenEffectWritePending:
		if err := d.writePending(session, pane, effect); err != nil {
			return err
		}
	case domain.ScreenEffectFocusMain:
		// フォーカスの失敗は握り潰す(現行版も `2>/dev/null || true`)。
		// zellij の外や、タブが既に閉じられている場合が該当する。
		_ = d.Focuser.FocusTab(domain.MainTabName)
	}
	return nil
}

// writePending はスクリーン検出が所有する pending を書く。
//
// claude_session_id はタブ名から合成する(`screen-<slug>`)。hook を持たない
// エージェントの完了は画面から判定するため、ここには本物のセッション ID が
// 無い。この合成 ID を復元(--resume)に使ってはならない。
//
// dir / task_type / transcript_path はレジストリの**更新時刻が最新**の
// エントリから借りる。screen 由来のエントリがそのタブの唯一の pending に
// なったとき、下流(削除時のログ収集と Done からの復元)が動き続けるように
// するためである。エントリが無ければ空のまま = キーごと省略される。
func (d *ScreenDetector) writePending(session string, pane AgentPane, effect domain.ScreenEffect) error {
	pending := domain.Pending{
		Tab:             pane.Tab,
		Session:         session,
		ClaudeSessionID: domain.ScreenPendingSessionID(pane.Tab),
		Message:         effect.Message,
		Event:           effect.Event,
		Time:            d.Clock.Now().Format(domain.PendingTimeLayout),
		Agent:           pane.Agent,
	}
	if entry, ok := d.Registry.LatestByTabMtime(session, pane.Tab); ok {
		pending.TranscriptPath = entry.TranscriptPath
		pending.Dir = entry.Dir
		pending.TaskType = entry.TaskType
	}

	if err := d.Writer.Save(session, domain.ScreenPendingSessionID(pane.Tab), pending); err != nil {
		return fmt.Errorf("スクリーン検出の pending の書き込みに失敗しました: %w", err)
	}
	return nil
}
