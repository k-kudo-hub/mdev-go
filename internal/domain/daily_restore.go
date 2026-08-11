package domain

import (
	"encoding/json"
	"strings"
)

// DailyRestoreTarget は Done のタスクをダッシュボードへ戻すときに使う、
// daily レコードの値である。
//
// タブの作り直しに要るものだけを持つ。表示に使う値(完了時刻・統計)は
// DoneRow が別に持っている。
type DailyRestoreTarget struct {
	// Dir は作業ディレクトリ。これが無いとタブを作り直せない。
	Dir string
	// TaskType はレイアウトを決めるタスク種別。
	TaskType string
	// ClaudeSessionID は再開するエージェントのセッション ID。
	ClaudeSessionID string
	// TranscriptPath は会話の記録。実在するときだけ再開できる。
	TranscriptPath string
	// Agent は名前付きエージェント。空なら旧来の単一エージェント経路。
	Agent string
}

// FindRestorableDaily は (tab, completedAt) が一致し、まだ復元されていない
// **最初の** 1 件を返す。
//
// 現行 restore-task.sh の
//
//	jq -c 'select(.tab == $t and .completed_at == $c and (.restored // false) != true)' | head -1
//
// に対応する。`(.restored // false) != true` なので、除外されるのは
// **真偽値の true** だけである(文字列の "true" は対象に残る)。
//
// 壊れた行に当たったらそこで読むのをやめる。jq は流し読みで、解釈できない
// 値に当たった時点で終了するためである(それより前の出力は残る)。
func FindRestorableDaily(lines [][]byte, tab, completedAt string) (DailyRestoreTarget, bool) {
	for _, line := range lines {
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(line, &fields); err != nil {
			// jq がここで止まる。以降の行は読まれない。
			return DailyRestoreTarget{}, false
		}
		if !matchesRestoreTarget(fields, tab, completedAt) {
			continue
		}
		return DailyRestoreTarget{
			Dir:             jqOptionalString(fields["dir"]),
			TaskType:        jqOptionalString(fields["task_type"]),
			ClaudeSessionID: jqOptionalString(fields["claude_session_id"]),
			TranscriptPath:  jqOptionalString(fields["transcript_path"]),
			Agent:           jqOptionalString(fields["agent"]),
		}, true
	}
	return DailyRestoreTarget{}, false
}

// MarkRestoredDaily は最初の一致行へ `"restored":true` を足した中身を返す。
//
// 現行版の
//
//	jq -s '(map(...) | index(true)) as $i
//	     | if $i == null then . else (.[$i] += {restored: true}) end | .[]'
//
// に対応する。**反転するのは最初の 1 件だけ**である。(tab, completed_at) は
// 一意な鍵ではなく、作り直したタブは 1 つなので、同じ組の兄弟は Done に
// 残さなければならない。
//
// 1 行でも JSON として読めなければ ok=false を返す。現行版は `jq -s` が
// 全体で失敗して書き戻しへ進まない(exit 5)ため、その挙動に合わせている。
// 判断できない中身を書き直さないほうが安全でもある。
//
// 触っていない行は**読んだままのバイト列で**書き戻す。現行版はファイル全体を
// jq の整形で出し直すため無関係な行の表記まで変わるが、そこは揃えていない
// (evidence §5-2)。
func MarkRestoredDaily(content []byte, tab, completedAt string) ([]byte, bool) {
	lines := strings.Split(string(content), "\n")

	kept := make([]string, 0, len(lines))
	target := -1
	for _, line := range lines {
		if len(strings.TrimSpace(line)) == 0 {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			return nil, false
		}
		if target < 0 && matchesRestoreTarget(fields, tab, completedAt) {
			target = len(kept)
		}
		kept = append(kept, line)
	}

	if target >= 0 {
		kept[target] = withRestoredFlag(kept[target])
	}

	var out strings.Builder
	for _, line := range kept {
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return []byte(out.String()), true
}

// matchesRestoreTarget は復元対象の条件を判定する。
func matchesRestoreTarget(fields map[string]json.RawMessage, tab, completedAt string) bool {
	if jqOptionalString(fields["tab"]) != tab {
		return false
	}
	if jqOptionalString(fields["completed_at"]) != completedAt {
		return false
	}
	return !isJSONTrue(fields["restored"])
}

// isJSONTrue は raw が真偽値の true かどうかを返す。
// `(.restored // false) != true` の除外条件に対応する。
func isJSONTrue(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "true"
}

// withRestoredFlag は JSON オブジェクトの 1 行へ `"restored":true` を差し込む。
//
// 行全体を読み直して書き出すのではなく、閉じ括弧の手前へ足すだけにする。
// map で読み直すとキーの並びが失われ、大きな整数が指数表記になり、mdev が
// 知らないキーの表記も変わってしまう。daily log は作業の履歴なので、
// 触る必要の無いところは 1 バイトも変えない。
//
// 呼び出し側が JSON オブジェクトとして解釈できた行だけを渡す。
func withRestoredFlag(line string) string {
	trimmed := strings.TrimRight(line, " \t\r")
	close := strings.LastIndexByte(trimmed, '}')
	if close < 0 {
		return line
	}
	// 空のオブジェクトにはカンマを付けない。
	separator := ","
	if strings.TrimSpace(trimmed[:close]) == "{" {
		separator = ""
	}
	return trimmed[:close] + separator + `"restored":true` + trimmed[close:]
}
