// Package codex は codex CLI が残す状態の読み取りを担当する。
// internal/app が定義する port の実装(adapter)である(ADR-0002)。
package codex

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/app"
)

// codexHomeDefault は CODEX_HOME が空のときの置き場所(HOME からの相対)。
const codexHomeDefault = ".codex"

// stateDBPrefix / stateDBSuffix は状態 DB のファイル名の形である
// (実物は state_5.sqlite のように版番号が入る)。
const (
	stateDBPrefix = "state_"
	stateDBSuffix = ".sqlite"
)

// sessionsDirName は会話ログの置き場所(CODEX_HOME からの相対)。
const sessionsDirName = "sessions"

// rolloutSuffix は会話ログの拡張子。ファイル名は
// rollout-<日時>-<スレッド ID>.jsonl の形になる。
const rolloutSuffix = ".jsonl"

// sqliteCommand は状態 DB を引く CLI である。
//
// Go の sqlite ドライバを足さないのは、cgo か巨大な純 Go 実装のどちらかを
// 抱え込むことになる一方、ここでの用途が「1 行を best-effort で引く」だけだ
// ためである。現行版と同じ CLI を使えば、引けるかどうかの条件も揃う。
const sqliteCommand = "sqlite3"

// sqliteTimeout は DB がロックされていたときに諦めるまでの時間(ミリ秒)。
//
// codex 自身が書き込み中だと DB はロックされている。会話ログは作業ログの
// アップロードにしか使わないので、待つより諦めて空で進むほうがよい
// (現行版の `-cmd '.timeout 200'`)。
const sqliteTimeout = "200"

// Locator は codex の会話ログ(rollout)の場所を探す。
type Locator struct {
	home string
	// runSQLite は状態 DB へ問い合わせる。テストで差し替える。
	runSQLite func(db, query string) (string, error)
}

var _ app.CodexTranscriptLocator = (*Locator)(nil)

// NewLocator は codexHome を見る Locator を返す。
// codexHome が空なら home/.codex を見る(現行版の `${CODEX_HOME:-$HOME/.codex}`)。
func NewLocator(codexHome, home string) *Locator {
	if codexHome == "" {
		codexHome = filepath.Join(home, codexHomeDefault)
	}
	return &Locator{home: codexHome, runSQLite: runSQLite}
}

// Locate は threadID の会話ログの絶対パスを返す。見つからなければ空文字を返す。
//
// 引き方は 2 つあり、順に試す。
//
//   - 新しい codex は状態 DB(threads.rollout_path)に場所を記録する
//   - 古い codex は sessions/ 配下にファイルを残すだけなので、名前で探す
//
// どちらも best-effort である。DB が無い・ロックされている・sqlite3 が無い・
// 記録された場所が既に消えている、のいずれでも次の手か空文字に落ちる。
func (l *Locator) Locate(threadID string) string {
	if path := l.fromStateDB(threadID); isFile(path) {
		return path
	}
	// 名前で探すほうは、見つけた時点で実在が確かめられている。
	return l.fromSessionsDir(threadID)
}

// fromStateDB は状態 DB から会話ログの場所を引く。
func (l *Locator) fromStateDB(threadID string) string {
	db := l.latestStateDB()
	if db == "" {
		return ""
	}
	out, err := l.runSQLite(db, selectRolloutPath(threadID))
	if err != nil {
		return ""
	}
	return strings.TrimRight(out, "\n")
}

// selectRolloutPath はスレッド ID で会話ログの場所を引く SQL を組み立てる。
//
// 値を文字列として埋め込む。sqlite3 CLI には引数を渡す口が無く、現行版も
// 同じ形で埋め込んでいる。引用符は 2 つ重ねて閉じられないようにする
// (現行版に無い手当て。スレッド ID は codex が作る UUID なので実害の出る
// 経路は無いが、payload は外から来る値なので閉じておく)。
func selectRolloutPath(threadID string) string {
	quoted := strings.ReplaceAll(threadID, "'", "''")
	return "SELECT rollout_path FROM threads WHERE id='" + quoted + "' LIMIT 1"
}

// latestStateDB は版番号が最も大きい状態 DB を返す。無ければ空文字を返す。
//
// 現行版の `ls state_*.sqlite | sort -t_ -k2 -n | tail -1` に対応する。
// 数として比べるのが要点で、辞書順だと state_10 が state_5 より前に来て
// 古い DB を引いてしまう。
//
// 現行版はパス全体を `_` で割った 2 つ目の欄で比べるため、CODEX_HOME の
// 途中に `_` があると版番号ではない欄を見て並びが崩れる。こちらは
// ファイル名だけを見るのでその影響を受けない。
func (l *Locator) latestStateDB() string {
	entries, err := os.ReadDir(l.home)
	if err != nil {
		return ""
	}
	latest, found := "", -1
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version, ok := stateDBVersion(entry.Name())
		if !ok || version < found {
			continue
		}
		latest, found = filepath.Join(l.home, entry.Name()), version
	}
	return latest
}

// stateDBVersion は state_<数>.sqlite の数を返す。形が違えば ok=false。
func stateDBVersion(name string) (int, bool) {
	if !strings.HasPrefix(name, stateDBPrefix) || !strings.HasSuffix(name, stateDBSuffix) {
		return 0, false
	}
	middle := name[len(stateDBPrefix) : len(name)-len(stateDBSuffix)]
	version, err := strconv.Atoi(middle)
	if err != nil {
		return 0, false
	}
	return version, true
}

// fromSessionsDir は sessions/ 配下から名前で会話ログを探す。
//
// 現行版の `find "$CODEX_HOME/sessions" -type f -name "*<thread>.jsonl" | head -1`
// に対応する。
//
// 現行版との意図的な差異: find は走査順(ディレクトリの並び)で最初の 1 件を
// 採るのに対し、こちらは名前順で最初の 1 件を採る。スレッド ID は codex が
// 作る一意な値なので複数が該当する経路は無く、順序が観測される場面も無い。
func (l *Locator) fromSessionsDir(threadID string) string {
	root := filepath.Join(l.home, sessionsDirName)
	var matches []string
	// 読めないディレクトリは黙って飛ばす。1 か所の権限で会話ログの引き当て
	// 全体を諦める理由は無い。
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // 読めない枝は飛ばして探索を続ける
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), threadID+rolloutSuffix) {
			matches = append(matches, path)
		}
		return nil
	})
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[0]
}

// isFile は path が実在する通常のファイルかを返す。
func isFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// runSQLite は sqlite3 CLI で問い合わせる。
func runSQLite(db, query string) (string, error) {
	out, err := exec.Command(sqliteCommand, "-cmd", ".timeout "+sqliteTimeout, db, query).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
