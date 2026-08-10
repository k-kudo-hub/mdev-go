package store

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// dailyDirName は CONDUCTOR_HOME 直下の daily log ディレクトリ名。
const dailyDirName = "daily"

// dailySuffix は daily log の拡張子。1 行 1 レコードの JSON Lines である。
const dailySuffix = ".jsonl"

// DailyLockTimeout は daily log のロックを諦めるまでの待ち時間
// (現行 record-output.sh:267 の `acquire_lock "$DAILY_LOCK" 2`)。
const DailyLockTimeout = 2 * time.Second

// DailyRoot は daily log の置き場所を返す。
// 現行版の `$CONDUCTOR_HOME/daily` に対応する。
func DailyRoot(conductorHome string) string {
	return filepath.Join(conductorHome, dailyDirName)
}

// DailyStore は daily log へレコードを追記する app.DailyAppender の実装である。
// レイアウトは <root>/<zellij セッション名>/<YYYY-MM-DD>.jsonl。
type DailyStore struct {
	root string
	warn io.Writer
}

// NewDailyStore は root 配下を使う DailyStore を返す。
// warn にはロックを取れなかったときの警告先(通常は os.Stderr)を渡す。
func NewDailyStore(root string, warn io.Writer) *DailyStore {
	return &DailyStore{root: root, warn: warn}
}

// Append は session の date のファイルへ record を 1 行書く。
//
// 同じ dedupe キー(tab と claude_session_id の組)を持つ未 restore の行が既に
// あれば、それらを取り除いてから末尾へ書く。アップロードの失敗でタスク削除が
// 中止されると record は同じ pending に対して何度も走るため、素朴に追記すると
// Done ペインに同じタスクのエントリが試行回数だけ並んでしまう。詳しい条件は
// removeSupersededDaily を参照。
//
// 書き込みはロックの中だけで行う。record の組み立て(transcript の解析)は呼び出し側が
// ロックの外で済ませているため、保持時間はミリ秒未満に収まる。解析の遅さで
// 並行する読み書き直し(restore)を待たせると、そちらが fail-open で
// 上書きに走ってしまうためである。
//
// ロックを取れなかった場合は警告を出したうえで書き込む(fail-open)。まれな競合
// より、完了の記録がロック待ちで失われることのほうが実害が大きい。
func (s *DailyStore) Append(session, date string, record domain.DailyRecord) error {
	b, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("daily レコードの JSON 化に失敗しました: %w", err)
	}

	dir := filepath.Join(s.root, session)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("ディレクトリ %s の作成に失敗しました: %w", dir, err)
	}
	path := filepath.Join(dir, date+dailySuffix)

	lock := NewLock(path + ".lock")
	acquired, err := lock.Acquire(DailyLockTimeout)
	if err != nil || !acquired {
		s.warnLockUnavailable(path, err)
	}
	if acquired {
		// 解放の失敗は追記の成否に影響しない(残っても次回 stale として回収される)。
		defer func() { _ = lock.Release() }()
	}

	// 置換は「消してから末尾へ足す」で行う。残った行の相対順序は変わらず、
	// 置き換えられた行だけが最終行へ移る。daily log は書かれた順に読まれるため、
	// 更新した記録が最新として扱われる形になる。
	if record.ClaudeSessionID != "" {
		removeSupersededDaily(path, record.Tab, record.ClaudeSessionID)
	}

	return appendLine(path, append(b, '\n'))
}

// removeSupersededDaily は path から、これから書く記録に取って代わられる行を消す。
//
// 消すのは tab と claude_session_id の両方が一致し、かつ restored が true でない行
// である。restored: true はダッシュボードへ戻したタスクの履歴であり、同じタブと
// セッション ID で作業が再開されても残さなければならない。claude_session_id を
// 持たない記録は dedupe キーが無いため、呼び出し側がここを通さず素通しで追記する。
//
// 途中で失敗しても呼び出し側へは伝えない。読めない・書けない場合は元のファイルを
// そのままにして追記へ進む。重複した記録は後から消せるが、切り詰めた記録は
// 取り戻せないためである。
func removeSupersededDaily(path, tab, claudeSessionID string) {
	content, err := os.ReadFile(path) //nolint:gosec // 呼び出し側が組み立てた daily ファイルのパス
	if err != nil {
		return
	}
	kept, removed := filterSupersededDaily(content, tab, claudeSessionID)
	if !removed {
		// 消す行が無いときは書き直さない。ファイルの中身を一切変えないことで、
		// 解析できない行があっても壊さずに済む。
		return
	}
	// 書き直しの失敗は無視する(rename までは元のファイルが残っている)。
	_ = writeFileAtomic(path, kept)
}

// filterSupersededDaily は content から一致行を除いた中身と、除いたかどうかを返す。
//
// 1 行でも JSON として読めなければ removed=false を返し、書き直しを見送らせる。
// 判断できない中身に対しては何もしないほうが安全である。
func filterSupersededDaily(content []byte, tab, claudeSessionID string) ([]byte, bool) {
	var kept []byte
	removed := false

	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// map で読むのは、想定外の型の値が入っていても解析を止めないためである
		// (型の合わない行は「一致しない行」として残る)。
		var fields map[string]any
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			return nil, false
		}
		if dailyString(fields, "tab") == tab &&
			dailyString(fields, "claude_session_id") == claudeSessionID &&
			fields["restored"] != true {
			removed = true
			continue
		}
		// 残す行は読んだままの文字列を書き戻す。整形し直さないことで、
		// mdev が知らないフィールドや表記もそのまま保たれる。
		kept = append(kept, line...)
		kept = append(kept, '\n')
	}
	return kept, removed
}

// dailyString は JSON の値を文字列として取り出す。文字列でなければ空を返す。
func dailyString(fields map[string]any, key string) string {
	value, _ := fields[key].(string)
	return value
}

// warnLockUnavailable はロックを取れなかったことを警告する。
func (s *DailyStore) warnLockUnavailable(path string, err error) {
	if s.warn == nil {
		return
	}
	reason := "待ち時間を過ぎました"
	if err != nil {
		reason = err.Error()
	}
	// 警告の書き込み失敗を報告する先は無いため無視する。
	_, _ = fmt.Fprintf(s.warn, "mdev: %s のロックを取得できないまま追記します(%s)\n", path, reason)
}

// appendLine は path の末尾に line を書き足す。
func appendLine(path string, line []byte) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm) //nolint:gosec // 呼び出し側が組み立てた daily ファイルのパス
	if err != nil {
		return fmt.Errorf("%s を開けませんでした: %w", path, err)
	}
	if _, err := file.Write(line); err != nil {
		_ = file.Close()
		return fmt.Errorf("%s への追記に失敗しました: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("%s のクローズに失敗しました: %w", path, err)
	}
	return nil
}
