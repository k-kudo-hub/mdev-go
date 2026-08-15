package domain

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// codex 0.147 は Claude Code 互換の hooks エンジンを内蔵しており、Codex アプリの
// external-agent import が `~/.claude/settings.json` の hooks を
// `$CODEX_HOME/hooks.json` へ **コピーする**。
//
// mdev はこのコピーを知らないまま `scripts/` を消したため、コピーされた hook が
// すべて exit 127 になり、codex の会話にエラーが出続けた(実環境で発生)。
//
// # なぜ書き換えではなく削除か
//
// codex は hooks.json の内容を `trusted_hash` として config.toml に覚えている。
// 中身を書き換えるとその照合に失敗し、利用者に再信頼の確認を求める。**mdev は
// codex を hook ではなくスクリーン検出で扱う**ので、このコピーはそもそも要らない。
// 要らないものを残して確認を出させるより、消すほうが筋が通る。
//
// # config.toml の [hooks.state] は触らない
//
// 参照先のファイルが消えれば、その項目は codex から見て単なる残骸になる
// (存在しない hook の信頼記録なので、何も起こさない)。TOML を書き換える手間と
// 壊す危険に見合わない。

// CodexHooksFileName は codex が読む hooks のコピーの名前である。
const CodexHooksFileName = "hooks.json"

// CodexHooksVerdict は hooks.json をどう扱うべきかである。
type CodexHooksVerdict int

const (
	// CodexHooksNone は hook が 1 つも無い(消すものが無い)ことを表す。
	CodexHooksNone CodexHooksVerdict = iota
	// CodexHooksAllConductor はすべてが conductor 由来で、消してよいことを表す。
	CodexHooksAllConductor
	// CodexHooksMixed は conductor 以外が混ざっており、触ってはならないことを表す。
	CodexHooksMixed
)

// CodexHooksReport は hooks.json の検査結果である。
type CodexHooksReport struct {
	// Verdict はどう扱うべきか。
	Verdict CodexHooksVerdict
	// Conductor は conductor 由来と判定した hook である(警告の列挙に使う)。
	Conductor []HookCommand
	// Foreign は conductor 由来でない hook である。
	Foreign []HookCommand
}

// InspectCodexHooks は codex の hooks.json を検査する。
//
// **conductor 由来かどうかだけを見る。** 判定は 2 つの形を対象にする。
//
//   - 旧 Shell 版: コマンドに `/scripts/pending-` を含む
//   - Go 版: コマンドが `bin/mdev hook <名前>` で終わる
//
// どちらでもない hook が 1 つでもあれば Mixed とし、呼び出し側は触らない。
// 利用者や他のツールが足したものを巻き添えにしないためである。
//
// 上位の形は 2 通りを受ける。`{"hooks": {...}}` と、イベント名が直に並ぶ
// `{...}` である。codex 側がどちらで写すかは版によって変わりうるので、
// 見分けずに両方を扱う。
func InspectCodexHooks(data []byte) (CodexHooksReport, error) {
	if !json.Valid(data) {
		return CodexHooksReport{}, errors.New("hooks.json として解釈できる JSON ではありません")
	}
	// null や配列でも json.Valid は通る。イベントの対応表として読めない形は
	// 「conductor 由来だと判定できない」ので、触らない側へ倒すためにエラーにする。
	if !isJSONObject(data) {
		return CodexHooksReport{}, errors.New("hooks.json のトップレベルがオブジェクトではありません")
	}

	events, err := codexHookEvents(data)
	if err != nil {
		return CodexHooksReport{}, err
	}

	var report CodexHooksReport
	for _, name := range sortedEventNames(events) {
		for _, command := range codexEventCommands(events[name]) {
			entry := HookCommand{Event: name, Command: command}
			if isConductorHookCommand(command) {
				report.Conductor = append(report.Conductor, entry)
				continue
			}
			report.Foreign = append(report.Foreign, entry)
		}
	}

	switch {
	case len(report.Conductor) == 0 && len(report.Foreign) == 0:
		report.Verdict = CodexHooksNone
	case len(report.Foreign) > 0:
		report.Verdict = CodexHooksMixed
	default:
		report.Verdict = CodexHooksAllConductor
	}
	return report, nil
}

// codexHookEvents は上位の形を吸収してイベントの対応表を返す。
func codexHookEvents(data []byte) (map[string]json.RawMessage, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, errors.New("hooks.json のトップレベルがオブジェクトではありません")
	}
	// `{"hooks": {...}}` で写されている場合はその中を見る。
	if nested, ok := doc[hooksKey]; ok {
		var events map[string]json.RawMessage
		if err := json.Unmarshal(nested, &events); err == nil {
			return events, nil
		}
	}
	return doc, nil
}

// sortedEventNames はイベント名を昇順で返す(表示と比較を安定させる)。
func sortedEventNames(events map[string]json.RawMessage) []string {
	names := make([]string, 0, len(events))
	for name := range events {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// codexEventCommands はイベント 1 件に含まれるコマンド文字列を順に返す。
//
// 形は Claude Code の settings.json と同じで、matcher の配列の中に hook の
// 配列が入る。解釈できない形は「コマンドが無い」として扱う(呼び出し側は
// conductor 由来だと判定できないので触らない側へ倒れる)。
func codexEventCommands(raw json.RawMessage) []string {
	var matchers []struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &matchers); err != nil {
		return nil
	}
	var out []string
	for _, matcher := range matchers {
		for _, hook := range matcher.Hooks {
			out = append(out, hook.Command)
		}
	}
	return out
}

// isConductorHookCommand は conductor が置いた hook かどうかを返す。
func isConductorHookCommand(command string) bool {
	return strings.Contains(command, pendingScriptMarker) || callsMdevHook(command)
}

// RenderCodexHooksWarning は触らなかったときの案内を組み立てる。
func RenderCodexHooksWarning(path string, conductor []HookCommand) string {
	names := make([]string, 0, len(conductor))
	for _, hook := range conductor {
		names = append(names, hook.Event)
	}
	return path + " に conductor 由来の hook が残っています(" +
		strings.Join(names, ", ") + ")。" +
		"他のツールの hook と混ざっているため触っていません。手で取り除いてください。"
}
