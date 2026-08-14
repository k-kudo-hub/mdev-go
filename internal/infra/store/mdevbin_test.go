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

// TestMdevBinaryStoreTaskControlCommand は操作バーの起動コマンドを確かめる。
//
// 起動するのは **今動いているバイナリ**である(ADR D7-2)。ここでは実際に
// テストの実行ファイルの場所が入るため、値そのものではなく形を見る。
// 差し替えたときの中身は taskcontrol_test.go が固定している。
func TestMdevBinaryStoreTaskControlCommand(t *testing.T) {
	t.Parallel()

	s := store.NewMdevBinaryStore("/ch")
	got := s.TaskControlCommand("-wip")

	// タブ名の手前に `--` を置く。`-` で始まる名前をフラグと解釈させないため。
	want := []string{"pane", "task-control", "--", "-wip"}
	if !reflect.DeepEqual(got[1:], want) {
		t.Errorf("TaskControlCommand() = %v, want [<バイナリ> %v]", got, want)
	}
	if got[0] == "" {
		t.Error("バイナリのパスが空")
	}
}
