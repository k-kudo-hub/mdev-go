package app

import (
	"errors"
	"fmt"
)

// ErrNoAgentCommand は起動するコマンドが決まらなかったことを表す。
//
// 設定の `.agent.command` が空白だけ、というときにここへ来る。空なら
// "claude" に落ちるが、空白だけは「書いてあるが語が無い」ので落とし先が無い。
var ErrNoAgentCommand = errors.New("起動するエージェントのコマンドが空です")

// ProcessExecer は今のプロセスを別のコマンドへ置き換える。
type ProcessExecer interface {
	// Exec は command を実行し、**戻らない**。失敗したときだけ error を返す。
	//
	// 新しいプロセスを起こして待つのではなく置き換えるのは、zellij のペインが
	// 見ているのがこのプロセスだからである。間に 1 段挟むと、ペインを閉じた
	// ときの signal がエージェントへ届かず、置き換えれば要らない中継役が
	// エージェントの生存期間ずっと居座る。
	Exec(command []string) error
}

// AgentLauncher は設定されたエージェント CLI を起動するユースケースである
// (現行 agent-launch.sh 相当)。
//
// dev.kdl の Agent ペインから呼ばれる。静的な KDL は config.json を読めない
// ため、レイアウトからは常にこのコマンドを指し、どの CLI を起こすかは
// こちらが設定から決める。
//
// タスクタブはこの経路を通らない。あちらは new-tab のコマンド行を
// TaskCreator が組み立てており、名前付きエージェントの選択もそこで行う。
type AgentLauncher struct {
	Config ConfigLoader
	Execer ProcessExecer
}

// Launch は設定されたエージェントへプロセスを置き換える。成功すれば戻らない。
//
// 設定を読めなかった場合も既定の "claude" で進む。現行版も
// `jq ... 2>/dev/null` で失敗を握り潰して既定へ落ちており、設定の不備で
// ペインが空になるより、既定のエージェントが立ち上がるほうがよい。
//
// 名前付きエージェント(.agents)は見ない。現行版が引数無しで agent_command を
// 呼んでおり、参照するのは `.agent.command` だけである。
func (l *AgentLauncher) Launch() error {
	config, _ := l.Config.Load()
	command := config.AgentCommand("")
	if len(command) == 0 {
		return ErrNoAgentCommand
	}
	if err := l.Execer.Exec(command); err != nil {
		return fmt.Errorf("エージェント %q の起動に失敗しました: %w", command[0], err)
	}
	return nil
}
