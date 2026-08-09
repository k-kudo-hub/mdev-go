package store

import "os"

// TranscriptStore はエージェントの transcript ファイルを読む
// app.TranscriptReader の実装である。
//
// 置き場所は conductor の管理外(claude / codex がそれぞれ決める)で、pending に
// 絶対パスが記録されている。そのため root を持たず、渡されたパスをそのまま読む。
type TranscriptStore struct{}

// NewTranscriptStore は TranscriptStore を返す。
func NewTranscriptStore() TranscriptStore { return TranscriptStore{} }

// Read は path の内容を返す。読めない場合は found=false を返す。
//
// 現行版は `[ -n "$TRANSCRIPT_PATH" ] && [ -f "$TRANSCRIPT_PATH" ]` で存在だけを
// 見て、読めなければ jq の失敗としてフォールバックに落ちる。どちらの経路も
// 「summary の無いレコードを書く」ことに変わりはないため、読めない理由は
// 区別せず found=false にまとめている。
func (TranscriptStore) Read(path string) ([]byte, bool) {
	data, err := os.ReadFile(path) //nolint:gosec // pending に記録された transcript のパス
	if err != nil {
		return nil, false
	}
	return data, true
}
