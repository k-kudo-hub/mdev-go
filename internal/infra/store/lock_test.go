package store_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/k-kudo-hub/mdev-go/internal/infra/store"
)

func TestLockAcquireAndRelease(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "test.lock")
	lock := store.NewLock(dir)

	ok, err := lock.Acquire(store.DefaultLockTimeout)
	if err != nil {
		t.Fatalf("Acquire() = %v", err)
	}
	if !ok {
		t.Fatal("空いているロックを取得できなかった")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("ロックディレクトリが作られていない: %v", err)
	}

	// 所有者 PID が記録されている。
	b, err := os.ReadFile(filepath.Join(dir, "pid")) //nolint:gosec // テスト内で組み立てた一時パス
	if err != nil {
		t.Fatalf("pid ファイルが読めない: %v", err)
	}
	if got, err := strconv.Atoi(strings.TrimSpace(string(b))); err != nil || got != os.Getpid() {
		t.Errorf("pid = %q, want %d", b, os.Getpid())
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release() = %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Release 後もロックが残っている: %v", err)
	}

	// 解放後は取り直せる。
	ok, err = lock.Acquire(store.DefaultLockTimeout)
	if err != nil || !ok {
		t.Errorf("再取得 = %v, %v, want true, nil", ok, err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release() = %v", err)
	}
}

func TestLockHeldLockTimesOut(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "test.lock")
	holder := store.NewLock(dir)
	ok, err := holder.Acquire(store.DefaultLockTimeout)
	if err != nil || !ok {
		t.Fatalf("Acquire() = %v, %v", ok, err)
	}
	t.Cleanup(func() { _ = holder.Release() })

	// 所有者(このテストプロセス自身)は生きているので stale 回収は起きず、
	// タイムアウトまで待って false を返す。
	start := time.Now()
	ok, err = store.NewLock(dir).Acquire(300 * time.Millisecond)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Acquire() = %v", err)
	}
	if ok {
		t.Error("保持中のロックを取得できてしまった")
	}
	// 100ms 刻みで 3 回待つため、最低でも 200ms は経過する。
	if elapsed < 200*time.Millisecond {
		t.Errorf("待ち時間 = %v, want 200ms 以上", elapsed)
	}
}

func TestLockReclaimsStaleLock(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "test.lock")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	// 存在しない PID を所有者として書く。
	if err := os.WriteFile(filepath.Join(dir, "pid"), []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	lock := store.NewLock(dir)
	start := time.Now()
	ok, err := lock.Acquire(300 * time.Millisecond)
	if err != nil {
		t.Fatalf("Acquire() = %v", err)
	}
	if !ok {
		t.Fatal("所有者の消えたロックを回収できなかった")
	}
	// 回収はポーリングを待たずに行われる。
	if elapsed := time.Since(start); elapsed >= 100*time.Millisecond {
		t.Errorf("回収に %v かかった, want 即時", elapsed)
	}
	t.Cleanup(func() { _ = lock.Release() })

	// 退避用ディレクトリが残っていないこと。
	matches, err := filepath.Glob(dir + ".stale.*")
	if err != nil {
		t.Fatalf("Glob() = %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("退避ディレクトリが残っている: %v", matches)
	}
}

func TestLockTreatsUnknownOwnerAsAlive(t *testing.T) {
	t.Parallel()

	// PID が読めないロックは所有者不明として stale 判定せず、
	// タイムアウトまで待つ(生きている所有者を横取りしないため)。
	tests := []struct {
		name    string
		pid     string
		writePi bool
	}{
		{name: "pid ファイルが無い", writePi: false},
		{name: "pid が数値でない", pid: "not-a-pid", writePi: true},
		{name: "pid が 0", pid: "0", writePi: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), "test.lock")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("MkdirAll() = %v", err)
			}
			if tt.writePi {
				if err := os.WriteFile(filepath.Join(dir, "pid"), []byte(tt.pid), 0o600); err != nil {
					t.Fatalf("WriteFile() = %v", err)
				}
			}

			ok, err := store.NewLock(dir).Acquire(200 * time.Millisecond)
			if err != nil {
				t.Fatalf("Acquire() = %v", err)
			}
			if ok {
				t.Error("所有者不明のロックを横取りしてしまった")
			}
		})
	}
}

func TestLockAcquireReportsUnexpectedFailure(t *testing.T) {
	t.Parallel()

	// 親ディレクトリが無い場合は「既に存在する」ではないためエラーになる。
	dir := filepath.Join(t.TempDir(), "missing-parent", "test.lock")
	ok, err := store.NewLock(dir).Acquire(store.DefaultLockTimeout)
	if err == nil {
		t.Error("Acquire() = nil, want エラー")
	}
	if ok {
		t.Error("Acquire() = true, want false")
	}
}

func TestLockAcquireWithZeroTimeoutFailsImmediately(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "test.lock")
	holder := store.NewLock(dir)
	if ok, err := holder.Acquire(store.DefaultLockTimeout); err != nil || !ok {
		t.Fatalf("Acquire() = %v, %v", ok, err)
	}
	t.Cleanup(func() { _ = holder.Release() })

	start := time.Now()
	ok, err := store.NewLock(dir).Acquire(0)
	if err != nil {
		t.Fatalf("Acquire() = %v", err)
	}
	if ok {
		t.Error("保持中のロックを取得できてしまった")
	}
	if elapsed := time.Since(start); elapsed >= 100*time.Millisecond {
		t.Errorf("待ち時間 = %v, want 即時", elapsed)
	}
}

func TestLockSerialisesConcurrentHolders(t *testing.T) {
	t.Parallel()

	// 同時に取りに行っても 1 つしか成功しないこと。
	dir := filepath.Join(t.TempDir(), "test.lock")
	const racers = 8

	results := make(chan bool, racers)
	for range racers {
		go func() {
			ok, err := store.NewLock(dir).Acquire(0)
			results <- ok && err == nil
		}()
	}

	acquired := 0
	for range racers {
		if <-results {
			acquired++
		}
	}
	if acquired != 1 {
		t.Errorf("取得できた数 = %d, want 1", acquired)
	}
	if err := store.NewLock(dir).Release(); err != nil {
		t.Fatalf("Release() = %v", err)
	}
}
