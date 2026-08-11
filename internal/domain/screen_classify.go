package domain

import (
	"regexp"
	"strings"
	"unicode"
)

// スクリーン検出が観測しうる状態。
//
// 現行 screen-detect-lib.sh の screen_classify が出力する 4 つと、
// 状態ファイルにだけ現れる idle_pending である。
const (
	// ScreenNeutral はエージェントの進行状態が読み取れない画面
	// (全画面ビューアやピッカー)。何も更新しない。
	ScreenNeutral = "neutral"
	// ScreenBlocked は既知の承認プロンプトが出ている画面。
	ScreenBlocked = "blocked"
	// ScreenWorking はターンが進行中の画面(スピナー行がある)。
	ScreenWorking = "working"
	// ScreenIdle は上のどれでもない画面。ターンの終わりかもしれない。
	ScreenIdle = "idle"
	// ScreenIdlePending は idle を 1 度観測したが確定させていない内部状態。
	// 分類の結果としては現れず、状態ファイルにだけ書かれる。
	ScreenIdlePending = "idle_pending"
)

// ScreenTailLines は分類に使う窓の行数(現行版の SCREEN_TAIL_LINES=20)。
//
// dump-screen はビューポートの高さまで空行で埋めるため、生の末尾 N 行では
// 背の高いペインでパディングしか見えない。空行を落としてから数える。
const ScreenTailLines = 20

// isScreenBlank は空白として扱う文字かどうかを返す。
//
// 現行版の `grep -v '^[[:space:]]*$'` と `sed 's/^[[:space:]]*//'` に対応する。
// **ASCII の空白だけでは足りない。** BSD の grep / sed は LC_CTYPE が UTF-8 の
// とき `[[:space:]]` をロケールに従って判定し、全角空白(U+3000)や NBSP
// (U+00A0)も空白として扱う。実行環境は UTF-8 なので、ASCII だけで判定すると
// 全角空白で作られたパディング行が「中身のある行」として末尾 20 行の窓を
// 埋め、承認プロンプトが窓の外へ押し出される。
//
// unicode.IsSpace が grep と同じ集合を指すことは実機で確認済み
// (U+00A0 / U+2003 / U+3000 は空白、U+200B は非空白で一致。evidence §1-1)。
func isScreenBlank(r rune) bool { return unicode.IsSpace(r) }

// ScreenPatterns は 1 エージェントぶんのスクリーン検出パターンである
// (設定の `.agents.<name>.patterns`)。
//
// いずれも POSIX ERE(grep -E)で、行に対して部分一致で判定する。
// 空の並びはその状態に決して分類されないことを意味する。
type ScreenPatterns struct {
	// Neutral は「この画面からは何も分からない」ことを表すパターン。
	Neutral []string `json:"neutral"`
	// Blocked は承認待ちのプロンプトを表すパターン。
	Blocked []string `json:"blocked"`
	// Working はターンが進行中であることを表すパターン。
	Working []string `json:"working"`
}

// ScreenObservation は 1 回の画面観測の結果である。
type ScreenObservation struct {
	// State は ScreenNeutral / ScreenBlocked / ScreenWorking / ScreenIdle。
	State string
	// Message は blocked のときだけ入る、一致した行(先頭の空白を除いたもの)。
	Message string
}

// ScreenTailWindow は分類に使う画面末尾の窓を返す。
//
// 現行版の `grep -v '^[[:space:]]*$' | tail -n N` に対応する。空白のみの行を
// 落としてから末尾 n 行を取る。n が 0 以下なら全行を返す。
//
// 行の中身は加工しない。blocked のメッセージ整形(先頭の空白除去)は
// ClassifyScreen が一致行に対してだけ行う。
func ScreenTailWindow(text string, n int) []string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimLeftFunc(line, isScreenBlank) == "" {
			continue
		}
		kept = append(kept, line)
	}
	if n > 0 && len(kept) > n {
		kept = kept[len(kept)-n:]
	}
	return kept
}

// ClassifyScreen は画面のダンプを 1 つの状態へ分類する。
//
// 優先順位は neutral > blocked > working > idle である(現行版と同じ)。
//
//   - neutral が最優先なのは、エージェントが所有していない画面ではスピナーが
//     隠れて偽の done に、スクロールバックが承認文言を映して偽の blocked に
//     なるためである(herdr の skip_state_update 相当)。
//   - blocked が working に勝つのは、承認ダイアログこそ人間が答えるべき
//     ものだからである。
//   - どれにも当てはまらない画面は idle に倒す。未知のダイアログを
//     blocked として上げると、エージェントの新しい UI が出るたびに
//     ダッシュボードが偽の承認待ちで埋まる。
//
// blocked のメッセージは**パターン主導**で決める。設定の配列順に見て、最初に
// ヒットしたパターンの、最初のヒット行を返す(evidence §1-3)。
//
// 窓が空(画面が空白だけ)の場合は空行 1 行を照合対象にする。現行版の
// `printf '%s\n' "$tail_buf"` が空でも改行 1 個を grep へ渡すためである
// (evidence §1-4)。
func ClassifyScreen(patterns ScreenPatterns, text string) ScreenObservation {
	window := ScreenTailWindow(text, ScreenTailLines)
	if len(window) == 0 {
		window = []string{""}
	}

	if matchesAnyScreenPattern(patterns.Neutral, window) {
		return ScreenObservation{State: ScreenNeutral}
	}
	if message, ok := firstScreenMatch(patterns.Blocked, window); ok {
		return ScreenObservation{State: ScreenBlocked, Message: message}
	}
	if matchesAnyScreenPattern(patterns.Working, window) {
		return ScreenObservation{State: ScreenWorking}
	}
	return ScreenObservation{State: ScreenIdle}
}

// matchesAnyScreenPattern は窓のどれかの行がどれかのパターンに一致するかを返す
// (現行版の `grep -E -q`)。
func matchesAnyScreenPattern(patterns []string, window []string) bool {
	for _, pattern := range patterns {
		re, ok := compileScreenPattern(pattern)
		if !ok {
			continue
		}
		for _, line := range window {
			if re.MatchString(line) {
				return true
			}
		}
	}
	return false
}

// firstScreenMatch は最初にヒットしたパターンの最初のヒット行を、先頭の空白を
// 除いて返す(現行版の `grep -E -m 1` + `sed 's/^[[:space:]]*//'`)。
//
// ヒット行が空文字の場合はそのパターンを不一致として扱い、次のパターンへ進む。
// 現行版はコマンド置換の結果を `[[ -n "$line" ]]` で見ており、空行への一致は
// blocked として成立しない(evidence §1-3)。
func firstScreenMatch(patterns []string, window []string) (string, bool) {
	for _, pattern := range patterns {
		re, ok := compileScreenPattern(pattern)
		if !ok {
			continue
		}
		for _, line := range window {
			if !re.MatchString(line) {
				continue
			}
			if line == "" {
				break
			}
			return strings.TrimLeftFunc(line, isScreenBlank), true
		}
	}
	return "", false
}

// compileScreenPattern はパターンを POSIX ERE として組み立てる。
//
// 空のパターン(現行版の `[[ -z "$pattern" ]] && continue`)と、正規表現として
// 壊れているものは ok=false を返して飛ばす。grep -E も不正な正規表現では
// 非ゼロ終了し、`2>/dev/null` と `|| true` で不一致に潰れる。
func compileScreenPattern(pattern string) (*regexp.Regexp, bool) {
	if pattern == "" {
		return nil, false
	}
	re, err := regexp.CompilePOSIX(pattern)
	if err != nil {
		return nil, false
	}
	return re, true
}
