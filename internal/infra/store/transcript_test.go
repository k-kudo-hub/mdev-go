package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

var _ app.TranscriptReader = store.TranscriptStore{}

func TestTranscriptStoreRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	content := `{"type":"user"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	got, found := store.NewTranscriptStore().Read(path)
	if !found {
		t.Fatal("Read() found = false, want true")
	}
	if string(got) != content {
		t.Errorf("Read() = %q, want %q", got, content)
	}
}

func TestTranscriptStoreReadMissing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{name: "ファイルが無い", path: filepath.Join(t.TempDir(), "gone.jsonl")},
		{name: "パスが空", path: ""},
		{name: "ディレクトリ", path: t.TempDir()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, found := store.NewTranscriptStore().Read(tt.path); found {
				t.Error("Read() found = true, want false")
			}
		})
	}
}
