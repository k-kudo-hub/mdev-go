package app

import (
	"errors"
	"fmt"
	"io"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// Uninstaller は `mdev uninstall` のユースケースである(現行 uninstall.sh 相当)。
//
// 自分自身(CONDUCTOR_HOME/bin/mdev)も一緒に消える。実行中のバイナリの
// ファイルを消しても、そのプロセスは動き続ける(開いている inode は残る)ので、
// 削除の後に残りの案内まで出し切れる。
type Uninstaller struct {
	Paths domain.InstallPaths
	Files FileStore
	// PendingRoot は pending の置き場所(ホーム直下で CONDUCTOR_HOME の外)。
	PendingRoot string
}

// Uninstall は設定を外し、keepData が偽ならデータも消す。
func (u *Uninstaller) Uninstall(out io.Writer, keepData bool) error {
	_, _ = fmt.Fprintln(out, "mdev を取り除きます")

	var errs []error
	if err := u.removeHooks(out); err != nil {
		errs = append(errs, err)
	}
	if err := u.removeCodexNotify(out); err != nil {
		errs = append(errs, err)
	}

	if keepData {
		_, _ = fmt.Fprintf(out, "  ? データは残しました(%s / %s)\n", u.Paths.ConductorHome, u.PendingRoot)
	} else if err := u.removeData(out); err != nil {
		errs = append(errs, err)
	}

	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "次の行を .zshrc から消してください:")
	_, _ = fmt.Fprintln(out, "  "+domain.ZshrcSourceLine)
	return errors.Join(errs...)
}

// removeHooks は settings.json から conductor の hooks を外す。
//
// 現行 uninstall.sh は「conductor に触れるイベントを丸ごと落とす」jq を
// 使っていた。同じイベントに利用者が足した hook まで一緒に消えるため、
// ここでは **mdev を指すコマンドだけ**を消す。
func (u *Uninstaller) removeHooks(out io.Writer) error {
	current, found, err := u.Files.Read(u.Paths.Settings)
	if err != nil || !found {
		return err
	}
	cleaned, removed, err := domain.RemoveHookCommands(current)
	if err != nil {
		return fmt.Errorf("hooks を外せません: %w", err)
	}
	if removed == 0 {
		return nil
	}
	if err := u.Files.Write(u.Paths.Settings, cleaned); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "  ✓ hooks から mdev を外しました(%d 件)\n", removed)
	return nil
}

// removeCodexNotify は codex の notify から mdev の行を落とす。
func (u *Uninstaller) removeCodexNotify(out io.Writer) error {
	current, found, err := u.Files.Read(u.Paths.CodexConfig)
	if err != nil || !found {
		return err
	}
	cleaned, removed := domain.RemoveCodexNotify(string(current), u.Paths.MdevBinaryPath())
	if !removed {
		return nil
	}
	if err := u.Files.Write(u.Paths.CodexConfig, []byte(cleaned)); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "  ✓ codex の notify から mdev を外しました(%s)\n", u.Paths.CodexConfig)
	return nil
}

// removeData は CONDUCTOR_HOME と pending を消す。
//
// **消す前に何が失われるかを出す。** daily の作業ログはここにしか無い。
//
// **消してよい場所かどうかを先に確かめる**(domain.CheckRemovable)。
// CONDUCTOR_HOME は環境変数で外から与えられるため、`/` やホームそのもの、
// mdev と無関係なディレクトリが届きうる。満たさなければ理由を出して
// 消さずに進む(uninstall 全体は止めない。hooks の解除は既に済んでいる)。
func (u *Uninstaller) removeData(out io.Writer) error {
	// 設置されていなければ消すものが無い。確かめる前に抜ける
	// (何も無い環境で「痕跡がありません」と言っても意味が無い)。
	if u.Files.Exists(u.Paths.ConductorHome) {
		if err := domain.CheckRemovable(
			u.Paths.ConductorHome, u.Paths.Home, u.Files.Exists); err != nil {
			_, _ = fmt.Fprintf(out, "  ! データは消しませんでした: %v\n", err)
			return err
		}
	}

	var errs []error
	for _, dir := range []string{u.Paths.ConductorHome, u.PendingRoot} {
		if !u.Files.Exists(dir) {
			continue
		}
		_, _ = fmt.Fprintf(out, "  ✓ %s を削除します\n", dir)
		if names, err := u.Files.ListDir(dir); err == nil && len(names) > 0 {
			for _, name := range names {
				_, _ = fmt.Fprintf(out, "      - %s\n", name)
			}
		}
		if err := u.Files.Remove(dir); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
