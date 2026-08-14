package assets_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		"init.zsh",
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

// TestDomainTestdataMatchesAssets は domain のテストが使う写しが本物と
// 一致することを確かめる。
//
// domain は assets を import できない(ADR-0002 の依存方向)ため、hooks の
// 雛形は testdata へ写してある。写しが古くなると、テストは通るのに配布物は
// 別物という状態になる。
func TestDomainTestdataMatchesAssets(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"hooks.json", "config.default.json"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want, ok := assets.Read(name)
			if !ok {
				t.Fatalf("%s が埋め込まれていない", name)
			}
			got, err := os.ReadFile(domainTestdataPath(name))
			if err != nil {
				t.Fatalf("写しが読めない: %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("写しが本物と違う: %s", domainTestdataPath(name))
			}
		})
	}
}

// domainTestdataPath は domain のテストが読む写しの場所を返す。
func domainTestdataPath(name string) string {
	if name == "config.default.json" {
		return filepath.Join("..", "internal", "domain", "testdata", "golden-config-merge", name)
	}
	return filepath.Join("..", "internal", "domain", "testdata", name)
}

// TestInitZshIsAShim は入口が中身を持たないことを確かめる。
//
// 関数の定義がここに戻ってくると、バイナリを更新しても古い関数が動き続ける
// (機能を足すたびに入口の書き換えが要る、という元の問題に逆戻りする)。
func TestInitZshIsAShim(t *testing.T) {
	t.Parallel()

	b, ok := assets.Read("init.zsh")
	if !ok {
		t.Fatal("init.zsh が埋め込まれていない")
	}
	body := string(b)

	if !strings.Contains(body, `mdev" init zsh`) {
		t.Errorf("mdev init zsh を呼んでいない:\n%s", body)
	}
	// mdev は PATH 上のバイナリが受ける。同名の関数を定義してはいけない。
	for _, forbidden := range []string{"mdev()", "dev()", "zs()", "_conductor_session_name"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("入口が %q を定義している:\n%s", forbidden, body)
		}
	}
	if strings.Contains(body, "/scripts/") {
		t.Errorf("Shell スクリプトを呼んでいる:\n%s", body)
	}
}
