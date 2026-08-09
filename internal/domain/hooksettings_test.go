package domain_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestSwitchHookCommandsOnConductorSettings は、現行 claude-conductor の
// hooks.json を install.sh と同じ `jq '.hooks = ... + $hooks'` でマージした
// 実物の settings.json(testdata)に対して動くことを確認する。
//
// 期待値は sed で 3 種類のスクリプトパスを置換して作った。差分は 4 行のみである。
func TestSwitchHookCommandsOnConductorSettings(t *testing.T) {
	t.Parallel()

	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatalf("ReadFile(%s) = %v", name, err)
		}
		return b
	}
	before := read("settings-conductor-merged.json")
	want := read("settings-conductor-merged.switched.json")

	out, changes, err := domain.SwitchHookCommands(before)
	if err != nil {
		t.Fatalf("SwitchHookCommands() = %v", err)
	}
	if string(out) != string(want) {
		t.Errorf("出力が期待と異なる:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}

	// jq はインデント 2 でキー順を保つ。書き戻しをしていないことの確認として、
	// 触っていない箇所がそのまま残っていることを見る。
	wantEvents := []string{"Notification", "Stop", "PostToolUse", "UserPromptSubmit"}
	if len(changes) != len(wantEvents) {
		t.Fatalf("変更一覧 = %+v, want %d 件", changes, len(wantEvents))
	}
	for i, event := range wantEvents {
		if changes[i].Event != event {
			t.Errorf("changes[%d].Event = %q, want %q", i, changes[i].Event, event)
		}
	}
}

// settingsBefore は install.sh が hooks.json をマージした直後の settings.json を
// 模したものである。次の 3 点をわざと入れてある。
//   - hooks 以外のキー(permissions / model)と、アルファベット順ではないキー順
//   - インデント 4(現行の jq 出力はインデント 2 なので、書き戻しが起きれば壊れる)
//   - .hooks の外にある同じスクリプトパス(permissions.allow)
const settingsBefore = `{
    "model": "opus",
    "hooks": {
        "Notification": [
            {
                "matcher": "",
                "hooks": [
                    {
                        "type": "command",
                        "command": "MSG=$(cat | jq -r '.message // \"Needs attention\"' 2>/dev/null); REPO=$(basename \"$(pwd)\"); terminal-notifier -title \"Claude Code: $REPO\" -message \"$MSG\" -sound default"
                    },
                    {
                        "type": "command",
                        "command": "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh"
                    }
                ]
            }
        ],
        "Stop": [
            {
                "matcher": "",
                "hooks": [
                    {
                        "type": "command",
                        "command": "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh"
                    }
                ]
            }
        ],
        "PostToolUse": [
            {
                "matcher": "",
                "hooks": [
                    {
                        "type": "command",
                        "command": "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-post-tool.sh"
                    }
                ]
            }
        ],
        "UserPromptSubmit": [
            {
                "matcher": "",
                "hooks": [
                    {
                        "type": "command",
                        "command": "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-resolve.sh"
                    }
                ]
            }
        ]
    },
    "permissions": {
        "allow": [
            "Bash(${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh)"
        ]
    }
}
`

// settingsAfter は settingsBefore を切り替えた結果である。
// 4 箇所のコマンド文字列以外は 1 バイトも変わらない。
const settingsAfter = `{
    "model": "opus",
    "hooks": {
        "Notification": [
            {
                "matcher": "",
                "hooks": [
                    {
                        "type": "command",
                        "command": "MSG=$(cat | jq -r '.message // \"Needs attention\"' 2>/dev/null); REPO=$(basename \"$(pwd)\"); terminal-notifier -title \"Claude Code: $REPO\" -message \"$MSG\" -sound default"
                    },
                    {
                        "type": "command",
                        "command": "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify"
                    }
                ]
            }
        ],
        "Stop": [
            {
                "matcher": "",
                "hooks": [
                    {
                        "type": "command",
                        "command": "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify"
                    }
                ]
            }
        ],
        "PostToolUse": [
            {
                "matcher": "",
                "hooks": [
                    {
                        "type": "command",
                        "command": "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook post-tool"
                    }
                ]
            }
        ],
        "UserPromptSubmit": [
            {
                "matcher": "",
                "hooks": [
                    {
                        "type": "command",
                        "command": "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook resolve"
                    }
                ]
            }
        ]
    },
    "permissions": {
        "allow": [
            "Bash(${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh)"
        ]
    }
}
`

func TestSwitchHookCommandsReplacesFourCommands(t *testing.T) {
	t.Parallel()

	out, changes, err := domain.SwitchHookCommands([]byte(settingsBefore))
	if err != nil {
		t.Fatalf("SwitchHookCommands() = %v", err)
	}

	// キー順・インデント・未知キー・terminal-notifier のインラインコマンド・
	// .hooks の外にある同じパスがすべてそのままであることを、
	// バイト列の完全一致で確認する。
	if string(out) != settingsAfter {
		t.Errorf("出力が期待と異なる:\n--- got ---\n%s\n--- want ---\n%s", out, settingsAfter)
	}

	want := []domain.HookCommandChange{
		{
			Event:  "Notification",
			Before: "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh",
			After:  "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify",
		},
		{
			Event:  "Stop",
			Before: "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh",
			After:  "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify",
		},
		{
			Event:  "PostToolUse",
			Before: "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-post-tool.sh",
			After:  "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook post-tool",
		},
		{
			Event:  "UserPromptSubmit",
			Before: "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-resolve.sh",
			After:  "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook resolve",
		},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Errorf("変更一覧 = %+v\nwant %+v", changes, want)
	}
}

func TestSwitchHookCommandsIsIdempotent(t *testing.T) {
	t.Parallel()

	once, _, err := domain.SwitchHookCommands([]byte(settingsBefore))
	if err != nil {
		t.Fatalf("1 回目 = %v", err)
	}
	twice, changes, err := domain.SwitchHookCommands(once)
	if err != nil {
		t.Fatalf("2 回目 = %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("2 回目の変更一覧 = %+v, want 空", changes)
	}
	if string(twice) != string(once) {
		t.Errorf("2 回目で内容が変わった:\n%s", twice)
	}
}

func TestSwitchHookCommandsPreservesPrefix(t *testing.T) {
	t.Parallel()

	// mdev-test の worktree 隔離が効くよう、コマンドの前置きは維持して
	// 末尾のスクリプトパスだけを差し替える。前置きが絶対パスでも同じ規則で動く。
	tests := []struct {
		name   string
		before string
		after  string
	}{
		{
			name:   "環境変数展開の前置き",
			before: "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh",
			after:  "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify",
		},
		{
			name:   "絶対パスの前置き",
			before: "/Users/x/.claude-conductor/scripts/pending-post-tool.sh",
			after:  "/Users/x/.claude-conductor/bin/mdev hook post-tool",
		},
		{
			name:   "$CONDUCTOR_HOME の前置き",
			before: "$CONDUCTOR_HOME/scripts/pending-resolve.sh",
			after:  "$CONDUCTOR_HOME/bin/mdev hook resolve",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := `{"hooks":{"Stop":[{"hooks":[{"command":"` + tt.before + `"}]}]}}`
			want := `{"hooks":{"Stop":[{"hooks":[{"command":"` + tt.after + `"}]}]}}`

			out, changes, err := domain.SwitchHookCommands([]byte(in))
			if err != nil {
				t.Fatalf("SwitchHookCommands() = %v", err)
			}
			if string(out) != want {
				t.Errorf("出力 = %s, want %s", out, want)
			}
			if len(changes) != 1 || changes[0].Before != tt.before || changes[0].After != tt.after {
				t.Errorf("変更一覧 = %+v", changes)
			}
		})
	}
}

func TestSwitchHookCommandsLeavesUnrelatedContentUntouched(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
	}{
		{
			name: "hooks が無い",
			in:   `{"permissions":{"allow":["Bash(x)"]}}`,
		},
		{
			name: "hooks の外にある同じパス",
			in:   `{"scripts":"$CONDUCTOR_HOME/scripts/pending-notify.sh","hooks":{}}`,
		},
		{
			name: "hooks が null",
			in:   `{"hooks":null}`,
		},
		{
			name: "対象パスがキーとして現れる",
			in:   `{"hooks":{"$CONDUCTOR_HOME/scripts/pending-notify.sh":[]}}`,
		},
		{
			name: "command 以外のフィールド",
			in:   `{"hooks":{"Stop":[{"note":"$C/scripts/pending-notify.sh"}]}}`,
		},
		{
			name: "conductor と無関係の hook",
			in:   `{"hooks":{"Stop":[{"hooks":[{"command":"echo hi"}]}]}}`,
		},
		{
			name: "似ているが一致しないパス",
			in:   `{"hooks":{"Stop":[{"hooks":[{"command":"$X/scripts/pending-notify.sh --flag"}]}]}}`,
		},
		{
			name: "トップレベルがオブジェクトではない",
			in:   `[1,2,3]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, changes, err := domain.SwitchHookCommands([]byte(tt.in))
			if err != nil {
				t.Fatalf("SwitchHookCommands() = %v", err)
			}
			if len(changes) != 0 {
				t.Errorf("変更一覧 = %+v, want 空", changes)
			}
			if string(out) != tt.in {
				t.Errorf("出力 = %s, want %s(入力のまま)", out, tt.in)
			}
		})
	}
}

func TestSwitchHookCommandsRejectsBrokenJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
	}{
		{name: "空", in: ``},
		{name: "途中で切れている", in: `{"hooks":{"Stop":[`},
		{name: "閉じ括弧が無い", in: `{"hooks":{}`},
		{name: "JSON ではない", in: `hooks = {}`},
		{name: "余分な後続データ", in: `{"hooks":{}} {}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, _, err := domain.SwitchHookCommands([]byte(tt.in)); err == nil {
				t.Error("SwitchHookCommands() = nil, want エラー")
			}
		})
	}
}

func TestSwitchHookCommandsDoesNotAliasInput(t *testing.T) {
	t.Parallel()

	// 呼び出し側がバックアップ用に保持している入力を壊さないこと。
	in := []byte(`{"hooks":{"Stop":[{"hooks":[{"command":"$C/scripts/pending-resolve.sh"}]}]}}`)
	original := string(in)

	if _, _, err := domain.SwitchHookCommands(in); err != nil {
		t.Fatalf("SwitchHookCommands() = %v", err)
	}
	if string(in) != original {
		t.Errorf("入力が書き換えられた: %s", in)
	}
}

func TestRestoreHookCommandsReversesSwitch(t *testing.T) {
	t.Parallel()

	out, changes, err := domain.RestoreHookCommands([]byte(settingsAfter))
	if err != nil {
		t.Fatalf("RestoreHookCommands() = %v", err)
	}
	if string(out) != settingsBefore {
		t.Errorf("出力が期待と異なる:\n--- got ---\n%s\n--- want ---\n%s", out, settingsBefore)
	}

	want := []domain.HookCommandChange{
		{
			Event:  "Notification",
			Before: "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify",
			After:  "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh",
		},
		{
			Event:  "Stop",
			Before: "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook notify",
			After:  "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-notify.sh",
		},
		{
			Event:  "PostToolUse",
			Before: "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook post-tool",
			After:  "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-post-tool.sh",
		},
		{
			Event:  "UserPromptSubmit",
			Before: "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev hook resolve",
			After:  "${CONDUCTOR_HOME:-$HOME/.claude-conductor}/scripts/pending-resolve.sh",
		},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Errorf("変更一覧 = %+v\nwant %+v", changes, want)
	}
}

// TestSwitchRestoreRoundTripIsIdentity は switch → restore の往復が
// バイト単位で恒等であることを固定する。
//
// restore がバックアップの全文ではなく現在の内容の逆変換で戻せるのは、
// この性質があるからである。逆向き(restore → switch)も同時に見る。
//
// ただし対象の文字列リテラルが `\/` のような非正規なエスケープで
// 書かれていた場合は、1 回目の変換で素直な表記へ正規化されるため恒等にならない
// (TestSwitchHookCommandsHandlesEscapedTargets で正規化そのものを固定している)。
func TestSwitchRestoreRoundTripIsIdentity(t *testing.T) {
	t.Parallel()

	conductor, err := os.ReadFile(filepath.Join("testdata", "settings-conductor-merged.json"))
	if err != nil {
		t.Fatalf("ReadFile() = %v", err)
	}

	tests := []struct {
		name string
		in   string
	}{
		{name: "install.sh がマージした実物", in: string(conductor)},
		{name: "インデント 4 と未知キー", in: settingsBefore},
		{name: "1 行の最小構成", in: `{"hooks":{"Stop":[{"hooks":[{"command":"$C/scripts/pending-notify.sh"}]}]}}`},
		{name: "絶対パスの前置き", in: `{"hooks":{"Stop":[{"hooks":[{"command":"/Users/x/.claude-conductor/scripts/pending-post-tool.sh"}]}]}}`},
		{name: "対象が 1 つも無い", in: `{"hooks":{"Stop":[{"hooks":[{"command":"echo hi"}]}]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			switched, _, err := domain.SwitchHookCommands([]byte(tt.in))
			if err != nil {
				t.Fatalf("SwitchHookCommands() = %v", err)
			}
			back, _, err := domain.RestoreHookCommands(switched)
			if err != nil {
				t.Fatalf("RestoreHookCommands() = %v", err)
			}
			if string(back) != tt.in {
				t.Errorf("switch → restore が恒等でない:\n--- got ---\n%s\n--- want ---\n%s", back, tt.in)
			}

			// 逆向きも同じく恒等である。切り替え済みの settings.json を
			// restore → switch しても元へ戻る。
			again, _, err := domain.SwitchHookCommands(back)
			if err != nil {
				t.Fatalf("2 回目の SwitchHookCommands() = %v", err)
			}
			if string(again) != string(switched) {
				t.Errorf("restore → switch が恒等でない:\n--- got ---\n%s\n--- want ---\n%s", again, switched)
			}
		})
	}
}

func TestRestoreHookCommandsIsIdempotent(t *testing.T) {
	t.Parallel()

	once, _, err := domain.RestoreHookCommands([]byte(settingsAfter))
	if err != nil {
		t.Fatalf("1 回目 = %v", err)
	}
	twice, changes, err := domain.RestoreHookCommands(once)
	if err != nil {
		t.Fatalf("2 回目 = %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("2 回目の変更一覧 = %+v, want 空", changes)
	}
	if string(twice) != string(once) {
		t.Errorf("2 回目で内容が変わった:\n%s", twice)
	}
}

func TestRestoreHookCommandsLeavesUnrelatedContentUntouched(t *testing.T) {
	t.Parallel()

	// switch と同じ限定条件が逆向きの規則でも効いていること。
	tests := []struct {
		name string
		in   string
	}{
		{
			name: "hooks の外にある mdev 呼び出し",
			in:   `{"scripts":"$C/bin/mdev hook notify","hooks":{}}`,
		},
		{
			name: "command 以外のフィールド",
			in:   `{"hooks":{"Stop":[{"note":"$C/bin/mdev hook notify"}]}}`,
		},
		{
			name: "似ているが一致しないコマンド",
			in:   `{"hooks":{"Stop":[{"hooks":[{"command":"$C/bin/mdev hook notify --flag"}]}]}}`,
		},
		{
			name: "既にスクリプトを指している",
			in:   `{"hooks":{"Stop":[{"hooks":[{"command":"$C/scripts/pending-notify.sh"}]}]}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, changes, err := domain.RestoreHookCommands([]byte(tt.in))
			if err != nil {
				t.Fatalf("RestoreHookCommands() = %v", err)
			}
			if len(changes) != 0 {
				t.Errorf("変更一覧 = %+v, want 空", changes)
			}
			if string(out) != tt.in {
				t.Errorf("出力 = %s, want %s(入力のまま)", out, tt.in)
			}
		})
	}
}

func TestRestoreHookCommandsRejectsBrokenJSON(t *testing.T) {
	t.Parallel()

	if _, _, err := domain.RestoreHookCommands([]byte(`{"hooks":`)); err == nil {
		t.Error("RestoreHookCommands() = nil, want エラー")
	}
}

func TestRestoreHookCommandsDoesNotAliasInput(t *testing.T) {
	t.Parallel()

	in := []byte(`{"hooks":{"Stop":[{"hooks":[{"command":"$C/bin/mdev hook resolve"}]}]}}`)
	original := string(in)

	if _, _, err := domain.RestoreHookCommands(in); err != nil {
		t.Fatalf("RestoreHookCommands() = %v", err)
	}
	if string(in) != original {
		t.Errorf("入力が書き換えられた: %s", in)
	}
}

func TestSwitchHookCommandsHandlesEscapedTargets(t *testing.T) {
	t.Parallel()

	// JSON としては同じ値でも表記が違う(`\/` と `$` によるエスケープ)場合に、
	// デコードした値で照合し、素直な表記へ書き戻す。
	in := `{"hooks":{"Stop":[{"hooks":[{"command":"$C\/scripts\/pending-notify.sh"}]}]}}`
	want := `{"hooks":{"Stop":[{"hooks":[{"command":"$C/bin/mdev hook notify"}]}]}}`

	out, changes, err := domain.SwitchHookCommands([]byte(in))
	if err != nil {
		t.Fatalf("SwitchHookCommands() = %v", err)
	}
	if string(out) != want {
		t.Errorf("出力 = %s, want %s", out, want)
	}
	if len(changes) != 1 {
		t.Fatalf("変更一覧 = %+v, want 1 件", changes)
	}
}
