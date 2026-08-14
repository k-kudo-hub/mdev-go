package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// SessionLauncher は `mdev`(引数なし)と `mdev <名前>` のユースケースである
// (現行 init.zsh の mdev() 相当)。
//
// 同じディレクトリから何度実行しても同じセッションへ戻る(attach-or-create)。
// 時刻付きのセッションが積み上がらないようにするための作りで、名前の決め方が
// その要である(domain.SessionRequest)。
type SessionLauncher struct {
	Sessions SessionLister
	Remover  SessionRemover
	Cleaner  SessionCleanService
	News     NewsRefreshService
	Update   UpdateCheckService
	Pending  SessionPendingRemover
	Files    FileStore
	Execer   ProcessExecer
	Chooser  SessionChooser
	Paths    domain.InstallPaths
	Clock    Clock
	// WorkDir は今の作業ディレクトリ。空なら os.Getwd を使う。
	WorkDir string
}

// SessionCleanService は溜まったセッションの掃除である。実体は SessionCleaner。
type SessionCleanService interface {
	// Clean は掃除を行う。dryRun が真なら数えるだけで何もしない。
	Clean(dryRun bool) (CleanupResult, error)
}

// NewsRefreshService は当日のニュースの用意である。実体は NewsRefresher。
type NewsRefreshService interface {
	Refresh(force bool)
}

// UpdateCheckService は起動時の更新確認である。実体は UpdateChecker。
type UpdateCheckService interface {
	Check(force bool) string
}

// SessionPendingRemover はセッション単位で pending をまとめて消す。
//
// 1 件ずつ消す PendingRemover とは別の口にしている。落ちたセッションの
// pending は「そのセッションより前のもの」としてまとめて捨てるのが正しく、
// 1 件ずつ選んで消す操作とは意味が違う。
type SessionPendingRemover interface {
	// RemoveSession は session の pending をまとめて消す。
	RemoveSession(session string) error
	// RemoveAll はすべての pending を消す。
	RemoveAll() error
}

// SessionChooser は候補から 1 つ選ばせる。
//
// 現行版は fzf を呼んでいた。Go 版はタスク作成の画面と同じ選択リストを
// 使うため、fzf は要らない(依存チェックからも外している)。
type SessionChooser interface {
	// Choose は prompt を出して 1 つ選ばせる。何も選ばなければ空を返す。
	Choose(prompt string, options []string) (string, error)
}

// SessionRequest はどのセッションを開くかの指定である。
//
// 中身は domain.SessionRequest と同じだが、cli は app にしか依存できない
// ため境界の型として持つ(ADR-0002)。
type SessionRequest struct {
	// Name は利用者が指定した名前。空なら作業ディレクトリから決める。
	Name string
	// Dir は今いる作業ディレクトリ。
	Dir string
	// Stamp は `--new` のときに足す時刻。空なら足さない。
	Stamp string
}

// InitZshScript はシェルへ読み込ませる定義である(`mdev init zsh` の出力)。
const InitZshScript = domain.InitZshScript

// NewSessionTimeLayout は `--new` が名前へ足す時刻の書式である。
const NewSessionTimeLayout = domain.NewSessionTimeLayout

// Start はセッションへ attach するか、新しく作る。成功すれば **戻らない**
// (zellij がこのプロセスを置き換える)。
//
// 手順は現行版のままである。
//
//   - 動いていれば attach する。ここでは掃除もニュースも更新確認も走らせない
//     (戻ってくるだけの操作を毎回重くしない)
//   - 落ちたまま残っていれば消してから作り直す。zellij の復元に任せると、
//     タスクのペインが会話を失った新しいエージェントとして立ち上がる。
//     タスクの復元はレジストリ経由で --resume が行う
//   - 落ちたセッションの pending も消す。そのセッションより前の記録なので、
//     残すとレジストリが復元したタスクを古い行が覆い隠す
func (s *SessionLauncher) Start(out io.Writer, req SessionRequest) error {
	name := domain.SessionRequest(req).SessionName()

	listing, err := s.Sessions.ListSessions()
	if err != nil {
		// 一覧を引けないときは「無い」とは見なさない。掃除の事故と同じ形
		// (rc=0 かつ空)を作らないため、そのまま失敗させる。
		return fmt.Errorf("セッションの一覧を取得できません: %w", err)
	}

	if domain.ParseSessionState(listing, name) == domain.SessionAlive {
		return s.exec("attach", name)
	}

	s.prepare(out)

	if domain.ParseSessionState(listing, name) == domain.SessionExited {
		// 消せなくても作り直しは試す。zellij 側が既に消していることもある。
		_ = s.Remover.DeleteSession(name)
		_ = s.Pending.RemoveSession(name)
	}
	return s.execNew(name)
}

