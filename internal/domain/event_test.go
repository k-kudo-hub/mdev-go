package domain_test

import (
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

func TestShouldOverwritePending(t *testing.T) {
	t.Parallel()

	// 現行 pending-notify.sh:43-48 の規則。
	//   if [ -f "$PENDING_FILE" ] && [ "$HOOK_EVENT" = "Stop" ]; then
	//       EXISTING_EVENT=$(jq -r '.event' "$PENDING_FILE" 2>/dev/null)
	//       if [ "$EXISTING_EVENT" = "Notification" ] || [ "$EXISTING_EVENT" = "Waiting" ]; then
	//           exit 0
	//       fi
	//   fi
	// pending が無い場合・壊れている場合は jq が空文字を返すため existing = "" になる。
	tests := []struct {
		name     string
		existing string
		next     string
		want     bool
	}{
		{name: "pending 無しに Notification", existing: "", next: domain.EventNotification, want: true},
		{name: "pending 無しに Stop", existing: "", next: domain.EventStop, want: true},
		{name: "pending 無しに unknown", existing: "", next: domain.EventUnknown, want: true},
		{name: "Notification は Notification を上書きする", existing: domain.EventNotification, next: domain.EventNotification, want: true},
		{name: "Notification は Stop を上書きする", existing: domain.EventStop, next: domain.EventNotification, want: true},
		{name: "Notification は Waiting も無条件に上書きする", existing: domain.EventWaiting, next: domain.EventNotification, want: true},
		{name: "Stop は Notification を上書きしない", existing: domain.EventNotification, next: domain.EventStop, want: false},
		{name: "Stop は Waiting を上書きしない", existing: domain.EventWaiting, next: domain.EventStop, want: false},
		{name: "Stop は Stop を上書きする", existing: domain.EventStop, next: domain.EventStop, want: true},
		{name: "Stop は壊れ JSON(event 空)を上書きする", existing: "", next: domain.EventStop, want: true},
		{name: "Stop は未知の event を上書きする", existing: domain.EventUnknown, next: domain.EventStop, want: true},
		{name: "unknown は Notification を上書きする", existing: domain.EventNotification, next: domain.EventUnknown, want: true},
		{name: "unknown は Waiting を上書きする", existing: domain.EventWaiting, next: domain.EventUnknown, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.ShouldOverwritePending(tt.existing, tt.next)
			if got != tt.want {
				t.Errorf("ShouldOverwritePending(%q, %q) = %v, want %v", tt.existing, tt.next, got, tt.want)
			}
		})
	}
}
