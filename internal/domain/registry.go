package domain

import "sort"

// RegistryUpdatedAtLayout は registry の updated_at の書式。
// 現行 registry-lib.sh の `date '+%Y-%m-%dT%H:%M:%S%z'` に対応する
// (数値タイムゾーンでコロンを含まないため RFC3339 とは異なる)。
const RegistryUpdatedAtLayout = "2006-01-02T15:04:05-0700"

// RegistryEntry はタスク 1 件の永続レコードである。
//
// pending がユーザーの応答待ちの間だけ存在するのに対し、registry はタスクの
// 生存期間中ずっと残り、zellij セッションが落ちた後の復元(--resume)に使う。
// 現行 registry-lib.sh と同じ JSON 形状を持ち、dir / task_type / agent /
// transcript_path は空ならキーごと省略される。
type RegistryEntry struct {
	Tab             string `json:"tab"`
	Session         string `json:"session"`
	ClaudeSessionID string `json:"claude_session_id"`
	UpdatedAt       string `json:"updated_at"`
	Dir             string `json:"dir,omitempty"`
	TaskType        string `json:"task_type,omitempty"`
	Agent           string `json:"agent,omitempty"`
	TranscriptPath  string `json:"transcript_path,omitempty"`
}

// LatestPerTab はタブごとに updated_at が最新の 1 件だけを残し、
// タブ名の昇順で返す。
//
// --resume での再起動はエージェントのセッション ID を変えるため、同じタブに
// 対する古いエントリは使えないセッション ID を持つ。復元時はタブごとに最新の
// 1 件だけを候補にする必要がある(restore-session.sh:69 の
// `group_by(.tab) | map(max_by(.updated_at // "")) | .[]` と同じ選択)。
//
// updated_at が同値の場合は入力順で後のものを採る(jq の max_by と同じ)。
// entries は変更しない。
func LatestPerTab(entries []RegistryEntry) []RegistryEntry {
	latest := make(map[string]RegistryEntry, len(entries))
	for _, e := range entries {
		if prev, ok := latest[e.Tab]; ok && e.UpdatedAt < prev.UpdatedAt {
			continue
		}
		latest[e.Tab] = e
	}

	result := make([]RegistryEntry, 0, len(latest))
	for _, e := range latest {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Tab < result[j].Tab })
	return result
}
