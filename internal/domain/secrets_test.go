package domain_test

import (
	"strings"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestFilterSecrets は現行 test.sh の「41. upload-log.sh filter_secrets」の
// 全ケースを移植したものである。Shell 側は grep で「印があること」と
// 「素の値が残っていないこと」を確かめているため、ここでも出力全体を
// 期待値として書ける形(1 行入力)に落として比較する。
func TestFilterSecrets(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "anthropic API キーを伏せる",
			input: "auth key=sk-ant-api03-abcDEF123456789ghijklmnop_qrstuvwxyz",
			want:  "auth key=***REDACTED***\n",
		},
		{
			name:  "github トークンを伏せる",
			input: "token ghp_abcdefghijklmnopqrstuvwxyz0123456789",
			want:  "token ***REDACTED***\n",
		},
		{
			name:  "github の fine-grained トークンを伏せる",
			input: "token github_pat_11ABCDEFG0abcdefghijklmnopqrstuvwxyz",
			want:  "token ***REDACTED***\n",
		},
		{
			name:  "aws のアクセスキーを伏せる",
			input: "aws AKIAIOSFODNN7EXAMPLE here",
			want:  "aws ***REDACTED*** here\n",
		},
		{
			name:  "slack のトークンを伏せる",
			input: "slack xoxb-EXAMPLESLACKtokenplaceholder",
			want:  "slack ***REDACTED***\n",
		},
		{
			// Bearer だけは前置きを残す(文脈が読めなくなるため)。
			name:  "bearer トークンは前置きを残して伏せる",
			input: "Authorization: Bearer aBcDeF1234567890xyzTOKEN",
			want:  "Authorization: Bearer ***REDACTED***\n",
		},
		{
			name:  "小文字の bearer も伏せる",
			input: "authorization: bearer aBcDeF1234567890xyzTOKEN",
			want:  "authorization: bearer ***REDACTED***\n",
		},
		{
			name:  "sk- で始まる汎用のキーを伏せる",
			input: "openai sk-abcdefghijklmnopqrstuvwxyz0123",
			want:  "openai ***REDACTED***\n",
		},
		{
			name:  "秘密でない文章はそのまま通る",
			input: "just a normal log line about task completion",
			want:  "just a normal log line about task completion\n",
		},
		{
			// 40 文字以上かつ base64 の文字だけの行は、印が無くても落とす。
			name:  "base64 だけの長い行を伏せる",
			input: "MIIBVAIBADANBgkqhkiG9w0BAQEFAASCAT4wggE6AAAA",
			want:  "***REDACTED***\n",
		},
		{
			// 39 文字なので長さの条件を満たさない。
			name:  "39 文字の base64 行はそのまま通る",
			input: "MIIBVAIBADANBgkqhkiG9w0BAQEFAASCAT4wggE",
			want:  "MIIBVAIBADANBgkqhkiG9w0BAQEFAASCAT4wggE\n",
		},
		{
			// base64 の文字だけで構成されていないので長くても通る。
			name:  "長くても base64 以外を含む行はそのまま通る",
			input: "this line is definitely longer than forty characters",
			want:  "this line is definitely longer than forty characters\n",
		},
		{
			// 現行 test.sh の PEM ケース。折り返しの最終行は 26 文字しかなく、
			// 長さでは落ちないため状態機械で落とす必要がある。
			name: "PEM の秘密鍵ブロックを 1 行の印に潰す",
			input: strings.Join([]string{
				"before",
				"-----BEGIN PRIVATE KEY-----",
				"MIIBVAIBADANBgkqhkiG9w0BAQEFAASCAT4wggE6",
				"AgEAAoGBAKsecretkeymaterialxyz0123456789",
				"k7QmShortTailLine24charsZ=",
				"-----END PRIVATE KEY-----",
				"after",
			}, "\n"),
			want: "before\n***REDACTED PRIVATE KEY***\nafter\n",
		},
		{
			name: "RSA など種別付きの PEM も潰す",
			input: strings.Join([]string{
				"-----BEGIN RSA PRIVATE KEY-----",
				"short",
				"-----END RSA PRIVATE KEY-----",
				"tail",
			}, "\n"),
			want: "***REDACTED PRIVATE KEY***\ntail\n",
		},
		{
			// END が来ないまま入力が終わったら末尾まで落とす(over-mask)。
			name: "閉じない PEM は末尾まで落とす",
			input: strings.Join([]string{
				"head line",
				"-----BEGIN PRIVATE KEY-----",
				"MIIBstraykeymaterialthatmustnotleak12345",
				"tail secret line",
			}, "\n"),
			want: "head line\n***REDACTED PRIVATE KEY***\n",
		},
		{
			// ブロックが 2 つあっても、それぞれが独立して閉じる。
			name: "PEM ブロックが 2 つあっても両方潰す",
			input: strings.Join([]string{
				"-----BEGIN PRIVATE KEY-----",
				"aaa",
				"-----END PRIVATE KEY-----",
				"middle",
				"-----BEGIN EC PRIVATE KEY-----",
				"bbb",
				"-----END EC PRIVATE KEY-----",
				"tail",
			}, "\n"),
			want: "***REDACTED PRIVATE KEY***\nmiddle\n***REDACTED PRIVATE KEY***\ntail\n",
		},
		{
			// 1 行に複数のトークンが並んでも全部伏せる(sed の /g)。
			name:  "1 行の複数トークンをすべて伏せる",
			input: "a ghp_abcdefghijklmnopqrstuvwxyz0123456789 b AKIAIOSFODNN7EXAMPLE c",
			want:  "a ***REDACTED*** b ***REDACTED*** c\n",
		},
		{
			name:  "空入力は空を返す",
			input: "",
			want:  "",
		},
		{
			// awk は末尾に改行が無い行も 1 レコードとして読み、print で改行を足す。
			name:  "末尾に改行が無くても出力には改行が付く",
			input: "a\nb",
			want:  "a\nb\n",
		},
		{
			name:  "末尾の改行は 1 つだけ区切りとして扱う",
			input: "a\nb\n",
			want:  "a\nb\n",
		},
		{
			name:  "空行は空行のまま残る",
			input: "a\n\nb\n",
			want:  "a\n\nb\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.FilterSecrets(tt.input); got != tt.want {
				t.Errorf("FilterSecrets(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestFilterSecretsIsIdempotent は 2 回かけても結果が変わらないことを固定する。
//
// アップロードの経路では「要約へ渡す前」と「最終の markdown 全体」の 2 回
// マスクをかける。2 回目で印そのものが壊れると、伏せたはずの箇所が読めなく
// なるため、冪等であることがこの二重適用の前提になる。
func TestFilterSecretsIsIdempotent(t *testing.T) {
	inputs := []string{
		"token ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"Authorization: Bearer aBcDeF1234567890xyzTOKEN",
		"-----BEGIN PRIVATE KEY-----\nbody\n-----END PRIVATE KEY-----\ntail",
		"normal text",
	}
	for _, input := range inputs {
		once := domain.FilterSecrets(input)
		twice := domain.FilterSecrets(once)
		if once != twice {
			t.Errorf("FilterSecrets が冪等ではありません: 1 回目 %q, 2 回目 %q", once, twice)
		}
	}
}
