package domain

import "strconv"

// スクリーン検出が書く pending の文言。
const (
	// ScreenApprovalMessage は blocked の一致行が空だったときの既定の文言。
	ScreenApprovalMessage = "Approval required"
	// ScreenCompleteMessage は確定した idle で書く Stop の文言。
	ScreenCompleteMessage = "Task complete"
)

// screenPendingSuffix は pending ファイルの拡張子である。
//
// 状態機械は「タブの pending の中から自分が所有する 1 件を名前で見分ける」
// 必要があるため、ファイル名の規約を domain 側でも持つ。
const screenPendingSuffix = ".json"

// ScreenPendingSessionID はスクリーン検出が所有する pending の
// claude_session_id を返す(現行版の `screen-<slug>`)。
//
// タブ名の純関数であり、エージェントが発行した本物のセッション ID ではない。
// 復元(--resume)にこの値を使ってはならない(DailyRecord.HasDedupeKey と
// TaskRestorer のコメントを参照)。
func ScreenPendingSessionID(tab string) string {
	return ScreenSessionIDPrefix + ScreenTabSlug(tab)
}

// ScreenPendingName はスクリーン検出が所有する pending のファイル名を返す。
func ScreenPendingName(tab string) string {
	return ScreenPendingSessionID(tab) + screenPendingSuffix
}

// ScreenPendingEntry は判断に使う pending 1 件のスナップショットである。
// 状態機械が見るのはファイル名・タブ名・イベントの 3 つだけである。
type ScreenPendingEntry struct {
	// Name はファイル名。削除する対象を指すのに使う。
	Name string
	// Tab は pending の tab フィールド。
	Tab string
	// Event は pending の event フィールド。
	Event string
}

// ScreenEffectKind は副作用の種類である。
type ScreenEffectKind string

// 副作用の種類。実行はユースケース(app)が行う。
const (
	// ScreenEffectWriteState は状態ファイルへ 1 行書く。
	ScreenEffectWriteState ScreenEffectKind = "write-state"
	// ScreenEffectDeletePending は pending を 1 件消す。
	ScreenEffectDeletePending ScreenEffectKind = "delete-pending"
	// ScreenEffectWritePending はスクリーン検出が所有する pending を書く。
	ScreenEffectWritePending ScreenEffectKind = "write-pending"
	// ScreenEffectFocusMain はダッシュボードのタブへフォーカスを戻す。
	ScreenEffectFocusMain ScreenEffectKind = "focus-main"
)

// ScreenEffect は 1 つの副作用である。
type ScreenEffect struct {
	Kind ScreenEffectKind
	// Line は ScreenEffectWriteState で状態ファイルへ書く 1 行(改行なし)。
	Line string
	// Name は ScreenEffectDeletePending / ScreenEffectWritePending が
	// 対象にする pending のファイル名。
	Name string
	// Event / Message は ScreenEffectWritePending で書く内容。
	// 残りのフィールド(dir / task_type / transcript_path など)は
	// ユースケースがレジストリから借りて埋める。
	Event   string
	Message string
}

// ScreenDecisionInput は 1 回ぶんの判断の入力である。
type ScreenDecisionInput struct {
	// Tab は観測したペインのタブ名。
	Tab string
	// Observed は今回の観測結果(ClassifyScreen の戻り値)。
	Observed ScreenObservation
	// Prev は状態ファイルに入っていた前回の状態。
	Prev ScreenState
	// Now は現在時刻(epoch 秒)。
	Now int64
	// Pendings はセッションの pending 一覧。ファイル名の昇順で渡すこと
	// (現行版の glob の並びが削除の順序になる)。
	Pendings []ScreenPendingEntry
}

