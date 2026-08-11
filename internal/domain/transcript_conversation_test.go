package domain_test

import (
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

// TestConversationText は現行 test.sh の「42 / 42b generate_summary」が
// 会話抽出に期待している内容を移植したものである。期待値は現行版の jq を
// 同じ入力へ実際に流して確かめている。
func TestConversationText(t *testing.T) {
	tests := []struct {
		name   string
		data   string
		want   string
		wantOK bool
	}{
		{
			name: "claude: 文字列 content とテキストブロックを繋ぐ",
			data: `{"type":"user","message":{"role":"user","content":"hello"},"uuid":"u1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]},"uuid":"a1"}
{"type":"user","message":{"role":"user","content":"fix the bug"},"uuid":"u2"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/tmp/app.ts"}}]},"uuid":"a2"}
{"type":"summary","summary":"ignored"}
`,
			want:   "hello\nhi\nfix the bug",
			wantOK: true,
		},
		{
			name: "codex v1: user_message / agent_message を拾い developer prompt は除く",
			data: `{"type":"session_meta","payload":{"id":"t"}}
{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"DEVELOPERPROMPTMARKER"}]}}
{"type":"event_msg","payload":{"type":"user_message","message":"CODEXUSERMARKER fix the bug"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"CODEXAGENTMARKER fixed and pushed"}}
{"type":"event_msg","payload":{"type":"token_count","info":{}}}
`,
			want:   "CODEXUSERMARKER fix the bug\nCODEXAGENTMARKER fixed and pushed",
			wantOK: true,
		},
		{
			name: "codex v2: item_completed を拾い Reasoning は除く",
			data: `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u1","content":[{"type":"text","text":"V2USERMARKER fix the bug"}]}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"Reasoning","id":"r1","content":[{"type":"Text","text":"V2REASONMARKER"}]}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"m1","content":[{"type":"Text","text":"V2AGENTMARKER tests pass"}]}}}
`,
			want:   "V2USERMARKER fix the bug\nV2AGENTMARKER tests pass",
			wantOK: true,
		},
		{
			name: "codex v2: 素の文字列要素と .text の無い要素が混ざっても読める分を拾う",
			data: `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u1","content":["LENIENTSTRINGMARKER bare string element"]}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"m1","content":[{"type":"Image","url":"x"},{"type":"Text","text":"LENIENTTEXTMARKER done"}]}}}
`,
			want:   "LENIENTSTRINGMARKER bare string element\nLENIENTTEXTMARKER done",
			wantOK: true,
		},
		{
			// 現行版の配列連結の順。claude 形式が先で codex 形式が後になる。
			name: "両形式が混ざったら claude 形式を先に並べる",
			data: `{"type":"event_msg","payload":{"type":"user_message","message":"CODEX1"}}
{"type":"user","message":{"role":"user","content":"CLAUDE1"}}
`,
			want:   "CLAUDE1\nCODEX1",
			wantOK: true,
		},
		{
			name: "どちらの形式でもない jsonl は会話が取れない",
			data: `{"type":"session_meta","payload":{"id":"x"}}
{"type":"event_msg","payload":{"type":"token_count","info":{}}}
`,
			wantOK: false,
		},
		{
			name:   "空の transcript は会話が取れない",
			data:   "",
			wantOK: false,
		},
		{
			name:   "壊れた JSON は jq が落ちるのと同じく失敗する",
			data:   "{\"type\":\"user\",\n",
			wantOK: false,
		},
		{
			// message が null なら content も null になり、空文字として捨てられる。
			name:   "message が null のレコードは飛ばす",
			data:   "{\"type\":\"user\",\"message\":null}\n{\"type\":\"user\",\"message\":{\"content\":\"x\"}}\n",
			want:   "x",
			wantOK: true,
		},
		{
			// 現行版は `.message.content` の添字でエラーになり jq ごと落ちる。
			name:   "message がスカラーなら失敗する",
			data:   "{\"type\":\"user\",\"message\":5}\n",
			wantOK: false,
		},
		{
			// `[]?` が抑えるのは反復の失敗だけで、要素への `.type` は落ちる。
			name:   "claude の content にスカラー要素があれば失敗する",
			data:   "{\"type\":\"user\",\"message\":{\"content\":[\"bare\"]}}\n",
			wantOK: false,
		},
		{
			// join は null を空文字として挟む。
			name:   "claude のテキストブロックの text が null なら空文字になる",
			data:   "{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":null},{\"type\":\"text\",\"text\":\"a\"}]}}\n",
			want:   "\na",
			wantOK: true,
		},
		{
			// codex 側は select(type == "string") で落とすだけなので失敗しない。
			name:   "codex の message が文字列でなければ何も拾わない",
			data:   "{\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":5}}\n",
			wantOK: false,
		},
		{
			name:   "codex の payload がスカラーなら失敗する",
			data:   "{\"type\":\"event_msg\",\"payload\":5}\n",
			wantOK: false,
		},
		{
			name:   "codex の item がスカラーなら失敗する",
			data:   "{\"type\":\"event_msg\",\"payload\":{\"type\":\"item_completed\",\"item\":5}}\n",
			wantOK: false,
		},
		{
			// content が配列でなければ拾うものが無い(空の item は捨てられる)。
			name:   "codex の item content が配列でなければ何も拾わない",
			data:   "{\"type\":\"event_msg\",\"payload\":{\"type\":\"item_completed\",\"item\":{\"type\":\"UserMessage\",\"content\":\"x\"}}}\n",
			wantOK: false,
		},
		{
			name:   "レコードがオブジェクトでなければ失敗する",
			data:   "[1,2]\n",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := domain.ConversationText([]byte(tt.data))
			if ok != tt.wantOK {
				t.Fatalf("ConversationText ok = %v, want %v (got %q)", ok, tt.wantOK, got)
			}
			if ok && got != tt.want {
				t.Errorf("ConversationText = %q, want %q", got, tt.want)
			}
		})
	}
}
