package store_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/k-kudo-hub/mdev-go/internal/app"
	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

var (
	_ app.MdevBinaryLocator   = (*store.MdevBinaryStore)(nil)
	_ app.TaskControlLauncher = (*store.MdevBinaryStore)(nil)
)

func TestMdevBinaryStore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(t *testing.T, home string)
		want    bool
	}{
		{
			name:    "bin ディレクトリごと無い",
			prepare: func(*testing.T, string) {},
			want:    false,
		},
		{
			name: "bin はあるが mdev が無い",
			prepare: func(t *testing.T, home string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
					t.Fatalf("MkdirAll() = %v", err)
				}
			},
			want: false,
		},
		{
			name: "mdev がディレクトリ",
			prepare: func(t *testing.T, home string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(home, "bin", "mdev"), 0o755); err != nil {
					t.Fatalf("MkdirAll() = %v", err)
				}
			},
			want: false,
		},
		{
			name: "設置済み",
			prepare: func(t *testing.T, home string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
					t.Fatalf("MkdirAll() = %v", err)
				}
				if err := os.WriteFile(filepath.Join(home, "bin", "mdev"), []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // 実行可能ファイルを模す
					t.Fatalf("WriteFile() = %v", err)
				}
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := filepath.Join(t.TempDir(), ".claude-conductor")
			tt.prepare(t, home)

			path, got := store.NewMdevBinaryStore(home).MdevBinary()
			if want := filepath.Join(home, "bin", "mdev"); path != want {
				t.Errorf("パス = %q, want %q", path, want)
			}
			if got != tt.want {
				t.Errorf("exists = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMdevBinaryStoreTaskControlCommand(t *testing.T) {
	t.Parallel()

	// タブ名の手前に `--` を置く。`-` で始まる名前をフラグと解釈させないため。
	s := store.NewMdevBinaryStore("/ch")
	want := []string{filepath.Join("/ch", "bin", "mdev"), "pane", "task-control", "--", "-wip"}
	if got := s.TaskControlCommand("-wip"); !reflect.DeepEqual(got, want) {
		t.Errorf("TaskControlCommand() = %v, want %v", got, want)
	}
}
