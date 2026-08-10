package app

import (
	"fmt"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// RecordOutput はタスクの作業サマリを daily log へ書くユースケースである
// (現行 Shell 版の record-output.sh 相当)。
//
// タスクタブを閉じるときに呼ばれる。pending は削除しない。アップロードの失敗で
// 削除が取り消されるとタブは生き続けるため、pending の後始末は呼び出し側が持つ。
//
// つまり同じ pending に対して何度も呼ばれうる。2 回目以降が Done ペインへ
// エントリを増やさないよう、daily log への書き込みは (tab, claude_session_id) を
// dedupe キーとした置換になっている(DailyAppender.Append を参照)。このユースケースは
// 再実行でも同じキーを渡し続けることでその置換を成立させる。
type RecordOutput struct {
	Pending    PendingFinder
	Transcript TranscriptReader
	Daily      DailyAppender
	Pricing    PricingLoader
	Clock      Clock
}

// Execute は tab のタスクのサマリを daily log へ 1 行書く。
//
// tab が空、または該当する pending が無い場合は何もしない(現行版も exit 0)。
// transcript の解析に失敗しても書き込み自体は行う。作業が終わった事実の記録が、
// その内訳より優先されるためである。
func (r *RecordOutput) Execute(tab string, env RecordEnv) error {
	if tab == "" {
		return nil
	}

	session := domain.SessionName(env.ZellijSession)
	pending, found, err := r.Pending.FindByTab(session, tab)
	if err != nil {
		return fmt.Errorf("pending の探索に失敗しました: %w", err)
	}
	if !found {
		return nil
	}

	now := r.Clock.Now()
	source := domain.DailySource{
		Tab:             tab,
		Session:         session,
		CompletedAt:     now.Format(domain.DailyCompletedAtLayout),
		Message:         pending.Message,
		Dir:             pending.Dir,
		TaskType:        pending.TaskType,
		ClaudeSessionID: pending.ClaudeSessionID,
		TranscriptPath:  pending.TranscriptPath,
		Agent:           pending.Agent,
	}

	// transcript の読み込みはロックの外で行う。解析に時間がかかっても、
	// daily log を待っている他のプロセスを止めないためである。
	var transcript []byte
	hasTranscript := false
	if pending.TranscriptPath != "" {
		transcript, hasTranscript = r.Transcript.Read(pending.TranscriptPath)
	}

	record := domain.BuildDailyRecord(source, transcript, hasTranscript, r.Pricing.Load())
	if err := r.Daily.Append(session, now.Format(domain.DailyFileDateLayout), record); err != nil {
		return fmt.Errorf("daily log への追記に失敗しました: %w", err)
	}
	return nil
}
