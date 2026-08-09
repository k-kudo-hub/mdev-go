package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/k-kudo-hub/mdev-go/internal/cli"
	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestSwitchedHookCommandsMatchCobraSubCommands は、settings.json へ書き込む
// コマンド文字列に現れるサブコマンド名が、mdev が実際に受け付ける子コマンドと
// 一致していることを確かめる。
//
// 置換規則(internal/domain)と cobra のコマンドツリー(internal/cli)は
// 互いを参照しないため、片方だけを直しても両方ともコンパイルが通り、
// それぞれのパッケージのテストも緑のままになる。そして実環境では
// 「hooks は書き換わったが hook 実行時に unknown command で落ち続ける」
// という形でしか現れない。ADR-0002 で全パッケージを参照できるのは
// cmd/mdev だけなので、突き合わせはここで行う。
func TestSwitchedHookCommandsMatchCobraSubCommands(t *testing.T) {
	t.Parallel()

	root := cli.NewRootCommand(cli.Deps{})

	var wantNames []string
	for _, suffix := range domain.SwitchedHookCommandSuffixes() {
		// `/bin/mdev hook notify` を「バイナリのパス」「親コマンド」
		// 「子コマンド」の 3 語として読む。
		fields := strings.Fields(suffix)
		if len(fields) != 3 {
			t.Fatalf("切り替え後のコマンド %q が「パス 親 子」の 3 語ではない", suffix)
		}
		if !strings.HasSuffix(fields[0], "/mdev") {
			t.Errorf("切り替え後のコマンド %q が mdev を指していない", suffix)
		}

		parent := findSubCommand(root, fields[1])
		if parent == nil {
			t.Fatalf("mdev に %q コマンドが無い(切り替え後のコマンド %q)", fields[1], suffix)
		}
		if findSubCommand(parent, fields[2]) == nil {
			t.Errorf("mdev %s に %q コマンドが無い(切り替え後のコマンド %q)",
				fields[1], fields[2], suffix)
		}
		if !slices.Contains(wantNames, fields[2]) {
			wantNames = append(wantNames, fields[2])
		}
	}

	// 逆向きも見る。hook のサブコマンドを増やしても置換規則を足さなければ、
	// そのイベントだけ Shell 版のまま取り残される。
	hook := findSubCommand(root, "hook")
	if hook == nil {
		t.Fatal("mdev に hook コマンドが無い")
	}
	for _, sub := range hook.Commands() {
		if !slices.Contains(wantNames, sub.Name()) {
			t.Errorf("mdev hook %s に対応する置換規則が無い", sub.Name())
		}
	}
}

// findSubCommand は name の子コマンドを返す。無ければ nil を返す。
func findSubCommand(parent *cobra.Command, name string) *cobra.Command {
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}
