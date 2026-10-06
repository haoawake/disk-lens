//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// macOS 上按实际占用的磁盘块统计：稀疏文件几乎不占空间
func TestAllocatedSize(t *testing.T) {
	root := makeTree(t, "real.bin=10000")
	f, err := os.Create(filepath.Join(root, "sparse.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	var st syscall.Stat_t
	if err := syscall.Stat(filepath.Join(root, "real.bin"), &st); err != nil {
		t.Fatal(err)
	}
	s := scanDone(t, root)
	_, real, _, _, _ := s.Info([]string{"real.bin"})
	if real != st.Blocks*512 || real < 10000 {
		t.Errorf("real.bin = %d, want %d (blocks × 512)", real, st.Blocks*512)
	}
	if _, sparse, _, _, _ := s.Info([]string{"sparse.bin"}); sparse > 1<<20 {
		t.Errorf("sparse 64 MB file counted as %d bytes", sparse)
	}
}

// 同一个文件的多个硬链接只算一次；重新扫描其中一个文件夹之后也还是一次
func TestHardLinksCountedOnce(t *testing.T) {
	root := makeTree(t, "a/big.bin=50000", "b/other.bin=100")
	if err := os.Link(filepath.Join(root, "a/big.bin"), filepath.Join(root, "b/big-link.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "a/big.bin"), filepath.Join(root, "a/big-again.bin")); err != nil {
		t.Fatal(err)
	}
	want := sizeOf(t, root, "a/big.bin", "b/other.bin")
	s := scanDone(t, root)
	if got := s.Status().Bytes; got != want {
		t.Fatalf("total = %d, want %d (hard links counted once)", got, want)
	}
	for _, dir := range []string{"a", "b"} {
		if err := s.Rescan([]string{dir}); err != nil {
			t.Fatal(err)
		}
		s.Wait()
		if got := s.Status().Bytes; got != want {
			t.Errorf("after rescanning %s: total = %d, want %d", dir, got, want)
		}
	}
}

// 访达里「已锁定」的文件，永久删除时先解锁
func TestDeleteLockedFile(t *testing.T) {
	root := makeTree(t, "d/locked.bin=10", "d/free.bin=20")
	p := filepath.Join(root, "d", "locked.bin")
	if err := syscall.Chflags(p, ufImmutable); err != nil {
		t.Fatal(err)
	}
	j := runDeletePosix(t, filepath.Join(root, "d"))
	if j.failed.Load() != 0 {
		_ = syscall.Chflags(p, 0)
		t.Fatalf("failed: %v", j.errs)
	}
	if _, err := os.Lstat(filepath.Join(root, "d")); !os.IsNotExist(err) {
		t.Error("folder with a locked file still exists")
	}
}

// 这台 Mac 的宗卷布局：/Users 通过固件链接在数据宗卷上，扫「/」时要进去，但不能从 /System/Volumes/Data 再数一遍
func TestSystemVolumeLayout(t *testing.T) {
	v := readSysVolumes()
	t.Logf("rootDev=%d dataDev=%d mounts=%v firmlinks=%d", v.rootDev, v.dataDev, v.mounts, len(v.firmlinks))
	if name := startupDiskName(); name == "" {
		t.Error("no startup disk name")
	} else {
		t.Logf("startup disk: %s", name)
	}
	if !isVolumeRoot("/") || isVolumeRoot("/Users") {
		t.Error("isVolumeRoot")
	}
	// 用这台 Mac 上真实的设备号判断（有的机器上系统宗卷和数据宗卷报的设备号一样，也必须对）
	users, _ := devOf("/Users")
	data, ok := devOf(dataVolume)
	if !ok {
		t.Skip("no data volume")
	}
	p := newMountPolicy("/", v)
	if !p.enter("/Users", v.rootDev, users) {
		t.Error("/Users should be entered when scanning /")
	}
	if p.enter("/System/Volumes/Data/Users", data, users) {
		t.Error("/System/Volumes/Data/Users would be counted twice")
	}
	if !p.enter(dataVolume, v.rootDev, data) {
		t.Error("the data volume's own leftovers should be counted")
	}
}

// 扫描范围里挂着的另一个磁盘（这里用一个磁盘映像）不进去
func TestMountedImageSkipped(t *testing.T) {
	if os.Getenv("DISKLENS_MOUNT_TEST") == "" {
		t.Skip("set DISKLENS_MOUNT_TEST=1 to run (uses hdiutil)")
	}
	root := makeTree(t, "here.bin=4096")
	img := filepath.Join(t.TempDir(), "t.dmg")
	mnt := filepath.Join(root, "mnt")
	if out, err := exec.Command("hdiutil", "create", "-size", "20m", "-fs", "APFS", "-volname", "DLTest", img).CombinedOutput(); err != nil {
		t.Fatalf("hdiutil create: %v\n%s", err, out)
	}
	if err := os.Mkdir(mnt, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("hdiutil", "attach", "-nobrowse", "-mountpoint", mnt, img).CombinedOutput(); err != nil {
		t.Fatalf("hdiutil attach: %v\n%s", err, out)
	}
	defer exec.Command("hdiutil", "detach", "-force", mnt).Run()
	if err := os.WriteFile(filepath.Join(mnt, "inside.bin"), make([]byte, 5<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	s := scanDone(t, root)
	if got, want := s.Status().Bytes, sizeOf(t, root, "here.bin"); got != want {
		t.Errorf("total = %d, want %d (the mounted image must not be counted)", got, want)
	}
	if _, _, _, _, ok := s.Info([]string{"mnt"}); ok {
		t.Error("mount point should not appear in the tree")
	}
}
