package app

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// HookSwitcher は Claude Code の settings.json の hooks を、現行 Shell 版の
// スクリプト呼び出しから `mdev hook` サブコマンドへ切り替える / 元へ戻す
// ユースケースである。
//
// settings.json は Claude Code 全体の設定ファイルであり、mdev の管理外の
// 設定(permissions の類)も入っている。書き換えは必ず「読む → 退避 →
// 原子的に置き換える」の順で行い、退避に失敗したら書き換えない。
type HookSwitcher struct {
	Settings SettingsStore
	// Binary は切り替え後の hooks が呼ぶ mdev の所在を調べる。
	Binary MdevBinaryLocator
	// Flavor は「Go 版を使う」という印を書く / 消す。
	//
	// hooks の切り替えは Go 版採用の意思表示そのものなので、印の更新も
	// ここで行う。印が無いと install.sh と `mdev update` が layouts と
	// hooks を Shell 版へ黙って戻してしまう。
	Flavor FlavorStore
}

// HookCommandChange は置き換える hook コマンド 1 件である。
//
// 中身は domain.HookCommandChange と同じだが、cli / tui は app にしか
// 依存できない(ADR-0002)ため、境界に出す型は app が持つ。
type HookCommandChange struct {
	// Event は `.hooks` 直下のイベント名(Notification / Stop の類)。
	Event string
	// Before / After は置換前後のコマンド文字列。
	Before string
	After  string
}

// HookCommand は `.hooks` 配下のコマンド 1 件である。
//
// 中身は domain.HookCommand と同じだが、cli / tui は app にしか
// 依存できない(ADR-0002)ため、境界に出す型は app が持つ。
type HookCommand struct {
	// Event は `.hooks` 直下のイベント名。
	Event string
	// Command はコマンド文字列。
	Command string
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
	// MissingBinaryPath は切り替え後の hooks が呼ぶ mdev が見つからなかった
	// 場合にそのパス。設置済みなら空になる。
	//
	// 切り替え自体は成功するため、エラーではなく警告として扱う。hook は
	// 非ブロッキングなので会話は壊れないが、pending が書かれずダッシュボードが
	// 無反応になる。
	MissingBinaryPath string
	// RemainingScripts は切り替えた後も `.hooks` 配下に残る conductor の
	// スクリプト呼び出しである。置換規則に無い亜種がここに出る。
	// 切り替え自体は成功しているため、エラーではなく警告として扱う。
	RemainingScripts []HookCommand
	// FlavorPath は「Go 版を使う」印を書いたファイルのパス。
	// dry-run のときは空になる。
	FlavorPath string
	// SettingsWritten は settings.json を実際に書き換えたことを表す。
	//
	// 失敗時の報告に要る。書き換えの後に印の書き込みで失敗した場合、
	// 「settings.json は変更されていません」と伝えると嘘になる。
	// BackupPath の有無では区別できない(退避は書き換えの前に済むため)。
	SettingsWritten bool
	// DryRun は書き込みを行わなかったことを表す。
	DryRun bool
}

// RestoreHooksResult は Restore の結果である。表示のために使う。
type RestoreHooksResult struct {
	// SettingsPath は対象の settings.json のパス。
	SettingsPath string
	// Changes は元へ戻した(dry-run では戻す予定の)コマンドの一覧。
	// 空なら既にスクリプトを指しており、何もしていない。
	// バックアップからの全文復元では中身が分からないため空になる。
	Changes []HookCommandChange
	// SettingsMissing は settings.json が存在しなかったことを表す。
	SettingsMissing bool
	// BackupPath は全文復元の復元元にしたバックアップのパス。
	// 通常の(逆向きの書き換えによる)復元では空になる。
	BackupPath string
	// RestoredFromBackup はバックアップの全文で書き戻した
	// (dry-run では書き戻す予定である)ことを表す。
	RestoredFromBackup bool
	// FlavorPath は消した「Go 版を使う」印のファイルのパス。
	// dry-run のときと、何も復元しなかったときは空になる。
	FlavorPath string
	// SettingsWritten は settings.json を実際に書き換えたことを表す
	// (SwitchHooksResult.SettingsWritten と同じ理由で失敗時の報告に要る)。
	SettingsWritten bool
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
	if path, exists := s.Binary.MdevBinary(); !exists {
		result.MissingBinaryPath = path
	}

	current, err := s.Settings.Read()
	if err != nil {
		return result, err
	}

	switched, changes, err := domain.SwitchHookCommands(current)
	if err != nil {
		return result, fmt.Errorf("%s: %w", result.SettingsPath, err)
	}
	result.Changes = toHookCommandChanges(changes)

	// 切り替えた結果に対して数える。規則で置き換わったものは残らないので、
	// ここに出るのは規則に無い亜種だけである。
	remaining, err := domain.RemainingPendingScriptCommands(switched)
	if err != nil {
		return result, fmt.Errorf("%s: %w", result.SettingsPath, err)
	}
	result.RemainingScripts = toHookCommands(remaining)

	if dryRun {
		return result, nil
	}

	// 既に切り替え済み(changes が空)なら settings.json には触らない。
	// 変更が無いのにバックアップを作ると、Restore が探す「最新のバックアップ」が
	// 切り替え後の内容になってしまう。
	if len(changes) > 0 {
		backupPath, err := s.Settings.Backup(current)
		if err != nil {
			return result, err
		}
		result.BackupPath = backupPath

		if err := s.Settings.Write(switched); err != nil {
			return result, err
		}
		result.SettingsWritten = true
	}

	// **hooks に変更が無くても印は書く。** 印は「Go 版を使う」という意思表示で
	// あって hooks の差分ではない。切り替え済みの状態で印だけを失っている
	// (install.sh が hooks を戻した直後など)場合に、ここで書き直せないと
	// 次の更新でまた巻き戻る。
	if err := s.Flavor.WriteFlavor(domain.FlavorGo); err != nil {
		return result, fmt.Errorf("%s、Go 版を使う印を書けませんでした"+
			"(このままでは install.sh や mdev update で設定が Shell 版へ戻ります): %w",
			switchedSettingsSummary(result.SettingsWritten), err)
	}
	result.FlavorPath = s.Flavor.Path()
	return result, nil
}

