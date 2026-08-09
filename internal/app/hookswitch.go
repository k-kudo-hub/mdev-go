package app

import (
	"bytes"
	"fmt"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// HookSwitcher は Claude Code の settings.json の hooks を、現行 Shell 版の
// スクリプト呼び出しから `mdev hook` サブコマンドへ切り替える / 元へ戻す
// ユースケースである。
//
// settings.json は Claude Code 全体の設定ファイルであり、mdev の管理外の
// 設定(permissions など)も入っている。書き換えは必ず「読む → 退避 →
// 原子的に置き換える」の順で行い、退避に失敗したら書き換えない。
type HookSwitcher struct {
	Settings SettingsStore
}

// HookCommandChange は置き換える hook コマンド 1 件である。
//
// 中身は domain.HookCommandChange と同じだが、cli / tui は app にしか
// 依存できない(ADR-0002)ため、境界に出す型は app が持つ。
type HookCommandChange struct {
	// Event は `.hooks` 直下のイベント名(Notification / Stop など)。
	Event string
	// Before / After は置換前後のコマンド文字列。
	Before string
	After  string
}

// SwitchHooksResult は Switch の結果である。表示のために使う。
type SwitchHooksResult struct {
	// SettingsPath は対象の settings.json のパス。
	SettingsPath string
	// Changes は置き換えた(dry-run では置き換える予定の)コマンドの一覧。
	// 空なら既に切り替え済みで、何もしていない。
	Changes []HookCommandChange
	// BackupPath は作成したバックアップのパス。
	// dry-run のときと変更が無いときは空になる。
	BackupPath string
	// DryRun は書き込みを行わなかったことを表す。
	DryRun bool
}

// RestoreHooksResult は Restore の結果である。
type RestoreHooksResult struct {
	// SettingsPath は対象の settings.json のパス。
	SettingsPath string
	// BackupPath は復元元にしたバックアップのパス。Found が false なら空。
	BackupPath string
	// Found はバックアップが見つかったかどうか。switch を一度も実行して
	// いなければ false になる。これはエラーではなく報告すべき状態である。
	Found bool
	// Changed は現在の内容がバックアップと異なり、書き戻した
	// (dry-run では書き戻す予定である)ことを表す。
	Changed bool
	// DryRun は書き込みを行わなかったことを表す。
	DryRun bool
}

// Switch は settings.json の hooks を mdev のサブコマンドへ切り替える。
//
// 既に切り替え済みなら何もしない(冪等)。変更が無いのにバックアップを作ると、
// Restore が探す「最新のバックアップ」が切り替え後の内容になってしまうため、
// 変更がある場合にだけ退避する。
//
// dryRun が true のときは変更内容だけを返し、退避も書き込みも行わない。
func (s *HookSwitcher) Switch(dryRun bool) (SwitchHooksResult, error) {
	result := SwitchHooksResult{SettingsPath: s.Settings.Path(), DryRun: dryRun}

	current, err := s.Settings.Read()
	if err != nil {
		return result, err
	}

	switched, changes, err := domain.SwitchHookCommands(current)
	if err != nil {
		return result, fmt.Errorf("%s: %w", result.SettingsPath, err)
	}
	result.Changes = toHookCommandChanges(changes)

	if len(changes) == 0 || dryRun {
		return result, nil
	}

	backupPath, err := s.Settings.Backup(current)
	if err != nil {
		return result, err
	}
	result.BackupPath = backupPath

	if err := s.Settings.Write(switched); err != nil {
		return result, err
	}
	return result, nil
}

// Restore は最新のバックアップから settings.json を書き戻す。
//
// バックアップが無い場合(switch を一度も実行していない場合)と、現在の内容が
// 既にバックアップと一致している場合はエラーにせず、その状態を結果で返す。
//
// 復元前に現在の内容を退避することはしない。退避すると次の Restore が
// 「切り替え後の内容」を最新のバックアップとして拾ってしまうためである。
func (s *HookSwitcher) Restore(dryRun bool) (RestoreHooksResult, error) {
	result := RestoreHooksResult{SettingsPath: s.Settings.Path(), DryRun: dryRun}

	backupPath, backup, found, err := s.Settings.LatestBackup()
	if err != nil {
		return result, err
	}
	if !found {
		return result, nil
	}
	result.BackupPath = backupPath
	result.Found = true

	current, err := s.Settings.Read()
	if err != nil {
		return result, err
	}
	if bytes.Equal(current, backup) {
		return result, nil
	}
	result.Changed = true

	if dryRun {
		return result, nil
	}
	if err := s.Settings.Write(backup); err != nil {
		return result, err
	}
	return result, nil
}

// toHookCommandChanges は domain の置換一覧を境界の型へ移し替える。
func toHookCommandChanges(changes []domain.HookCommandChange) []HookCommandChange {
	if len(changes) == 0 {
		return nil
	}
	out := make([]HookCommandChange, 0, len(changes))
	for _, c := range changes {
		out = append(out, HookCommandChange{Event: c.Event, Before: c.Before, After: c.After})
	}
	return out
}
