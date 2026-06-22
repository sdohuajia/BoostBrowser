package fsutil

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
)

func TestNormalizePathInputConvertsWindowsSeparators(t *testing.T) {
	t.Parallel()

	got := NormalizePathInput(`chrome\Chrom-144\chrome.exe`)
	want := filepath.Join("chrome", "Chrom-144", "chrome.exe")
	if got != want {
		t.Fatalf("NormalizePathInput() = %q, want %q", got, want)
	}
}

func TestEnsureExecutableRepairsMissingExecBitsOnUnix(t *testing.T) {
	t.Parallel()

	if goruntime.GOOS == "windows" {
		t.Skip("Windows does not use POSIX execute bits")
	}

	path := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(path, []byte("stub"), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	if err := EnsureExecutable(path); err != nil {
		t.Fatalf("EnsureExecutable() 返回错误: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取测试文件状态失败: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("EnsureExecutable() 未补充执行权限: mode=%#o", info.Mode().Perm())
	}
}
