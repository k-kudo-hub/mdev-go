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
// 統合 6-3 はこのコピーを知らないまま `scripts/` を消したため、写された hook が
// すべて exit 127 になり、codex の会話にエラーが出続けた(実環境で発生)。
//
// # mdev は直さない。知らせるだけである
//
// **このファイルは利用者が意図して置いた設定かもしれない。** 消すのも書き換える
// のも mdev の裁量を越える。理由は 3 つある。
//
//   - codex の hooks エンジンで mdev hook がどう動くかを検証していない。
//     スクリーン検出と二重に pending を更新する懸念がある
//   - codex は hooks.json の内容を trusted_hash として覚えており、書き換えると
//     再信頼の確認が利用者に出る。こちらの都合でそれを起こさない
//   - 消してよいかどうかは利用者にしか決められない。install が黙って消すと、
//     意図して置いた設定が次の install で消える作りになる
//
// できるのは「壊れていることと、直し方の選択肢」を伝えるところまでである。

// CodexHooksFileName は codex が読む hooks のコピーの名前である。
const CodexHooksFileName = "hooks.json"

// conductorScriptsMarker は撤去済みのスクリプト置き場を指す目印である。
const conductorScriptsMarker = "/scripts/"

// conductorHomeMarkers は conductor の設置場所を指す書き方である。
//
// 環境変数の展開形と、展開済みの絶対パスの両方を受ける。前者は hooks.json が
// settings.json から素直に写された場合、後者は写す側が展開してしまった場合に
// 現れる。
var conductorHomeMarkers = []string{"CONDUCTOR_HOME", "/" + conductorHomeDirName + "/"}

// conductorHomeDirName は CONDUCTOR_HOME 未設定時のディレクトリ名である。
const conductorHomeDirName = ".claude-conductor"

// BrokenCodexHooks は codex の hooks.json のうち、**撤去済みのスクリプトを
// 指している** hook を返す。
//
// これが空でなければ、codex の会話でその hook が exit 127 になる。
//
// 見るのは「conductor の scripts/ を指しているか」だけである。Go 版の
// `bin/mdev hook ...` を指すものは exit 0 で動くので対象にしない(動いている
// ものを壊れていると言わない)。conductor と無関係な hook も当然対象外である。
//
// 上位の形は 2 通りを受ける。`{"hooks": {...}}` と、イベント名が直に並ぶ
// `{...}` である。codex 側がどちらで写すかは版によって変わりうるので、
// 見分けずに両方を扱う。
func BrokenCodexHooks(data []byte) ([]HookCommand, error) {
	if !json.Valid(data) {
		return nil, errors.New("hooks.json として解釈できる JSON ではありません")
	}
	if !isJSONObject(data) {
		return nil, errors.New("hooks.json のトップレベルがオブジェクトではありません")
	}

	events, err := codexHookEvents(data)
	if err != nil {
		return nil, err
	}

	var broken []HookCommand
	for _, name := range sortedEventNames(events) {
		for _, command := range codexEventCommands(events[name]) {
			if referencesRemovedScripts(command) {
				broken = append(broken, HookCommand{Event: name, Command: command})
			}
		}
	}
	return broken, nil
}

// referencesRemovedScripts は撤去済みのスクリプトを指しているかを返す。
func referencesRemovedScripts(command string) bool {
	if !strings.Contains(command, conductorScriptsMarker) {
		return false
	}
	for _, marker := range conductorHomeMarkers {
		if strings.Contains(command, marker) {
			return true
		}
	}
	return false
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
// 配列が入る。解釈できない形は「コマンドが無い」として扱う(壊れていると
// 断定できないので、黙っている側へ倒れる)。
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

// RenderCodexHooksWarning は壊れた hook の事実と直し方を組み立てる。
//
// **どちらを選ぶかは利用者が決める。** mdev はどちらも代行しない。
func RenderCodexHooksWarning(path string, broken []HookCommand) []string {
	events := make([]string, 0, len(broken))
	for _, hook := range broken {
		events = append(events, hook.Event)
	}
	return []string{
		path + " の hooks が削除済みのスクリプトを参照しています(" +
			strings.Join(events, ", ") + ")。codex の会話で hook エラー(127)になります。",
		"この hooks が不要なら削除してください: rm " + path,
		"  (mdev は codex をスクリーン検出で扱うため、消しても動作に影響はありません)",
		"使いたい場合は該当コマンドを " +
			"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook <resolve|post-tool|notify> " +
			"へ手動で更新してください。",
		"  (codex 側で再信頼の確認が表示されます)",
	}
}
