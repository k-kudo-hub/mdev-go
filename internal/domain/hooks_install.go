package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// install が settings.json の hooks を整える手順は 2 段ある。
//
//  1. 足りないイベントを同梱の雛形から足す(既にあるイベントは触らない)
//  2. Shell 版を指しているコマンドを mdev の呼び出しへ書き換える
//
// 順序に意味がある。先に足してから書き換えれば、雛形が Shell 版のままでも
// 最終的に全部が mdev を指す。雛形を Shell 版のまま置いているのは、
// 「切り替えと復元の対応」を 1 か所(置換規則)に閉じ込めるためである。

// InstallHooksResult は hooks を整えた結果である。
type InstallHooksResult struct {
	// Settings は書き換え後の settings.json。
	Settings []byte
	// AddedEvents は雛形から足したイベント名(昇順)。
	AddedEvents []string
	// Changes は Shell 版から mdev へ書き換えたコマンド。
	Changes []HookCommandChange
	// RemainingScripts は規則に無い conductor スクリプトの呼び出し。
	RemainingScripts []HookCommand
}

// Changed は settings.json に手を入れたかどうかを返す。
func (r InstallHooksResult) Changed() bool {
	return len(r.AddedEvents) > 0 || len(r.Changes) > 0
}

// InstallHooks は settings.json の hooks を mdev の形へ整える。
//
// template は同梱の hooks.json(`.hooks` の中身そのもの)である。
//
// **既にあるイベントは触らない。** 利用者が自分で足した hook や、通知の
// コマンドを書き換えると、install のたびに設定が奪い合いになる。足すのは
// 雛形にあってこちらに無いイベントだけである。
//
// 書き換えはコマンド文字列のバイト範囲だけを差し替える(SwitchHookCommands)。
// キー順・インデント・未知キーは 1 バイトも変わらない。
//
// 2 回目以降は足すものも書き換えるものも無くなる(冪等)。
func InstallHooks(settings, template []byte) (InstallHooksResult, error) {
	if !json.Valid(settings) {
		return InstallHooksResult{}, errors.New("settings.json として解釈できる JSON ではありません")
	}
	if !json.Valid(template) {
		return InstallHooksResult{}, errors.New("同梱の hooks.json が壊れています")
	}

	var events map[string]json.RawMessage
	if err := json.Unmarshal(template, &events); err != nil {
		return InstallHooksResult{}, fmt.Errorf("同梱の hooks.json を読めません: %w", err)
	}

	withEvents, added, err := addMissingHookEvents(settings, events)
	if err != nil {
		return InstallHooksResult{}, err
	}

	switched, changes, err := SwitchHookCommands(withEvents)
	if err != nil {
		return InstallHooksResult{}, err
	}
	remaining, err := RemainingPendingScriptCommands(switched)
	if err != nil {
		return InstallHooksResult{}, err
	}
	return InstallHooksResult{
		Settings:         switched,
		AddedEvents:      added,
		Changes:          changes,
		RemainingScripts: remaining,
	}, nil
}

// NewHookSettings は settings.json が無いときに書く中身を返す。
//
// 雛形をそのまま `.hooks` に入れてから mdev の形へ書き換える。既存ファイルへ
// 足す経路と同じ関数を通すので、新規と移行で hooks の中身がずれない。
func NewHookSettings(template []byte) ([]byte, error) {
	empty := []byte("{}\n")
	result, err := InstallHooks(empty, template)
	if err != nil {
		return nil, err
	}
	return result.Settings, nil
}

// addMissingHookEvents は `.hooks` に無いイベントを足す。
//
// `.hooks` そのものが無ければ、トップレベルへ `.hooks` ごと足す。
func addMissingHookEvents(settings []byte, events map[string]json.RawMessage) ([]byte, []string, error) {
	root, hooks, err := scanHookObjects(settings)
	if err != nil {
		return nil, nil, err
	}

	var missing []string
	for name := range events {
		if hooks == nil || !hooks.has(name) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return bytes.Clone(settings), nil, nil
	}
	sort.Strings(missing)

	members := make([]jsonMember, 0, len(missing))
	for _, name := range missing {
		members = append(members, jsonMember{key: name, value: events[name]})
	}

	if hooks != nil {
		return applyEdits(settings, []byteEdit{insertMembers(settings, hooks.span, members)}), missing, nil
	}

	// `.hooks` ごと作る。イベントの並びは雛形の記述順が分からないので昇順にする。
	nested, err := json.Marshal(orderedHookEvents(events, missing))
	if err != nil {
		return nil, nil, fmt.Errorf("hooks の組み立てに失敗しました: %w", err)
	}
	edit := insertMembers(settings, root, []jsonMember{{key: hooksKey, value: nested}})
	return applyEdits(settings, []byteEdit{edit}), missing, nil
}

// orderedHookEvents は名前順に並べたイベントの対応表を返す。
func orderedHookEvents(events map[string]json.RawMessage, names []string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(names))
	for _, name := range names {
		out[name] = events[name]
	}
	return out
}

// hookObject は `.hooks` の位置と、そこに在るイベント名である。
type hookObject struct {
	span objectSpan
	keys map[string]struct{}
}

func (h hookObject) has(name string) bool {
	_, ok := h.keys[name]
	return ok
}

// scanHookObjects は settings を 1 回走査し、トップレベルと `.hooks` の位置を返す。
// `.hooks` が無い、またはオブジェクトでない場合は hooks が nil になる。
func scanHookObjects(settings []byte) (objectSpan, *hookObject, error) {
	dec := json.NewDecoder(bytes.NewReader(settings))

	var (
		stack   []jsonFrame
		opens   []int
		members []int
		root    objectSpan
		hooks   *hookObject
		keys    = map[string]struct{}{}
	)
	for {
		prev := dec.InputOffset()
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return objectSpan{}, nil, fmt.Errorf("settings.json の走査に失敗しました: %w", err)
		}

		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				opens = append(opens, int(prev)+bytes.IndexByte(settings[prev:], byte(d)))
				members = append(members, 0)
				stack = pushOrPop(stack, d)
			case '}', ']':
				open, count := opens[len(opens)-1], members[len(members)-1]
				opens, members = opens[:len(opens)-1], members[:len(members)-1]
				span := objectSpan{open: open, close: int(dec.InputOffset()) - 1, empty: count == 0}
				if d == '}' {
					switch {
					case len(stack) == 1:
						root = span
					case len(stack) == 2 && stack[0].isObject && stack[0].key == hooksKey:
						hooks = &hookObject{span: span, keys: keys}
					}
				}
				stack = pushOrPop(stack, d)
			}
			continue
		}

		s, isString := tok.(string)
		if isKeyPosition(stack) {
			stack[len(stack)-1].key = s
			stack[len(stack)-1].expectKey = false
			if len(members) > 0 {
				members[len(members)-1]++
			}
			if len(stack) == 2 && stack[0].isObject && stack[0].key == hooksKey {
				keys[s] = struct{}{}
			}
			continue
		}
		consumeValue(stack)
		if !isString && len(members) > 0 && !isObjectTop(stack) {
			members[len(members)-1]++
		}
	}
	return root, hooks, nil
}
