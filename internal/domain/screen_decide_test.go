package domain_test

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// screenTab はライフサイクルのテストで使うタブ名(test.sh 17b6 と同じ)。
const screenTab = "cx-task"

// renderScreenEffect は副作用を 1 行の文字列にする。
// テーブルの期待値を読みやすくするためのものである。
func renderScreenEffect(e domain.ScreenEffect) string {
	switch e.Kind {
	case domain.ScreenEffectWriteState:
		return "write-state " + e.Line
	case domain.ScreenEffectDeletePending:
		return "delete-pending " + e.Name
	case domain.ScreenEffectWritePending:
		return fmt.Sprintf("write-pending %s %s %s", e.Name, e.Event, e.Message)
	case domain.ScreenEffectFocusMain:
		return "focus-main"
	default:
		return "unknown " + string(e.Kind)
	}
}

func renderScreenEffects(effects []domain.ScreenEffect) []string {
	rendered := make([]string, 0, len(effects))
	for _, e := range effects {
		rendered = append(rendered, renderScreenEffect(e))
	}
	return rendered
}

// screenSim は副作用を実際のファイル操作の代わりに適用して状態を持ち越す、
// テスト用の最小の実行環境である。
//
// test.sh 17b6 は screen_update_pending を何度も呼んで pending ディレクトリの
// 状態を確かめる形なので、こちらも同じように「観測を重ねる」形で書けるように
// する。pending はファイル名の昇順で保つ(現行版の glob の並び)。
type screenSim struct {
	state    domain.ScreenState
	pendings []domain.ScreenPendingEntry
	focus    int
}

func (s *screenSim) putPending(name, tab, event string) {
	for i := range s.pendings {
		if s.pendings[i].Name == name {
			s.pendings[i].Event = event
			return
		}
	}
	s.pendings = append(s.pendings, domain.ScreenPendingEntry{Name: name, Tab: tab, Event: event})
	sort.Slice(s.pendings, func(i, j int) bool { return s.pendings[i].Name < s.pendings[j].Name })
}

func (s *screenSim) deletePending(name string) {
	kept := s.pendings[:0]
	for _, p := range s.pendings {
		if p.Name != name {
			kept = append(kept, p)
		}
	}
	s.pendings = kept
}

func (s *screenSim) hasPending(name string) bool {
	for _, p := range s.pendings {
		if p.Name == name {
			return true
		}
	}
	return false
}

// observe は 1 回の観測を流し込み、返った副作用を適用したうえで
// その並びを文字列として返す。
func (s *screenSim) observe(obs domain.ScreenObservation, now int64) []string {
	effects := domain.DecideScreen(domain.ScreenDecisionInput{
		Tab:      screenTab,
		Observed: obs,
		Prev:     s.state,
		Now:      now,
		Pendings: s.pendings,
	})
	for _, e := range effects {
		switch e.Kind {
		case domain.ScreenEffectWriteState:
			s.state = domain.ParseScreenState(e.Line)
		case domain.ScreenEffectDeletePending:
			s.deletePending(e.Name)
		case domain.ScreenEffectWritePending:
			s.putPending(e.Name, screenTab, e.Event)
		case domain.ScreenEffectFocusMain:
			s.focus++
		}
	}
	return renderScreenEffects(effects)
}

func blockedObs(message string) domain.ScreenObservation {
	return domain.ScreenObservation{State: domain.ScreenBlocked, Message: message}
}

var (
	workingObs = domain.ScreenObservation{State: domain.ScreenWorking}
	idleObs    = domain.ScreenObservation{State: domain.ScreenIdle}
	neutralObs = domain.ScreenObservation{State: domain.ScreenNeutral}
)

