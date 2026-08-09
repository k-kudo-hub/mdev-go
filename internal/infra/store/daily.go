package store

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// Append は session の date のファイルへ record を 1 行追記する。
//
// 追記はロックの中だけで行う。record の組み立て(transcript の解析)は呼び出し側が
// ロックの外で済ませているため、保持時間はミリ秒未満に収まる。解析の遅さで
// 並行する読み書き直し(restore)を待たせると、そちらが fail-open で
// 上書きに走ってしまうためである。
//
// ロックを取れなかった場合は警告を出したうえで追記する(fail-open)。まれな競合
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

	return appendLine(path, append(b, '\n'))
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
