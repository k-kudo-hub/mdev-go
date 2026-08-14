package assets_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/assets"
)

// TestNames は埋め込まれている資産の一覧を確かめる。
//
// ここが増減するのは配布物の中身が変わるときだけなので、名前を直接固定する。
func TestNames(t *testing.T) {
	t.Parallel()

	want := []string{
		"config.default.json",
		"hooks.json",
		"layouts/dev.kdl",
		"layouts/multi.kdl",
	}
	if got := assets.Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("Names = %q, want %q", got, want)
	}
}

// TestReadUnknown は無い名前を求められたときを確かめる。
func TestReadUnknown(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", "layouts", "missing.json", "../assets.go"} {
		if _, ok := assets.Read(name); ok {
			t.Errorf("Read(%q) が見つかったと答えた", name)
		}
	}
}

// TestDefaultConfigIsValidJSON は同梱の設定が JSON として読めることを
// 確かめる。壊れたまま配ると、設定を置いていない利用者が全員動かなくなる。
func TestDefaultConfigIsValidJSON(t *testing.T) {
	t.Parallel()

	b, ok := assets.Read("config.default.json")
	if !ok {
		t.Fatal("config.default.json が埋め込まれていない")
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatalf("JSON として読めない: %v", err)
	}
	for _, key := range []string{"agent", "pricing"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("%q が無い", key)
		}
	}
}

// TestHooksIsValidJSON は同梱の hooks 雛形が JSON として読めることを確かめる。
func TestHooksIsValidJSON(t *testing.T) {
	t.Parallel()

	b, ok := assets.Read("hooks.json")
	if !ok {
		t.Fatal("hooks.json が埋め込まれていない")
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatalf("JSON として読めない: %v", err)
	}
	// settings.json の `.hooks` へそのまま入る形なので、最上位は
	// イベント名(Notification / Stop の類)である。
	for _, event := range []string{"Notification", "Stop", "PostToolUse", "UserPromptSubmit"} {
		if _, ok := fields[event]; !ok {
			t.Errorf("%q が無い", event)
		}
	}
}

// TestLayoutsPointAtMdev はレイアウトが Shell スクリプトを呼ばないことを
// 確かめる。
//
// このフェーズの目的そのものである。1 か所でも scripts/ が残ると、
// scripts/ を配らなくなった時点でそのペインだけが起動しなくなる。
func TestLayoutsPointAtMdev(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// wants は必ず現れるコマンド。
		wants []string
	}{
		{
			name: "layouts/multi.kdl",
			wants: []string{
				`\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\" pane dashboard`,
				`\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\" pane waiting`,
				`\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\" pane done`,
				`\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\" pane news`,
				`\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\" pane task-create`,
			},
		},
		// パスは引用符で囲む。HOME に空白が入っていると、囲まずに書いた
		// 場合に bash が語分割してコマンドが見つからなくなる(現行版の
		// dev.kdl も `bash "..."` と囲んでいる)。
		{name: "layouts/dev.kdl", wants: []string{`\"${CONDUCTOR_HOME:-$HOME/.claude-conductor}/bin/mdev\" agent launch`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, ok := assets.Read(tt.name)
			if !ok {
				t.Fatalf("%s が埋め込まれていない", tt.name)
			}
			body := string(b)
			if strings.Contains(body, "/scripts/") {
				t.Errorf("Shell スクリプトの呼び出しが残っている:\n%s", body)
			}
			for _, want := range tt.wants {
				if !strings.Contains(body, want) {
					t.Errorf("%q が無い:\n%s", want, body)
				}
			}
		})
	}
}
