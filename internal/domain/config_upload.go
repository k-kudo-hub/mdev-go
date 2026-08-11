package domain

import "encoding/json"

// 作業ログのアップロード設定の既定値。
const (
	// DefaultUploadBaseDir はログリポジトリ内の置き場所の既定(`upload.base_dir`)。
	DefaultUploadBaseDir = "work-log"
	// DefaultUploadBranch は push 先ブランチの既定(`upload.branch`)。
	DefaultUploadBranch = "main"
)

// UploadTimeLayout は completed_at が無いときに補う現在時刻の書式である。
// 現行 upload-log.sh の `date '+%Y-%m-%dT%H:%M:%S%z'` に対応する。
const UploadTimeLayout = "2006-01-02T15:04:05-0700"

// UploadConfig は作業ログのアップロード設定(`.upload`)である。
type UploadConfig struct {
	// Enabled はアップロードを行うかどうか(`upload.enabled`)。
	//
	// 現行版は `jq -r '.upload.enabled // false'` の出力を文字列 "true" と
	// 比較するため、真偽値の true と文字列の "true" がどちらも有効になる。
	Enabled bool
	// Repo はログリポジトリ(URL・ローカルパス・"owner/name")。
	// 空ならアップロードは行わない。
	Repo string
	// BaseDir はリポジトリ内の置き場所。既定は "work-log"。
	BaseDir string
	// Branch は push 先ブランチ。既定は "main"。
	Branch string
}

// uploadConfigRaw は `.upload` をキーごとに未解釈のまま受ける入れ物である。
type uploadConfigRaw struct {
	Enabled json.RawMessage `json:"enabled"`
	Repo    json.RawMessage `json:"repo"`
	BaseDir json.RawMessage `json:"base_dir"`
	Branch  json.RawMessage `json:"branch"`
}

// unmarshalUploadKeys は Config のアップロード向けフィールドを埋める。
// Config.UnmarshalJSON から呼ばれる。
func (c *Config) unmarshalUploadKeys(data []byte) {
	var root struct {
		Upload json.RawMessage `json:"upload"`
	}
	// `.upload` が無い・型が違う場合も既定値の一式にする。Enabled が false
	// なので、実際にアップロードが走ることはない。
	c.Upload = UploadConfig{BaseDir: DefaultUploadBaseDir, Branch: DefaultUploadBranch}
	if err := json.Unmarshal(data, &root); err != nil {
		return
	}
	var raw uploadConfigRaw
	if err := json.Unmarshal(root.Upload, &raw); err != nil {
		return
	}
	c.Upload = UploadConfig{
		Enabled: jqEqualsTrue(raw.Enabled),
		Repo:    jqOptionalString(raw.Repo),
		BaseDir: fallback(jqOptionalString(raw.BaseDir), DefaultUploadBaseDir),
		Branch:  fallback(jqOptionalString(raw.Branch), DefaultUploadBranch),
	}
}

// UpdateCheckConfig は起動時の更新確認の設定(`.update_check`)である。
type UpdateCheckConfig struct {
	// Disabled は `update_check.enabled` が **明示的に** false のときだけ真になる。
	//
	// キーが無い場合は確認を行う(既定は有効)。現行 check-update.sh:20-26 が
	// jq の `//` を使わず生の値を読んでいるのは、`false // true` が true を
	// 返してしまい「明示的に切った」設定が無視されるためである。
	Disabled bool
}

// unmarshalUpdateCheckKeys は Config の更新確認向けフィールドを埋める。
func (c *Config) unmarshalUpdateCheckKeys(data []byte) {
	var root struct {
		UpdateCheck json.RawMessage `json:"update_check"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(root.UpdateCheck, &fields); err != nil {
		return
	}
	// 現行版は `jq -r '.update_check.enabled'` の出力を "false" と比べるため、
	// 真偽値の false と文字列の "false" のどちらでも無効になる。
	c.UpdateCheck.Disabled = jqRawString(fields["enabled"]) == "false"
}