// TestDecideScreenTransitions は「前回状態 × 観測」の遷移表を全行固定する
// (evidence §2-2 / §2-3)。
func TestDecideScreenTransitions(t *testing.T) {
	t.Parallel()

	const now = int64(1000)

	tests := []struct {
		name      string
		prev      domain.ScreenState
		observed  domain.ScreenObservation
		wantState string
		wantFocus bool
	}{
		{
			name: "初回観測の working は Main へ戻らない",
			prev: domain.ScreenState{}, observed: workingObs,
			wantState: "working",
		},
		{
			name: "blocked から working はタブ内で承認した合図なので Main へ戻る",
			prev: domain.ScreenState{State: domain.ScreenBlocked}, observed: workingObs,
			wantState: "working", wantFocus: true,
		},
		{
			name: "idle から working は新しいプロンプト送信なので Main へ戻る",
			prev: domain.ScreenState{State: domain.ScreenIdle}, observed: workingObs,
			wantState: "working", wantFocus: true,
		},
		{
			name: "idle_pending から working は Main へ戻らない(スピナーのちらつき)",
			prev: domain.ScreenState{State: domain.ScreenIdlePending, At: "999"}, observed: workingObs,
			wantState: "working",
		},
		{
			name: "working が続いても Main へ戻らない",
			prev: domain.ScreenState{State: domain.ScreenWorking}, observed: workingObs,
			wantState: "working",
		},
		{
			name: "初回観測の idle は idle_pending に入らない",
			prev: domain.ScreenState{}, observed: idleObs,
			wantState: "idle",
		},
		{
			name: "working から idle は idle_pending へ退避する",
			prev: domain.ScreenState{State: domain.ScreenWorking}, observed: idleObs,
			wantState: "idle_pending 1000",
		},
		{
			name: "1 秒経たない再観測は最初の時刻を保ったまま待つ",
			prev: domain.ScreenState{State: domain.ScreenIdlePending, At: "1000"}, observed: idleObs,
			wantState: "idle_pending 1000",
		},
		{
			name: "1 秒経っていれば idle を確定する",
			prev: domain.ScreenState{State: domain.ScreenIdlePending, At: "999"}, observed: idleObs,
			wantState: "idle",
		},
		{
			name: "時刻が数値でなければ確定側へ倒す",
			prev: domain.ScreenState{State: domain.ScreenIdlePending, At: "broken"}, observed: idleObs,
			wantState: "idle",
		},
		{
			name: "時刻が無ければ確定側へ倒す",
			prev: domain.ScreenState{State: domain.ScreenIdlePending}, observed: idleObs,
			wantState: "idle",
		},
		{
			name: "blocked から idle は idle_pending に入らない",
			prev: domain.ScreenState{State: domain.ScreenBlocked}, observed: idleObs,
			wantState: "idle",
		},
		{
			name: "idle が続いても idle_pending に入らない",
			prev: domain.ScreenState{State: domain.ScreenIdle}, observed: idleObs,
			wantState: "idle",
		},
		{
			name: "blocked は前回状態にかかわらず blocked を書く",
			prev: domain.ScreenState{State: domain.ScreenIdlePending, At: "1000"}, observed: blockedObs("approval"),
			wantState: "blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			effects := domain.DecideScreen(domain.ScreenDecisionInput{
				Tab: screenTab, Observed: tt.observed, Prev: tt.prev, Now: now,
			})
			rendered := renderScreenEffects(effects)
			// 状態ファイルの書き込みは必ず**末尾**に来る(evidence §2-8)。
			if len(rendered) == 0 || rendered[len(rendered)-1] != "write-state "+tt.wantState {
				t.Fatalf("末尾の副作用 = %q, want %q", rendered, "write-state "+tt.wantState)
			}
			gotFocus := false
			for _, e := range rendered {
				if e == "focus-main" {
					gotFocus = true
				}
			}
			if gotFocus != tt.wantFocus {
				t.Errorf("focus-main = %v, want %v(%q)", gotFocus, tt.wantFocus, rendered)
			}
		})
	}
}

// TestDecideScreenNeutralIsCompleteNoOp は neutral が状態ファイルすら
// 書かないことを固定する(現行版は mkdir すら通らない)。
func TestDecideScreenNeutralIsCompleteNoOp(t *testing.T) {
	t.Parallel()

	sim := &screenSim{state: domain.ScreenState{State: domain.ScreenWorking}}
	sim.putPending("thread-n.json", screenTab, domain.EventNotification)

	if got := sim.observe(neutralObs, 1000); len(got) != 0 {
		t.Errorf("neutral の副作用 = %q, want 空", got)
	}
	if sim.state.State != domain.ScreenWorking {
		t.Errorf("neutral が前回状態を変えた: %+v", sim.state)
	}
	if !sim.hasPending("thread-n.json") {
		t.Error("neutral が pending を消した")
	}
	if sim.focus != 0 {
		t.Error("neutral が Main へ戻した")
	}
}