// DecideScreen は 1 回の観測から行うべき副作用の並びを返す。
//
// 現行 screen-detect-lib.sh の screen_update_pending を純粋関数にしたもので、
// **実行順序そのものが仕様**である(evidence §2-1)。
//
//  1. neutral は完全な no-op。状態ファイルすら書かない。全画面ビューアや
//     ピッカーの上ではスピナーもプロンプトも見えず、そこから何も結論できない
//     ためである。空の並びを返す。
//  2. 状態ファイルの書き込みは Waiting ガードより**前**に決まる。Waiting で
//     退避中のタブでも内部状態だけは進み、復帰したときに整合が取れる。
//  3. Waiting の pending があるタブでは、状態の書き込み以外は何もしない。
//     外部の返答待ちとして利用者が退避したものを、検出が勝手に戻したり
//     消したりしてはならない。
//  4. そのあとで観測した状態ごとの副作用を積む。
//
// **状態ファイルの書き込みは並びの末尾に置く。** 値は上の 2 で決まるが、
// 実行はいちばん最後である。呼び出し側は最初の失敗で残りを打ち切るので、
// pending の書き込みや削除が失敗したときに状態が進まず、次の観測で同じ判断が
// もう一度出て自然に再試行される。先頭に置くと「状態だけ進んで pending は
// 書けていない」状態で固定され、確定した Stop が二度と書かれない
// (evidence §2-8)。pending を消したり書いたりする副作用どうしの順序は
// 変わらないため、すべて成功する経路のファイルの最終状態は同じである。
//
// Pendings は変更しない。idle の判断は「1 段目で消した pending は 2 段目以降
// からは見えない」という現行版の挙動を持つため、内部でスナップショットを
// 削っていく(evidence §2-5)。
func DecideScreen(in ScreenDecisionInput) []ScreenEffect {
	if in.Observed.State == ScreenNeutral {
		return nil
	}

	next, confirmIdle := nextScreenState(in)
	writeState := ScreenEffect{Kind: ScreenEffectWriteState, Line: next.Format()}

	// 判断に関わるのは同じタブの pending だけである(現行版もすべての
	// ループが `.tab == $tab` を条件にしている)。
	tabPendings := make([]ScreenPendingEntry, 0, len(in.Pendings))
	waiting := false
	for _, p := range in.Pendings {
		if p.Tab != in.Tab {
			continue
		}
		if p.Event == EventWaiting {
			waiting = true
			break
		}
		tabPendings = append(tabPendings, p)
	}
	if waiting {
		return []ScreenEffect{writeState}
	}

	var effects []ScreenEffect
	screenName := ScreenPendingName(in.Tab)
	switch in.Observed.State {
	case ScreenBlocked:
		effects = appendScreenBlocked(effects, tabPendings, screenName, in.Observed.Message)
	case ScreenWorking:
		effects = appendScreenWorking(effects, tabPendings, in.Prev)
	case ScreenIdle:
		effects = appendScreenIdle(effects, tabPendings, screenName, confirmIdle)
	}
	return append(effects, writeState)
}

// appendScreenBlocked は承認待ちの副作用を積む。
//
// 既にスクリーン検出の Notification があるときは書き直さない。時刻が
// 「承認が最初に現れたとき」を指したままになるようにするためで、毎回書き直すと
// ダッシュボードの表示が最後のポーリング時刻に更新され続けてしまう。
func appendScreenBlocked(effects []ScreenEffect, tabPendings []ScreenPendingEntry,
	screenName, message string,
) []ScreenEffect {
	if findScreenEvent(tabPendings, screenName) == EventNotification {
		return effects
	}
	if message == "" {
		message = ScreenApprovalMessage
	}
	return append(effects, ScreenEffect{
		Kind: ScreenEffectWritePending, Name: screenName,
		Event: EventNotification, Message: message,
	})
}

// appendScreenWorking はターン再開の副作用を積む。
//
// タブの pending は notify 由来のものも含めてすべて消す。ターンが目に見えて
// 再開した以上、古い承認待ちも前のターンの完了も答えが出ている。
//
// Main への自動復帰は前回が blocked か idle のときだけである。初回観測(空)を
// 除くのは、ターンの途中でダッシュボードを再起動しただけでフォーカスを
// 奪わないためである。idle_pending を除くのは、それが「その idle は信用しない」
// という内部状態であり、利用者には何も起きていないように見えているためである。
func appendScreenWorking(effects []ScreenEffect, tabPendings []ScreenPendingEntry,
	prev ScreenState,
) []ScreenEffect {
	for _, p := range tabPendings {
		effects = append(effects, ScreenEffect{Kind: ScreenEffectDeletePending, Name: p.Name})
	}
	if prev.State == ScreenBlocked || prev.State == ScreenIdle {
		effects = append(effects, ScreenEffect{Kind: ScreenEffectFocusMain})
	}
	return effects
}

