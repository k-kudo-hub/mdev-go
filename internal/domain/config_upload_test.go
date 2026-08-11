package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// parseConfig は設定 JSON を読む。
func parseConfig(t *testing.T, data string) domain.Config {
	t.Helper()
	var config domain.Config
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		t.Fatalf("設定の解釈に失敗しました: %v", err)
	}
	return config
}

// TestUploadConfig は `.upload` の読み取りを固定する。
//
// 既定値の当て方は現行版の jq に合わせる。`jq -r '.upload.base_dir //
// "work-log"'` の `//` は **null と false だけ**を偽とするため、空文字は
// そのまま通る(実測で確認済み)。
func TestUploadConfig(t *testing.T) {
	tests := []struct {
		name string
		data string
		want domain.UploadConfig
	}{
		{
			name: "すべて指定",
			data: `{"upload":{"enabled":true,"repo":"o/r","base_dir":"logs","branch":"trunk"}}`,
			want: domain.UploadConfig{Enabled: true, Repo: "o/r", BaseDir: "logs", Branch: "trunk"},
		},
		{
			name: "キーが無ければ既定値",
			data: `{"upload":{"enabled":true,"repo":"o/r"}}`,
			want: domain.UploadConfig{Enabled: true, Repo: "o/r", BaseDir: "work-log", Branch: "main"},
		},
		{
			name: "null は既定値",
			data: `{"upload":{"enabled":true,"repo":"o/r","base_dir":null,"branch":null}}`,
			want: domain.UploadConfig{Enabled: true, Repo: "o/r", BaseDir: "work-log", Branch: "main"},
		},
		{
			// 現行版は空のまま git へ渡して失敗し、アップロードが中止される
			// (= タブが消えない)。既定値へ倒すと、現行版なら止まっていた
			// 設定ミスが黙ってリポジトリ直下へ書き始めてしまう。
			name: "明示的な空文字は空文字のまま残す",
			data: `{"upload":{"enabled":true,"repo":"o/r","base_dir":"","branch":""}}`,
			want: domain.UploadConfig{Enabled: true, Repo: "o/r", BaseDir: "", Branch: ""},
		},
		{
			name: "upload セクションが無ければ既定値一式",
			data: `{}`,
			want: domain.UploadConfig{BaseDir: "work-log", Branch: "main"},
		},
		{
			name: "upload がオブジェクトでなければ既定値一式",
			data: `{"upload":5}`,
			want: domain.UploadConfig{BaseDir: "work-log", Branch: "main"},
		},
		{
			// jq -r は真偽値の true と文字列の "true" を同じ出力にする。
			name: "enabled は文字列の true でも有効",
			data: `{"upload":{"enabled":"true","repo":"o/r"}}`,
			want: domain.UploadConfig{Enabled: true, Repo: "o/r", BaseDir: "work-log", Branch: "main"},
		},
		{
			name: "enabled が false なら無効",
			data: `{"upload":{"enabled":false,"repo":"o/r"}}`,
			want: domain.UploadConfig{Repo: "o/r", BaseDir: "work-log", Branch: "main"},
		},
		{
			name: "repo が無ければ空",
			data: `{"upload":{"enabled":true}}`,
			want: domain.UploadConfig{Enabled: true, BaseDir: "work-log", Branch: "main"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseConfig(t, tt.data).Upload; got != tt.want {
				t.Errorf("Upload = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestUpdateCheckConfig は `.update_check` の読み取りを固定する。
//
// 現行 check-update.sh は jq の `//` を **使わず** 生の値を "false" と
// 比較する。`false // true` が true になってしまい、明示的に切った設定が
// 無視されるためである。
func TestUpdateCheckConfig(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "明示的な false は無効", data: `{"update_check":{"enabled":false}}`, want: true},
		{name: "文字列の false も無効", data: `{"update_check":{"enabled":"false"}}`, want: true},
		{name: "true は有効", data: `{"update_check":{"enabled":true}}`, want: false},
		{name: "キーが無ければ有効", data: `{"update_check":{}}`, want: false},
		{name: "null は有効", data: `{"update_check":{"enabled":null}}`, want: false},
		{name: "セクションが無ければ有効", data: `{}`, want: false},
		{name: "オブジェクトでなければ有効", data: `{"update_check":5}`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseConfig(t, tt.data).UpdateCheck.Disabled; got != tt.want {
				t.Errorf("UpdateCheck.Disabled = %v, want %v", got, tt.want)
			}
		})
	}
}