// TestDecideScreenNeutralPreservesIdlePending は neutral を挟んでも
// working → idle の確定手順が途切れないことを固定する。
func TestDecideScreenNeutralPreservesIdlePending(t *testing.T) {
	t.Parallel()

	sim := &screenSim{}
	sim.observe(workingObs, 1000)
	sim.observe(idleObs, 1000)
	parked := sim.state

	sim.observe(neutralObs, 1001)
	if sim.state != parked {
		t.Fatalf("neutral が idle_pending を壊した: %+v, want %+v", sim.state, parked)
	}
	sim.observe(idleObs, 1002)
	if !sim.hasPending(domain.ScreenPendingName(screenTab)) {
		t.Error("neutral を挟むと idle が確定しなくなった")
	}
}

// TestDecideScreenLifecycle は test.sh 17b6(:1240-1466)の観測列をそのまま
// 移したものである。1 ケース = 1 本の観測列で、各手順の副作用の並びを固定する。
func TestDecideScreenLifecycle(t *testing.T) {
	t.Parallel()

	screenName := domain.ScreenPendingName(screenTab)

	type step struct {
		what string
		obs  domain.ScreenObservation
		now  int64
		// setup は観測の前に外から置かれる pending(notify 由来の Stop や
		// Waiting の退避)。
		setup func(*screenSim)
		want  []string
	}

	tests := []struct {
		name  string
		steps []step
	}{
		{
			name: "blocked は Notification を書き、続く blocked では書き直さない",
			steps: []step{
				{
					what: "承認ダイアログを検出",
					obs:  blockedObs("Would you like to run the following command?"), now: 1000,
					want: []string{"write-pending " + screenName + " Notification Would you like to run the following command?",

						"write-state blocked",
					},
				},
				{
					what: "同じ承認が続く(初回検出時刻を保つ)",
					obs:  blockedObs("Would you like to run the following command?"), now: 1001,
					want: []string{"write-state blocked"},
				},
			},
		},
		{
			name: "message が空なら既定の文言を使う",
			steps: []step{
				{
					obs: blockedObs(""), now: 1000,
					want: []string{"write-pending " + screenName + " Notification Approval required",

						"write-state blocked",
					},
				},
			},
		},
		{
			name: "working はタブの pending を notify 由来ごと消す",
			steps: []step{
				{obs: blockedObs("approval"), now: 1000, want: []string{"write-pending " + screenName + " Notification approval",

					"write-state blocked",
				}},
				{
					what: "notify 由来の Stop が届いたあとにターンが再開する",
					setup: func(s *screenSim) {
						s.putPending("thread-1.json", screenTab, domain.EventStop)
					},
					obs: workingObs, now: 1001,
					want: []string{"delete-pending " + screenName,
						"delete-pending thread-1.json",
						"focus-main",

						"write-state working",
					},
				},
			},
		},
		{
			name: "working 直後の 1 回の idle では Stop を書かない",
			steps: []step{
				{obs: workingObs, now: 1000, want: []string{"write-state working"}},
				{
					what: "スピナーが消えた 1 フレーム目",
					obs:  idleObs, now: 1000,
					want: []string{"write-state idle_pending 1000"},
				},
				{
					what: "キー入力で早回りした再観測(同じ秒)",
					obs:  idleObs, now: 1000,
					want: []string{"write-state idle_pending 1000"},
				},
				{
					what: "実時間が経ってからの idle で確定",
					obs:  idleObs, now: 1005,
					want: []string{"write-pending " + screenName + " Stop Task complete",

						"write-state idle",
					},
				},
				{
					what: "確定後の idle は Stop を書き直さない",
					obs:  idleObs, now: 1006,
					want: []string{"write-state idle"},
				},
			},
		},
		{
			name: "idle 保留中の working は pending を消すが Main へは戻らない",
			steps: []step{
				{obs: workingObs, now: 1000, want: []string{"write-state working"}},
				{obs: idleObs, now: 1000, want: []string{"write-state idle_pending 1000"}},
				{
					setup: func(s *screenSim) {
						s.putPending("thread-p.json", screenTab, domain.EventNotification)
					},
					obs: workingObs, now: 1001,
					want: []string{"delete-pending thread-p.json", "write-state working"},
				},
			},
		},
		{
			name: "idle 保留中でも blocked は即座に Notification を出す",
			steps: []step{
				{obs: workingObs, now: 1000, want: []string{"write-state working"}},
				{obs: idleObs, now: 1000, want: []string{"write-state idle_pending 1000"}},
				{obs: blockedObs("approval"), now: 1000, want: []string{"write-pending " + screenName + " Notification approval",

					"write-state blocked",
				}},
			},
		},
		{
			name: "working を経ていない idle は何も書かない(新規タブの誤 done 防止)",
			steps: []step{
				{obs: idleObs, now: 1000, want: []string{"write-state idle"}},
				{obs: idleObs, now: 1005, want: []string{"write-state idle"}},
			},
		},
		{
			name: "notify 由来の Stop があるタブでは重複 Stop を書かない",
			steps: []step{
				{obs: workingObs, now: 1000, want: []string{"write-state working"}},
				{
					setup: func(s *screenSim) {
						s.putPending("thread-2.json", screenTab, domain.EventStop)
					},
					obs: idleObs, now: 1000,
					want: []string{"write-state idle_pending 1000"},
				},
				{obs: idleObs, now: 1005, want: []string{"write-state idle"}},
			},
		},
		{
			name: "blocked 解消後の idle は Notification を消す",
			steps: []step{
				{obs: blockedObs("approval"), now: 1000, want: []string{"write-pending " + screenName + " Notification approval",

					"write-state blocked",
				}},
				{
					setup: func(s *screenSim) {
						s.putPending("thread-3.json", screenTab, domain.EventStop)
					},
					obs: idleObs, now: 1001,
					want: []string{"delete-pending " + screenName, "write-state idle"},
				},
			},
		},
		{
			name: "遅れて届いた notify Stop に screen 由来 Stop が収束する",
			steps: []step{
				{obs: workingObs, now: 1000, want: []string{"write-state working"}},
				{obs: idleObs, now: 1000, want: []string{"write-state idle_pending 1000"}},
				{obs: idleObs, now: 1005, want: []string{"write-pending " + screenName + " Stop Task complete",

					"write-state idle",
				}},
				{
					what: "notify の Stop が後から着弾する",
					setup: func(s *screenSim) {
						s.putPending("thread-4.json", screenTab, domain.EventStop)
					},
					obs: idleObs, now: 1006,
					want: []string{"delete-pending " + screenName, "write-state idle"},
				},
			},
		},
		{
			name: "blocked から降りた idle は何度観測しても done にならない",
			steps: []step{
				{obs: blockedObs("approval"), now: 1000, want: []string{"write-pending " + screenName + " Notification approval",

					"write-state blocked",
				}},
				{obs: idleObs, now: 1001, want: []string{"delete-pending " + screenName, "write-state idle"}},
				{obs: idleObs, now: 1010, want: []string{"write-state idle"}},
				{obs: idleObs, now: 1020, want: []string{"write-state idle"}},
			},
		},
		{
			name: "Waiting のタブには一切触らないが内部状態だけは進む",
			steps: []step{
				{
					setup: func(s *screenSim) {
						s.putPending("park.json", screenTab, domain.EventWaiting)
					},
					obs: blockedObs("approval"), now: 1000,
					want: []string{"write-state blocked"},
				},
				{obs: workingObs, now: 1001, want: []string{"write-state working"}},
				{obs: idleObs, now: 1002, want: []string{"write-state idle_pending 1002"}},
				{obs: idleObs, now: 1010, want: []string{"write-state idle"}},
			},
		},
		{
			name: "別タブの pending は判断に影響しない",
			steps: []step{
				{obs: workingObs, now: 1000, want: []string{"write-state working"}},
				{
					setup: func(s *screenSim) {
						s.putPending("other.json", "other-tab", domain.EventStop)
						s.putPending("park-other.json", "other-tab", domain.EventWaiting)
					},
					obs: idleObs, now: 1000,
					want: []string{"write-state idle_pending 1000"},
				},
				{obs: idleObs, now: 1005, want: []string{"write-pending " + screenName + " Stop Task complete",

					"write-state idle",
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sim := &screenSim{}
			for i, s := range tt.steps {
				if s.setup != nil {
					s.setup(sim)
				}
				got := sim.observe(s.obs, s.now)
				if !reflect.DeepEqual(got, s.want) {
					t.Errorf("手順 %d(%s)の副作用 =\n  %s\nwant\n  %s",
						i+1, s.what, strings.Join(got, "\n  "), strings.Join(s.want, "\n  "))
				}
			}
		})
	}
}

// TestScreenPendingName はスクリーン検出が所有する pending の名前を固定する
// (現行版の `screen-<slug>.json` と `claude_session_id: "screen-<slug>"`)。
func TestScreenPendingName(t *testing.T) {
	t.Parallel()

	slug := domain.ScreenTabSlug(screenTab)
	if got, want := domain.ScreenPendingSessionID(screenTab), "screen-"+slug; got != want {
		t.Errorf("ScreenPendingSessionID() = %q, want %q", got, want)
	}
	if got, want := domain.ScreenPendingName(screenTab), "screen-"+slug+".json"; got != want {
		t.Errorf("ScreenPendingName() = %q, want %q", got, want)
	}
}

// TestDecideScreenWritesStateLast は状態ファイルの書き込みが必ず並びの末尾に
// 来ることを固定する。
//
// 呼び出し側は最初の失敗で残りを打ち切る。状態を先に進めると「状態だけ進んで
// pending は書けていない」状態で固定され、確定した Stop が二度と書かれない。
// 末尾に置けば、失敗した回は状態が進まず次の観測でやり直せる(evidence §2-8)。
func TestDecideScreenWritesStateLast(t *testing.T) {
	t.Parallel()

	screenName := domain.ScreenPendingName(screenTab)

	tests := []struct {
		name  string
		in    domain.ScreenDecisionInput
		count int
	}{
		{
			name: "blocked(pending の書き込みを伴う)",
			in: domain.ScreenDecisionInput{
				Tab: screenTab, Observed: blockedObs("approval"), Now: 1000,
			},
			count: 2,
		},
		{
			name: "working(削除と Main 帰還を伴う)",
			in: domain.ScreenDecisionInput{
				Tab: screenTab, Observed: workingObs, Now: 1000,
				Prev:     domain.ScreenState{State: domain.ScreenBlocked},
				Pendings: []domain.ScreenPendingEntry{{Name: screenName, Tab: screenTab, Event: domain.EventNotification}},
			},
			count: 3,
		},
		{
			name: "確定した idle(Stop の書き込みを伴う)",
			in: domain.ScreenDecisionInput{
				Tab: screenTab, Observed: idleObs, Now: 1000,
				Prev: domain.ScreenState{State: domain.ScreenIdlePending, At: "990"},
			},
			count: 2,
		},
		{
			name: "Waiting(状態の書き込みだけ)",
			in: domain.ScreenDecisionInput{
				Tab: screenTab, Observed: blockedObs("approval"), Now: 1000,
				Pendings: []domain.ScreenPendingEntry{{Name: "park.json", Tab: screenTab, Event: domain.EventWaiting}},
			},
			count: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			effects := domain.DecideScreen(tt.in)
			if len(effects) != tt.count {
				t.Fatalf("副作用の数 = %d, want %d(%q)", len(effects), tt.count,
					renderScreenEffects(effects))
			}
			if got := effects[len(effects)-1].Kind; got != domain.ScreenEffectWriteState {
				t.Errorf("末尾の副作用 = %q, want %q", got, domain.ScreenEffectWriteState)
			}
			for _, e := range effects[:len(effects)-1] {
				if e.Kind == domain.ScreenEffectWriteState {
					t.Errorf("状態の書き込みが末尾以外にある: %q", renderScreenEffects(effects))
				}
			}
		})
	}
}

// TestDecideScreenRetriesConfirmedStop は Stop の書き込みが失敗した回を
// 次の観測でやり直せること、そして書けた後は二重に書かないことを固定する。
//
// 状態ファイルが末尾になったことで、pending の書き込みが失敗した回は状態が
// idle_pending のまま残る。次の観測は同じ入力になるので同じ判断が出る。
func TestDecideScreenRetriesConfirmedStop(t *testing.T) {
	t.Parallel()

	screenName := domain.ScreenPendingName(screenTab)
	parked := domain.ScreenState{State: domain.ScreenIdlePending, At: "1000"}

	// 1 回目。Stop を書こうとする。
	first := domain.DecideScreen(domain.ScreenDecisionInput{
		Tab: screenTab, Observed: idleObs, Prev: parked, Now: 1005,
	})
	want := []string{
		"write-pending " + screenName + " Stop Task complete",
		"write-state idle",
	}
	if got := renderScreenEffects(first); !reflect.DeepEqual(got, want) {
		t.Fatalf("1 回目の副作用 = %q, want %q", got, want)
	}

	// pending の書き込みが失敗したとして、状態は進まなかったものとする
	// (呼び出し側は最初の失敗で打ち切るので write-state は実行されない)。
	retry := domain.DecideScreen(domain.ScreenDecisionInput{
		Tab: screenTab, Observed: idleObs, Prev: parked, Now: 1006,
	})
	if got := renderScreenEffects(retry); !reflect.DeepEqual(got, want) {
		t.Errorf("再試行の副作用 = %q, want %q", got, want)
	}

	// 書けた後は、同じ入力でも二重に書かない(タブに pending があるため)。
	done := domain.DecideScreen(domain.ScreenDecisionInput{
		Tab: screenTab, Observed: idleObs, Prev: parked, Now: 1007,
		Pendings: []domain.ScreenPendingEntry{
			{Name: screenName, Tab: screenTab, Event: domain.EventStop},
		},
	})
	if got := renderScreenEffects(done); !reflect.DeepEqual(got, []string{"write-state idle"}) {
		t.Errorf("Stop を二重に書いた: %q", renderScreenEffects(done))
	}
}

// TestDecideScreenConfirmsIdleWhenClockGoesBackwards は保留を始めた時刻が
// 「今」より後になっている場合に確定側へ倒すことを固定する。
//
// 時計が巻き戻ると差が負になり、そのまま待つと時計が追いつくまで保留が続いて
// 完了が出てこなくなる。読めない時刻を確定側へ倒すのと同じ考え方である。
func TestDecideScreenConfirmsIdleWhenClockGoesBackwards(t *testing.T) {
	t.Parallel()

	effects := domain.DecideScreen(domain.ScreenDecisionInput{
		Tab: screenTab, Observed: idleObs, Now: 1000,
		Prev: domain.ScreenState{State: domain.ScreenIdlePending, At: "2000"},
	})
	want := []string{
		"write-pending " + domain.ScreenPendingName(screenTab) + " Stop Task complete",
		"write-state idle",
	}
	if got := renderScreenEffects(effects); !reflect.DeepEqual(got, want) {
		t.Errorf("副作用 = %q, want %q", got, want)
	}
}

// TestIsScreenSessionID は合成 ID の判定を確かめる。
//
// ScreenPendingSessionID の対になる判定である。ここがずれると、合成 pending を
// 実セッションと取り違えて daily の置換キーに使ったり(履歴が消える)、
// 会話ゼロのタブで削除が止まったりする。
func TestIsScreenSessionID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sessionID string
		want      bool
	}{
		{name: "生成側と対になる", sessionID: domain.ScreenPendingSessionID("demo"), want: true},
		{name: "前置きだけ", sessionID: "screen-", want: true},
		{name: "実セッション ID", sessionID: "019ffa99-28ef-7d93-9d02-a606a979e0b7", want: false},
		{name: "空", sessionID: "", want: false},
		{name: "途中に現れるだけ", sessionID: "x-screen-demo", want: false},
		{name: "似た前置き", sessionID: "screening-demo", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.IsScreenSessionID(tt.sessionID); got != tt.want {
				t.Errorf("IsScreenSessionID(%q) = %v, want %v", tt.sessionID, got, tt.want)
			}
		})
	}
}