// appendScreenIdle は idle の副作用を 3 段で積む。
//
//  1. 自分が書いた Notification を消す(承認をタブの中で直接答えたケース)
//  2. 自分が書いた Stop は、同じタブに他の pending が現れたら消す。notify の
//     Stop は数秒遅れて着弾することがあり、二重の done は w キーの Waiting
//     切り替えも壊すため、後から来たほうへ譲る
//  3. idle が確定していて、かつタブに pending が 1 件も無いときだけ Stop を書く
//
// 1 で消した pending は 2 と 3 からは見えない(現行版は実際にファイルを消して
// から次の判定に進む)。
func appendScreenIdle(effects []ScreenEffect, tabPendings []ScreenPendingEntry,
	screenName string, confirmIdle bool,
) []ScreenEffect {
	switch findScreenEvent(tabPendings, screenName) {
	case EventNotification:
		effects = append(effects, ScreenEffect{Kind: ScreenEffectDeletePending, Name: screenName})
		tabPendings = removeScreenPending(tabPendings, screenName)
	case EventStop:
		if len(tabPendings) > 1 {
			effects = append(effects, ScreenEffect{Kind: ScreenEffectDeletePending, Name: screenName})
			tabPendings = removeScreenPending(tabPendings, screenName)
		}
	}

	if confirmIdle && len(tabPendings) == 0 {
		effects = append(effects, ScreenEffect{
			Kind: ScreenEffectWritePending, Name: screenName,
			Event: EventStop, Message: ScreenCompleteMessage,
		})
	}
	return effects
}

// nextScreenState は状態ファイルへ書く次の状態と、idle が確定したかを返す。
//
// working → idle は 1 回の観測では確定させず idle_pending に置く。codex の
// スピナー行はツール実行の切れ目や再描画の 1 フレームで消えるため、その瞬間を
// 拾うと偽の done がダッシュボードに出る。確定の条件は「次の観測」ではなく
// 「実時間が 1 秒以上経ってからの観測」である。ダッシュボードのポーリングは
// キー入力で早回りしうるので、観測回数だけを条件にすると同じ 1 フレームを
// 連続で見て確定してしまう。
//
// 比較は**整数 epoch 秒の差**で行う(現行版の `date +%s` の引き算)。実測の
// 経過時間が 1 秒に満たなくても秒の境界をまたげば確定する。パリティを優先して
// この粗さごと移植している(evidence §2-2)。
//
// blocked には遅延をかけない。人間を待たせている状態には即時性が要る。
func nextScreenState(in ScreenDecisionInput) (ScreenState, bool) {
	if in.Observed.State != ScreenIdle {
		return ScreenState{State: in.Observed.State}, false
	}

	switch in.Prev.State {
	case ScreenWorking:
		return ScreenState{State: ScreenIdlePending, At: strconv.FormatInt(in.Now, 10)}, false
	case ScreenIdlePending:
		// 時刻が読めない場合は待たずに確定させる(現行版も正規表現の判定が
		// 外れた時点で else 側へ落ちる)。
		//
		// 記録された時刻が「今」より後のときも確定側へ倒す。時計が巻き戻ると
		// 差が負になり、そのままでは時計が追いつくまで永久に保留が続いて
		// 完了が出てこなくなる。読めない時刻を確定側へ倒すのと同じ考え方で、
		// 信用できない時刻で待ち続けるより 1 回ぶん早く確定するほうがよい。
		since, ok := in.Prev.PendingSince()
		if ok && since <= in.Now && in.Now-since < 1 {
			return ScreenState{State: ScreenIdlePending, At: in.Prev.At}, false
		}
		return ScreenState{State: ScreenIdle}, true
	default:
		// 初回観測と blocked からの idle は idle_pending に入らない。
		// ターンが動いていたことを見ていないので、done ではありえない。
		return ScreenState{State: ScreenIdle}, false
	}
}

// findScreenEvent は name の pending の event を返す。無ければ空文字を返す。
func findScreenEvent(pendings []ScreenPendingEntry, name string) string {
	for _, p := range pendings {
		if p.Name == name {
			return p.Event
		}
	}
	return ""
}

// removeScreenPending は name の pending を取り除いた並びを返す。
func removeScreenPending(pendings []ScreenPendingEntry, name string) []ScreenPendingEntry {
	kept := make([]ScreenPendingEntry, 0, len(pendings))
	for _, p := range pendings {
		if p.Name != name {
			kept = append(kept, p)
		}
	}
	return kept
}
