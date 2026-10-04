//go:build windows

package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// makeJunction 建一个目录联接（相当于 mklink /J），不需要管理员权限
func makeJunction(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Mkdir(link, 0o755); err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(windows.StringToUTF16Ptr(link), windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	sub := utf16.Encode([]rune(`\??\` + target))
	pr := utf16.Encode([]rune(target))
	path := append(append(append(sub, 0), pr...), 0)
	data := make([]byte, 8+len(path)*2)
	le := binary.LittleEndian
	le.PutUint16(data[0:], 0)                    // SubstituteNameOffset
	le.PutUint16(data[2:], uint16(len(sub)*2))   // SubstituteNameLength
	le.PutUint16(data[4:], uint16(len(sub)*2+2)) // PrintNameOffset
	le.PutUint16(data[6:], uint16(len(pr)*2))    // PrintNameLength
	for i, c := range path {
		le.PutUint16(data[8+i*2:], c)
	}
	buf := make([]byte, 8+len(data))
	le.PutUint32(buf[0:], windows.IO_REPARSE_TAG_MOUNT_POINT)
	le.PutUint16(buf[4:], uint16(len(data)))
	copy(buf[8:], data)
	var n uint32
	if err := windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &buf[0], uint32(len(buf)), nil, 0, &n, nil); err != nil {
		t.Fatalf("set reparse point: %v", err)
	}
}

func runDelete(t *testing.T, path string) *deleteJob {
	t.Helper()
	done := make(chan struct{})
	j := startDelete(path, nil, func() { close(done) })
	<-done
	return j
}

func TestDeleteTree(t *testing.T) {
	root := makeTree(t,
		"victim/a.bin=1000",
		"victim/sub/b.bin=2000",
		"victim/sub/deeper/c.bin=3000",
		"victim/readonly.txt=10",
		"victim/emptydir/",
		"outside/keep.txt=5",
	)
	victim := filepath.Join(root, "victim")
	outside := filepath.Join(root, "outside")

	// 只读文件
	ro := filepath.Join(victim, "readonly.txt")
	if err := os.Chmod(ro, 0o444); err != nil {
		t.Fatal(err)
	}
	// 只读的文件夹属性也不该挡住删除
	if err := windows.SetFileAttributes(windows.StringToUTF16Ptr(filepath.Join(victim, "sub")), windows.FILE_ATTRIBUTE_READONLY); err != nil {
		t.Fatal(err)
	}
	// 指向外面的目录联接：只能删掉联接本身，外面的文件必须还在
	junction := filepath.Join(victim, "link-to-outside")
	makeJunction(t, junction, outside)

	j := runDelete(t, victim)
	if n := j.failed.Load(); n != 0 {
		t.Fatalf("%d failures: %v", n, j.errs)
	}
	if _, err := os.Lstat(victim); !os.IsNotExist(err) {
		t.Fatalf("victim still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("junction target was touched: %v", err)
	}
	if got := j.files.Load(); got != 5 { // 4 个文件 + 1 个联接
		t.Errorf("deleted %d entries, want 5", got)
	}
	if got := j.bytes.Load(); got != 6010 {
		t.Errorf("freed %d bytes, want 6010", got)
	}
}

func TestDeleteSingleFileAndJunction(t *testing.T) {
	root := makeTree(t, "f.bin=123", "target/inner.txt=9")
	j := runDelete(t, filepath.Join(root, "f.bin"))
	if j.failed.Load() != 0 || j.bytes.Load() != 123 {
		t.Fatalf("file delete: failed=%d bytes=%d", j.failed.Load(), j.bytes.Load())
	}
	// 直接删一个联接：联接没了，目标还在
	link := filepath.Join(root, "jl")
	makeJunction(t, link, filepath.Join(root, "target"))
	runDelete(t, link)
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("junction still there")
	}
	if _, err := os.Stat(filepath.Join(root, "target", "inner.txt")); err != nil {
		t.Errorf("junction target damaged: %v", err)
	}
}

func TestDeleteLockedFileReportsFailure(t *testing.T) {
	root := makeTree(t, "d/locked.bin=10", "d/free.bin=20")
	// 不带共享删除权限打开，模拟「正被其他程序使用」
	p := windows.StringToUTF16Ptr(filepath.Join(root, "d", "locked.bin"))
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	j := runDelete(t, filepath.Join(root, "d"))
	windows.CloseHandle(h)
	if j.failed.Load() != 1 {
		t.Fatalf("failed = %d, want 1 (%v)", j.failed.Load(), j.errs)
	}
	if _, err := os.Stat(filepath.Join(root, "d", "free.bin")); !os.IsNotExist(err) {
		t.Error("the free file should have been deleted")
	}
	if _, err := os.Stat(filepath.Join(root, "d", "locked.bin")); err != nil {
		t.Error("locked file should still exist")
	}
}

func TestProtection(t *testing.T) {
	sys := os.Getenv("SystemRoot")
	pf := os.Getenv("ProgramFiles")
	cases := []struct {
		path        string
		block, warn bool
	}{
		{`C:\`, true, false},
		{sys, true, false},
		{filepath.Join(sys, "System32"), true, false},
		{filepath.Join(sys, "Temp", "x.log"), false, false},
		{filepath.Join(sys, "SoftwareDistribution"), false, true},
		{pf, true, false},
		{filepath.Join(pf, "SomeApp"), false, true},
		{os.Getenv("USERPROFILE"), true, false},
		{filepath.Join(os.Getenv("USERPROFILE"), "Downloads", "big.iso"), false, false},
		{filepath.Join(os.TempDir(), "junk"), false, false},
		{filepath.Join(os.Getenv("LOCALAPPDATA"), "SomeApp"), false, true},
	}
	for _, c := range cases {
		block, warn := protection(c.path)
		if (block != "") != c.block || (warn != "") != c.warn {
			t.Errorf("protection(%s) = block %q, warn %q", c.path, block, warn)
		}
	}
}
