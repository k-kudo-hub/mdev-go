package app

import (
	"fmt"
	"strings"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// uploadSuccessMessage はアップロードできたときに画面へ出す文言である。
// 現行 upload-log.sh の `echo "upload-log: アップロードしました -> $REF"` から、
// 呼び出し側が取り除いていた `upload-log: ` を落としたもの。
const uploadSuccessMessage = "アップロードしました -> "

// DailySessionReader は 1 セッションの daily ファイルをファイル名順に全部読む。
//
// 当日ぶんだけを読む DailyReader とは別の操作である。アップロードは完了と削除が
// 日をまたいでも記録を見つけなければならず、当日のファイルに無いからといって
// 統計の無いログを残すと、そのタスクの記録が事実上失われる。
type DailySessionReader interface {
	// ReadSession は session の daily ファイルをファイル名の昇順で返す。
	// 1 件も無ければ空を返す(エラーにしない)。
	ReadSession(session string) [][]byte
}

var _ LogUploadRunner = (*LogUploader)(nil)

// LogUploader は作業ログをログ用リポジトリへアップロードする
// (現行 upload-log.sh 相当)。
type LogUploader struct {
	Config     ConfigLoader
	Pending    PendingFinder
	Transcript TranscriptReader
	Daily      DailySessionReader
	Summarizer SummaryGenerator
	Pusher     LogPusher
	Clock      Clock
}

// UploadLog はタブの作業ログをアップロードし、画面へ出す文言を返す。
//
// 戻り値は現行版の終了コードの契約をそのまま写したものである。
//
//   - ("", nil): 意図的に飛ばした(アップロード無効・リポジトリ未設定・
//     対象の pending が無い)。タブは削除してよい
//   - (message, nil): アップロードした。message を見せてから削除する
//   - ("", err): 失敗した。**タブを削除してはならない**。削除すると
//     会話も作業ログも同時に失われる
//
// 秘密のマスクは 2 回かける。1 回目は要約をモデルへ渡す前(会話そのものを
// 外へ出さないため)、2 回目は最終的な markdown 全体(モデルが要約の中へ
// 秘密を書き戻すことがあるため)。片方だけでは漏れる経路が残る。
func (u *LogUploader) UploadLog(env PaneEnv, tab string) (string, error) {
	if tab == "" {
		return "", nil
	}
	upload := u.uploadConfig()
	if !upload.Enabled || upload.Repo == "" {
		return "", nil
	}

	session := env.Session()
	// pending が無いタブはアップロードするものが無い(エラーではない)。
	// 読み取りの失敗も同じ扱いにする。現行版は pending の走査を
	// `jq ... 2>/dev/null` で行い、読めないファイルを黙って飛ばして
	// 「該当なし = 終了コード 0」へ落ちる。
	pending, found, err := u.Pending.FindByTab(session, tab)
	if err != nil || !found {
		return "", nil
	}

	// **合成 pending は「会話の記録が無い」ことを自ら示している。**
	// スクリーン検出は hook を持たないエージェントのために pending を合成する。
	// 1 ターンも会話していないタブでは transcript がまだ無く、その pending は
	// transcript_path を持たない。ここを従来どおり失敗させると、守るべき会話が
	// 無いのに「会話を失わないための防御」が働いてタブの削除が永久に止まる。
	//
	// 飛ばすのはこの組み合わせだけである。実セッションの pending や
	// transcript を持つ合成 pending は従来どおり失敗させる(会話を失わない)。
	if domain.IsScreenSessionID(pending.ClaudeSessionID) && pending.TranscriptPath == "" {
		return "", nil
	}

	summary, err := u.summarize(pending.TranscriptPath)
	if err != nil {
		return "", err
	}

	record := u.record(session, tab)
	completedAt := domain.RecordCompletedAt(record)
	if completedAt == "" {
		completedAt = u.Clock.Now().Format(domain.UploadTimeLayout)
	}

	relPath := domain.BuildLogPath(upload.BaseDir, completedAt, tab)
	reference, err := u.Pusher.Push(upload.Repo, upload.Branch, relPath, domain.BuildMarkdown(record, summary))
	if err != nil {
		return "", fmt.Errorf("ログリポジトリへのpushに失敗しました: %w", err)
	}
	return uploadSuccessMessage + reference, nil
}

// uploadConfig は設定からアップロードの設定を取り出す。
//
// 設定が読めなかった場合はゼロ値(Enabled が false)になり、アップロードは
// 飛ばされる。現行版も `jq ... 2>/dev/null` の失敗を「無効」に落としている。
func (u *LogUploader) uploadConfig() domain.UploadConfig {
	config, _ := u.Config.Load()
	return config.Upload
}

// summarize は transcript から会話要約を作る。
//
// 会話が取れない(transcript が無い・どちらの形式でもない)場合と要約に
// 失敗した場合は error を返す。ここで空の要約のまま進むと、中身の無いログを
// 残したままタブが消えてしまう。
func (u *LogUploader) summarize(transcriptPath string) (string, error) {
	if transcriptPath == "" {
		return "", fmt.Errorf("会話要約の生成に失敗しました: transcript のパスが記録されていません")
	}
	data, found := u.Transcript.Read(transcriptPath)
	if !found {
		return "", fmt.Errorf("会話要約の生成に失敗しました: transcript %s を読めません", transcriptPath)
	}
	conversation, ok := domain.ConversationText(data)
	if !ok {
		return "", fmt.Errorf("会話要約の生成に失敗しました: transcript %s から会話を取り出せません", transcriptPath)
	}

	// 1 回目のマスク。会話そのものをモデルへ渡す前に伏せる。末尾の改行を
	// 落とすのは、現行版がコマンド置換で受けているためである。
	masked := strings.TrimRight(domain.FilterSecrets(conversation), "\n")
	summary, err := u.Summarizer.Summarize(masked)
	if err != nil {
		return "", fmt.Errorf("会話要約の生成に失敗しました: %w", err)
	}
	return summary, nil
}

// record はアップロードに使う daily レコードを返す。
// 1 件も無ければタブ名とセッション名だけを持つプレースホルダを合成する。
func (u *LogUploader) record(session, tab string) []byte {
	if record, ok := domain.SelectUploadRecord(u.Daily.ReadSession(session), tab); ok {
		return record
	}
	return domain.PlaceholderUploadRecord(tab, session)
}
