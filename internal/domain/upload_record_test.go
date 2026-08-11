package domain_test

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestSelectUploadRecord は「全 daily ファイルを走査して最後の一致を採る」
// 選び方を固定する。test.sh「47. cross-day」が確かめている、記録が当日以外の
// ファイルにしか無い場合も含む。
func TestSelectUploadRecord(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		tab   string
		want  string
	}{
		{
			name:  "1 ファイル内では最後の一致が勝つ",
			files: []string{`{"tab":"a","n":1}` + "\n" + `{"tab":"a","n":2}` + "\n"},
			tab:   "a",
			want:  `{"tab":"a","n":2}`,
		},
		{
			name: "ファイル間では後ろのファイルが勝つ",
			files: []string{
				`{"tab":"a","n":1}` + "\n",
				`{"tab":"a","n":2}` + "\n",
			},
			tab:  "a",
			want: `{"tab":"a","n":2}`,
		},
		{
			// 当日ぶん(後ろのファイル)に一致が無ければ、前の日の記録が残る。
			name: "後ろのファイルに一致が無ければ前の記録が残る",
			files: []string{
				`{"tab":"xday","cost":1.23}` + "\n",
				`{"tab":"other","cost":9}` + "\n",
			},
			tab:  "xday",
			want: `{"tab":"xday","cost":1.23}`,
		},
		{
			name:  "タブ名が一致しなければ選ばれない",
			files: []string{`{"tab":"b"}` + "\n"},
			tab:   "a",
			want:  "",
		},
		{
			name:  "tab が文字列でなければ一致しない",
			files: []string{`{"tab":5}` + "\n"},
			tab:   "5",
			want:  "",
		},
		{
			// jq は解釈できない行でその場で終わり、直前までの出力だけが残る。
			name:  "壊れた行があればそこまでの一致を採る",
			files: []string{`{"tab":"a","n":1}` + "\nこれは JSON ではない\n" + `{"tab":"a","n":2}` + "\n"},
			tab:   "a",
			want:  `{"tab":"a","n":1}`,
		},
		{
			name:  "ファイルが無ければ選ばれない",
			files: nil,
			tab:   "a",
			want:  "",
		},
		{
			name:  "空のファイルは飛ばす",
			files: []string{"", `{"tab":"a"}` + "\n"},
			tab:   "a",
			want:  `{"tab":"a"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := make([][]byte, 0, len(tt.files))
			for _, f := range tt.files {
				files = append(files, []byte(f))
			}
			got, ok := domain.SelectUploadRecord(files, tt.tab)
			if ok != (tt.want != "") {
				t.Fatalf("SelectUploadRecord ok = %v, want %v (got %q)", ok, tt.want != "", got)
			}
			if ok && string(got) != tt.want {
				t.Errorf("SelectUploadRecord = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPlaceholderUploadRecord は記録が 1 件も無いときの合成レコードを固定する。
// 現行版の `jq -n '{tab, session, completed_at:"", summary:null, markers:{}}'`
// と同じ内容でなければ、build_markdown の既定値が変わってしまう。
func TestPlaceholderUploadRecord(t *testing.T) {
	got := string(domain.PlaceholderUploadRecord("my-tab", "my-session"))
	want := `{"tab":"my-tab","session":"my-session","completed_at":"","summary":null,"markers":{}}`
	if got != want {
		t.Errorf("PlaceholderUploadRecord = %s, want %s", got, want)
	}
}

// TestPlaceholderUploadRecordRendersDefaults はプレースホルダから作った
// markdown が既定値で埋まることを確かめる(選択とレンダリングの繋ぎ目)。
func TestPlaceholderUploadRecordRendersDefaults(t *testing.T) {
	md := domain.BuildMarkdown(domain.PlaceholderUploadRecord("my-tab", "my-session"), "S")
	for _, want := range []string{"# my-tab", "- **Session**: my-session", "- **Model**: unknown", "| ターン数 | 0 |"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown に %q が含まれていません:\n%s", want, md)
		}
	}
}
