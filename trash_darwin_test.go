//go:build darwin && cgo

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 移到废纸篓：符号链接只移走链接本身。会真的往当前用户的废纸篓里放东西，所以要明确打开才跑
func TestTrash(t *testing.T) {
	if os.Getenv("DISKLENS_TRASH_TEST") == "" {
		t.Skip("set DISKLENS_TRASH_TEST=1 to run (puts test files into the Trash)")
	}
	root := makeTree(t, "dir/f.bin=100", "target/keep.txt=5")
	link := filepath.Join(root, "dir", "link")
	if err := os.Symlink(filepath.Join(root, "target"), link); err != nil {
		t.Fatal(err)
	}
	if msg := uiTrash(link); msg != "" {
		t.Fatalf("trash link: %s", msg)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Error("link still there")
	}
	if _, err := os.Stat(filepath.Join(root, "target", "keep.txt")); err != nil {
		t.Errorf("link target damaged: %v", err)
	}
	if msg := uiTrash(filepath.Join(root, "dir")); msg != "" {
		t.Fatalf("trash dir: %s", msg)
	}
	if _, err := os.Lstat(filepath.Join(root, "dir")); !os.IsNotExist(err) {
		t.Error("dir still there")
	}
	if msg := uiTrash(filepath.Join(root, "nope")); msg == "" {
		t.Error("trashing a missing file should fail")
	} else {
		t.Logf("missing file: %s", msg)
	}
}
