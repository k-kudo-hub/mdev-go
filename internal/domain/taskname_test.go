package domain_test

import (
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/domain"
)

func TestUniqueTaskName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		base     string
		existing []string
		want     string
	}{
		{
			name:     "既存名が無ければ base をそのまま返す",
			base:     "myapp-dev",
			existing: nil,
			want:     "myapp-dev",
		},
		{
			name:     "重複しなければ base をそのまま返す",
			base:     "other-dev",
			existing: []string{"Main", "myapp-dev", "myapp-dev-2"},
			want:     "other-dev",
		},
		{
			name:     "重複したら -2 を付与する",
			base:     "myapp-dev",
			existing: []string{"Main", "myapp-dev"},
			want:     "myapp-dev-2",
		},
		{
			name:     "-2 も埋まっていれば空いている連番まで進む",
			base:     "myapp-dev",
			existing: []string{"Main", "myapp-dev", "myapp-dev-2"},
			want:     "myapp-dev-3",
		},
		{
			name:     "連番に歯抜けがあれば最小の空き番号を使う",
			base:     "myapp-dev",
			existing: []string{"myapp-dev", "myapp-dev-3"},
			want:     "myapp-dev-2",
		},
		{
			name:     "部分一致は重複として扱わない(完全一致のみ)",
			base:     "myapp",
			existing: []string{"myapp-dev", "myapp-dev-2"},
			want:     "myapp",
		},
		{
			name:     "base が空でも既存名と衝突しなければ空のまま返す",
			base:     "",
			existing: []string{"myapp-dev"},
			want:     "",
		},
		{
			name:     "大文字小文字は区別する",
			base:     "MyApp",
			existing: []string{"myapp"},
			want:     "MyApp",
		},
		{
			name:     "既存名に重複があっても結果は変わらない",
			base:     "myapp-dev",
			existing: []string{"myapp-dev", "myapp-dev", "myapp-dev-2"},
			want:     "myapp-dev-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := domain.UniqueTaskName(tt.base, tt.existing)
			if got != tt.want {
				t.Errorf("UniqueTaskName(%q, %#v) = %q, want %q", tt.base, tt.existing, got, tt.want)
			}
		})
	}
}

func TestUniqueTaskNameDoesNotMutateExisting(t *testing.T) {
	t.Parallel()

	existing := []string{"myapp-dev", "myapp-dev-2"}
	domain.UniqueTaskName("myapp-dev", existing)

	want := []string{"myapp-dev", "myapp-dev-2"}
	for i := range want {
		if existing[i] != want[i] {
			t.Fatalf("existing was mutated: got %#v, want %#v", existing, want)
		}
	}
}