// prepare はセッションを作る前の下ごしらえをする。
//
// どれも失敗しても起動は止めない。掃除もニュースも更新の案内も、セッションを
// 開くという目的から見れば付随的なものである。
func (s *SessionLauncher) prepare(out io.Writer) {
	// 掃除の失敗で起動を止めない(--auto と同じ扱い)。実事故のときと同じく、
	// 判断材料が取れないなら何もしないのが正しい。
	_, _ = s.Cleaner.Clean(false)
	s.News.Refresh(false)
	if notice := s.Update.Check(false); notice != "" {
		_, _ = io.WriteString(out, notice)
	}
}

// exec は zellij へプロセスを置き換える。
func (s *SessionLauncher) exec(args ...string) error {
	command := append([]string{domain.ZellijCommand}, args...)
	if err := s.Execer.Exec(command); err != nil {
		return fmt.Errorf("zellij の起動に失敗しました: %w", err)
	}
	return nil
}

// execNew はレイアウトを指定して新しいセッションを作る。
func (s *SessionLauncher) execNew(name string) error {
	layout := s.Paths.ConductorPath("layouts/multi.kdl")
	if !s.Files.Exists(layout) {
		return fmt.Errorf("レイアウトがありません: %s(`mdev install` を実行してください)", layout)
	}
	return s.exec("--new-session-with-layout", layout, "--session", name)
}

// StartDev は単一の開発セッションを開く(現行 init.zsh の dev() 相当)。
//
// attach-or-create ではない。名前を省くと時刻が入るため、毎回新しい
// セッションになる。使い捨ての作業場という位置づけを変えていない。
func (s *SessionLauncher) StartDev(name string) error {
	if name == "" {
		name = filepath.Base(s.workDir()) + "-" + s.Clock.Now().Format(domain.NewSessionTimeLayout)
	}
	layout := s.Paths.ConductorPath("layouts/dev.kdl")
	if !s.Files.Exists(layout) {
		return fmt.Errorf("レイアウトがありません: %s(`mdev install` を実行してください)", layout)
	}
	// 名前は zellij の上限に収める。現行版は切り詰めておらず、長い
	// ディレクトリ名では zellij に弾かれていた。
	return s.exec("--new-session-with-layout", layout,
		"--session", domain.ZellijSessionName(name, name))
}

// Attach は名前を指定して、または一覧から選んで attach する
// (現行 init.zsh の zs() 相当)。
//
// 現行版の `zellij attach || zellij --session` と同じ結果になるよう、
// **一覧に名前があれば EXITED でも attach する。** zellij は EXITED の
// セッションを attach で復活させるので、あちらの `attach` はその場合も
// 成功する。生きているものだけを attach の対象にすると、入りたくて zs を
// 叩いた利用者に対して**同じ名前の空のセッションを新しく作ってしまい**、
// 前のタブが行方不明になる。
func (s *SessionLauncher) Attach(name string) error {
	if name == "" {
		chosen, err := s.chooseSession()
		if err != nil {
			return err
		}
		if chosen == "" {
			// 何も選ばなければ何もしない(現行版も fzf の空選択で終わる)。
			return nil
		}
		name = chosen
	}

	listing, err := s.Sessions.ListSessions()
	if err != nil {
		return fmt.Errorf("セッションの一覧を取得できません: %w", err)
	}
	if domain.ParseSessionState(listing, name) != domain.SessionAbsent {
		return s.exec("attach", name)
	}
	return s.exec("--session", name)
}

// chooseSession は一覧から 1 つ選ばせる。
func (s *SessionLauncher) chooseSession() (string, error) {
	listing, err := s.Sessions.ListSessions()
	if err != nil {
		return "", fmt.Errorf("セッションの一覧を取得できません: %w", err)
	}
	entries := domain.ParseSessionList(listing)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if len(names) == 0 {
		return "", errors.New("開けるセッションがありません")
	}
	return s.Chooser.Choose("セッションを選ぶ", names)
}

// ClearPending は pending をすべて消す(現行 init.zsh の pending-clear 相当)。
//
// タスクそのものや作業ログには触らない。画面に出ている待ち状態だけを
// 落とすための操作である。
func (s *SessionLauncher) ClearPending(out io.Writer) error {
	if err := s.Pending.RemoveAll(); err != nil {
		return err
	}
	_, _ = io.WriteString(out, "待ち状態を消しました\n")
	return nil
}

// workDir は今の作業ディレクトリを返す。
func (s *SessionLauncher) workDir() string {
	if s.WorkDir != "" {
		return s.WorkDir
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}
