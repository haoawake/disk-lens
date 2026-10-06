//go:build !windows

package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func runDeletePosix(t *testing.T, path string) *deleteJob {
	t.Helper()
	done := make(chan struct{})
	j := startDelete(path, nil, func() { close(done) })
	<-done
	return j
}

// countTree 在删除前把要删的东西按当前平台的算法算一遍个数和大小
func countTree(t *testing.T, root string) (files, bytes int64) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files++
		bytes += diskSize(info)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files, bytes
}

func TestDeleteTreePosix(t *testing.T) {
	root := makeTree(t,
		"victim/a.bin=1000",
		"victim/sub/b.bin=2000",
		"victim/readonly/c.bin=300",
		"victim/emptydir/",
		"outside/keep.txt=5",
	)
	victim := filepath.Join(root, "victim")
	outside := filepath.Join(root, "outside")
	// 指向外面的符号链接：只能删掉链接本身，外面的文件必须还在
	if err := os.Symlink(outside, filepath.Join(victim, "link-to-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "keep.txt"), filepath.Join(victim, "link-to-file")); err != nil {
		t.Fatal(err)
	}
	// 自己的、但没有写权限的文件夹（比如 Go 的模块缓存）：确认过「我确定」后也要能删
	if err := os.Chmod(filepath.Join(victim, "readonly"), 0o555); err != nil {
		t.Fatal(err)
	}
	wantFiles, wantBytes := countTree(t, victim)

	j := runDeletePosix(t, victim)
	if n := j.failed.Load(); n != 0 {
		t.Fatalf("%d failures: %v", n, j.errs)
	}
	if _, err := os.Lstat(victim); !os.IsNotExist(err) {
		t.Fatalf("victim still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("symlink target was touched: %v", err)
	}
	if got := j.files.Load(); got != wantFiles || wantFiles != 5 { // 3 个文件 + 2 个链接
		t.Errorf("deleted %d entries, want %d (5)", got, wantFiles)
	}
	if got := j.bytes.Load(); got != wantBytes {
		t.Errorf("freed %d bytes, want %d", got, wantBytes)
	}
}

func TestDeleteSymlinkToDirPosix(t *testing.T) {
	root := makeTree(t, "target/inner.txt=9")
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "target"), link); err != nil {
		t.Fatal(err)
	}
	if j := runDeletePosix(t, link); j.failed.Load() != 0 {
		t.Fatalf("failed: %v", j.errs)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Error("link still there")
	}
	if _, err := os.Stat(filepath.Join(root, "target", "inner.txt")); err != nil {
		t.Errorf("link target damaged: %v", err)
	}
}

func TestDeleteMissingReportsFailure(t *testing.T) {
	j := runDeletePosix(t, filepath.Join(t.TempDir(), "nope"))
	if j.failed.Load() != 1 || len(j.errs) != 1 || !strings.Contains(j.errs[0], "已经不存在") {
		t.Errorf("failed=%d errs=%v", j.failed.Load(), j.errs)
	}
}

func TestDeleteErrorText(t *testing.T) {
	cases := map[syscall.Errno]string{
		syscall.EACCES: "没有权限", syscall.EPERM: "保护", syscall.ENOENT: "不存在",
		syscall.ENOTEMPTY: "删不掉", syscall.EBUSY: "正被使用", syscall.EROFS: "只读",
	}
	for errno, want := range cases {
		err := &os.PathError{Op: "unlink", Path: "/x", Err: errno}
		if got := deleteErrorText(err); !strings.Contains(got, want) {
			t.Errorf("deleteErrorText(%v) = %q, want it to mention %q", errno, got, want)
		}
	}
	if !isDirNotEmpty(&os.PathError{Err: syscall.ENOTEMPTY}) || isDirNotEmpty(errors.New("x")) {
		t.Error("isDirNotEmpty")
	}
}
