//go:build darwin

package main

// macOS 上和系统打交道的部分（不涉及界面）：文件实际占多少空间、宗卷布局、权限。

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// friendlyError 遇到「没有权限」时给的建议
const permissionHint = "没有权限访问它。如果它受 macOS 的隐私保护，可以在「系统设置 → 隐私与安全性 → 完全磁盘访问权限」里允许文件清理助手。"

// 文件标志位（sys/stat.h）
const (
	ufImmutable = 0x00000002 // 访达里的「已锁定」（uchg）
	sfDataless  = 0x40000000 // iCloud「优化存储」后只留了个影子、内容在云端的文件（或文件夹）
)

func init() {
	// 从访达双击打开时，老版本的 macOS 会多传一个 -psn_0_12345 参数；flag 包不认识它会直接退出
	args := os.Args[:1]
	for _, a := range os.Args[1:] {
		if !strings.HasPrefix(a, "-psn_") {
			args = append(args, a)
		}
	}
	os.Args = args
}

// diskSize 是文件实际占用磁盘的大小：按分配的磁盘块算（稀疏文件、压缩过的系统文件都按实际的算），
// iCloud 里「仅在云端」的文件算 0，和 Windows 上 OneDrive 的在线文件一样。
func diskSize(info fs.FileInfo) int64 {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.Size()
	}
	if st.Flags&sfDataless != 0 {
		return 0
	}
	return st.Blocks * 512
}

func isAdmin() bool     { return os.Geteuid() == 0 }
func enablePrivileges() {}

// diskSpace 是 path 所在磁盘的总容量和剩余空间。APFS 上同一个容器里的宗卷共用空间，
// 所以「/」的总容量就是整块磁盘，剩余空间不含「可清除」的部分。
func diskSpace(path string) (total, free int64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize), nil
}

func devOf(path string) (uint64, bool) {
	var st syscall.Stat_t
	if syscall.Lstat(path, &st) != nil {
		return 0, false
	}
	return uint64(st.Dev), true
}

// isVolumeRoot 表示 path 是一个磁盘的根（「/」或者 /Volumes/U盘 这种挂载点）
func isVolumeRoot(path string) bool {
	path = filepath.Clean(path)
	if path == "/" {
		return true
	}
	d, ok1 := devOf(path)
	pd, ok2 := devOf(filepath.Dir(path))
	return ok1 && ok2 && d != pd
}

// startupDiskName 是启动磁盘的名字（一般是「Macintosh HD」）：/Volumes 里指向「/」的那个链接
func startupDiskName() string {
	entries, _ := os.ReadDir("/Volumes")
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink == 0 {
			continue
		}
		if t, err := os.Readlink(filepath.Join("/Volumes", e.Name())); err == nil && t == "/" {
			return e.Name()
		}
	}
	return "Macintosh HD"
}

// readSysVolumes 读出这台 Mac 的宗卷布局（见 scanpolicy.go）
func readSysVolumes() sysVolumes {
	var v sysVolumes
	v.rootDev, _ = devOf("/")
	if d, ok := devOf(dataVolume); ok && d != v.rootDev {
		v.dataDev = d
	}
	v.mounts = map[string]uint64{}
	entries, _ := os.ReadDir("/System/Volumes")
	for _, e := range entries {
		p := filepath.Join("/System/Volumes", e.Name())
		if p == dataVolume || !e.IsDir() {
			continue
		}
		if d, ok := devOf(p); ok && d != v.rootDev {
			v.mounts[p] = d
		}
	}
	v.firmlinks = defaultFirmlinks
	if f, err := os.Open("/usr/share/firmlinks"); err == nil {
		if fl := parseFirmlinks(f); len(fl) > 0 {
			v.firmlinks = fl
		}
		f.Close()
	}
	return v
}

// ---------------------------------------------------------------- 扫描

type macHooks struct {
	policy *mountPolicy
}

func newScanHooks(root string) scanHooks {
	return &macHooks{policy: newMountPolicy(root, readSysVolumes())}
}

func (h *macHooks) displayName(root string) string {
	if filepath.Clean(root) == "/" {
		return startupDiskName()
	}
	return ""
}

func (h *macHooks) device(path string) uint64 {
	d, _ := devOf(path)
	return d
}

// enterDir：别的磁盘的挂载点不出现；iCloud 里内容还在云端的文件夹出现但不进去
// （读它会触发下载，而且不占这台 Mac 的空间）
func (h *macHooks) enterDir(path string, parentDev uint64, e fs.DirEntry) (show, enter bool, dev uint64) {
	info, err := e.Info()
	if err != nil {
		return true, true, parentDev // 让读文件夹的时候再报错
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return true, true, parentDev
	}
	dev = uint64(st.Dev)
	if !h.policy.enter(path, parentDev, dev) {
		return false, false, dev
	}
	return true, st.Flags&sfDataless == 0, dev
}

// fileSize：按实际占用的磁盘块算；有多个硬链接的文件只算第一次遇到的那个
func (h *macHooks) fileSize(s *Scan, info fs.FileInfo, owner *Dir) int64 {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.Size()
	}
	if st.Flags&sfDataless != 0 {
		return 0
	}
	size := st.Blocks * 512
	if st.Nlink > 1 && size > 0 && !s.firstLink(uint64(st.Dev), st.Ino, owner) {
		return 0
	}
	return size
}

// privacyDenied：被「隐私与安全性」拦下时系统返回 EPERM（「Operation not permitted」），
// 普通的权限不够是 EACCES（「Permission denied」）
func (h *macHooks) privacyDenied(err error) bool { return errors.Is(err, syscall.EPERM) }

// ---------------------------------------------------------------- 权限

// hasFullDiskAccess 看程序有没有「完全磁盘访问权限」：读一个只有拿到这个权限才能读的文件
func hasFullDiskAccess() bool {
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		"/Library/Application Support/com.apple.TCC/TCC.db",
		filepath.Join(home, "Library/Application Support/com.apple.TCC/TCC.db"),
		filepath.Join(home, "Library/Safari/Bookmarks.plist"),
	} {
		f, err := os.Open(p)
		if err == nil {
			f.Close()
			return true
		}
		if errors.Is(err, syscall.EPERM) {
			return false
		}
	}
	return true // 判断不出来就当作有，不去烦用户
}

// clearUserLock 解开访达里「已锁定」的文件（uchg 标记），解开了返回 true。
// 系统级的锁（schg）只有关掉系统保护才能动，不碰。
func clearUserLock(path string) bool {
	var st syscall.Stat_t
	if syscall.Lstat(path, &st) != nil || st.Flags&ufImmutable == 0 || st.Mode&syscall.S_IFMT == syscall.S_IFLNK {
		return false
	}
	return syscall.Chflags(path, int(st.Flags&^ufImmutable)) == nil
}

// protection 判断能不能删，规则在 protect_mac.go
func protection(abs string) (block, warn string) {
	home, _ := os.UserHomeDir()
	return macProtection(abs, home)
}