// switchedSettingsSummary は印の書き込みに失敗したときの前置きを返す。
//
// 「hooks を切り替えたうえで印だけ失敗した」のか「hooks は元から切り替え
// 済みで settings.json には触れていない」のかで、利用者が次に確かめる場所が
// 変わる。前者は settings.json が新しい内容になっているので、印を手で置くか
// 復元するかを選ぶことになる。
func switchedSettingsSummary(settingsWritten bool) string {
	if settingsWritten {
		return "hooks は切り替えました"
	}
	return "hooks は既に mdev を指しており settings.json は変更していません"
}

// Restore は settings.json の hooks を conductor のスクリプト呼び出しへ戻す。
//
// 現在の settings.json に対して Switch と逆向きの置換を行う。バックアップの
// 全文を書き戻さないのは、切り替え後に Claude Code 自身が settings.json へ
// 書いた変更(permissions.allow の追加が典型)を消さないためである。
// 逆向きの置換であれば hooks 以外の差分には一切触れない。
//
// 既にスクリプトを指している場合は何もしない(冪等)。
//
// settings.json が存在しない場合に限り、最新のバックアップの全文で書き戻す。
// これは「設定ファイルごと失った」という復元の主目的シナリオであり、
// 逆向きの置換の対象そのものが無いためである。バックアップも無ければ
// エラーにはせず、その状態を結果で返す。
//
// 復元前に現在の内容を退避することはしない。退避すると次の Restore の
// フォールバックが「切り替え後の内容」を最新のバックアップとして拾ってしまう。
func (s *HookSwitcher) Restore(dryRun bool) (RestoreHooksResult, error) {
	result := RestoreHooksResult{SettingsPath: s.Settings.Path(), DryRun: dryRun}

	current, err := s.Settings.Read()
	if errors.Is(err, fs.ErrNotExist) {
		result.SettingsMissing = true
		result, err = s.restoreFromBackup(result)
		// 何も復元できなかった(バックアップが 1 つも無い)ときは印にも触れない。
		// 「Shell 版へ戻した」と言える状態になっていないのに印だけ消すと、
		// hooks が mdev を指したまま install.sh が切り替え直さなくなる。
		if err != nil || dryRun || !result.RestoredFromBackup {
			return result, err
		}
		return s.clearFlavor(result)
	}
	if err != nil {
		return result, err
	}

	restored, changes, err := domain.RestoreHookCommands(current)
	if err != nil {
		return result, fmt.Errorf("%s: %w", result.SettingsPath, err)
	}
	result.Changes = toHookCommandChanges(changes)

	if dryRun {
		return result, nil
	}
	if len(changes) > 0 {
		if err := s.Settings.Write(restored); err != nil {
			return result, err
		}
		result.SettingsWritten = true
	}
	return s.clearFlavor(result)
}

// clearFlavor は「Go 版を使う」印を消す。
//
// **hooks に変更が無くても消す。** Switch が変更の有無に依らず印を書くのと
// 対称で、印は意思表示であって hooks の差分ではない。既に Shell 版を指して
// いるのに印だけが残っていると、その状態が正しいのかどうかが分からなくなる。
func (s *HookSwitcher) clearFlavor(result RestoreHooksResult) (RestoreHooksResult, error) {
	if err := s.Flavor.RemoveFlavor(); err != nil {
		return result, fmt.Errorf("%s、Go 版を使う印を消せませんでした"+
			"(このままでは install.sh が hooks を Go 版へ切り替え直します): %w",
			restoredSettingsSummary(result.SettingsWritten), err)
	}
	result.FlavorPath = s.Flavor.Path()
	return result, nil
}

// restoredSettingsSummary は印の削除に失敗したときの前置きを返す。
// switchedSettingsSummary と同じ理由で、settings.json に触れたかどうかで分ける。
func restoredSettingsSummary(settingsWritten bool) string {
	if settingsWritten {
		return "hooks は戻しました"
	}
	return "hooks は既に conductor のスクリプトを指しており settings.json は変更していません"
}

// restoreFromBackup は settings.json が存在しないときのフォールバックである。
// 最新のバックアップの全文で書き戻す。
func (s *HookSwitcher) restoreFromBackup(result RestoreHooksResult) (RestoreHooksResult, error) {
	backupPath, backup, found, err := s.Settings.LatestBackup()
	if err != nil {
		return result, err
	}
	if !found {
		return result, nil
	}
	result.BackupPath = backupPath
	result.RestoredFromBackup = true

	if result.DryRun {
		return result, nil
	}
	if err := s.Settings.Write(backup); err != nil {
		return result, err
	}
	result.SettingsWritten = true
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

// toHookCommands は domain のコマンド一覧を境界の型へ移し替える。
func toHookCommands(commands []domain.HookCommand) []HookCommand {
	if len(commands) == 0 {
		return nil
	}
	out := make([]HookCommand, 0, len(commands))
	for _, c := range commands {
		out = append(out, HookCommand{Event: c.Event, Command: c.Command})
	}
	return out
}
