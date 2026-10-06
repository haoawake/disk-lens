//go:build windows

package main

// Windows 上的永久删除：以管理员身份运行时借助「备份/还原」特权，
// 连只读的、权限设得很死的文件也能删（正被程序占用的除外）。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// isDirNotEmpty 表示删文件夹时它里面还有东西
func isDirNotEmpty(err error) bool {
	var errno windows.Errno
	return errors.As(err, &errno) && errno == windows.ERROR_DIR_NOT_EMPTY
}

// unlockDir 在打不开要删的文件夹时试着补救，Windows 上没什么可做的
func unlockDir(string, error) bool { return false }

func deleteErrorText(err error) string {
	var errno windows.Errno
	if errors.As(err, &errno) {
		switch errno {
		case windows.ERROR_ACCESS_DENIED:
			return "没有权限"
		case windows.ERROR_SHARING_VIOLATION, windows.ERROR_LOCK_VIOLATION:
			return "正被其他程序使用"
		case windows.ERROR_FILE_NOT_FOUND, windows.ERROR_PATH_NOT_FOUND:
			return "已经不存在"
		case windows.ERROR_DIR_NOT_EMPTY:
			return "文件夹里还有删不掉的文件"
		}
	}
	return err.Error()
}

// deleteEntry 打开文件拿到「删除」权限再标记删除。带 FILE_FLAG_BACKUP_SEMANTICS 打开时，
// 开着还原特权的管理员会绕过文件的权限设置；POSIX 方式删除让名字立刻消失，
// 只读属性也一并忽略。老系统或 FAT32 盘不支持时退回普通删除。
func deleteEntry(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	const fileReadAttributes, fileWriteAttributes = 0x80, 0x100
	h, err := windows.CreateFile(p, windows.DELETE|fileReadAttributes|fileWriteAttributes,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)

	const dispDelete, dispPosix, dispIgnoreReadonly = 0x1, 0x2, 0x10
	flags := uint32(dispDelete | dispPosix | dispIgnoreReadonly)
	err = windows.SetFileInformationByHandle(h, windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&flags)), 4)
	if err == nil {
		return nil
	}
	// 普通删除不会忽略只读属性，先把属性清掉
	var basic struct {
		CreationTime, LastAccessTime, LastWriteTime, ChangeTime int64
		FileAttributes                                          uint32
		_                                                       uint32
	}
	const fileBasicInfo, fileDispositionInfo = 0, 4
	if windows.GetFileInformationByHandleEx(h, fileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic))) == nil &&
		basic.FileAttributes&windows.FILE_ATTRIBUTE_READONLY != 0 {
		basic.FileAttributes &^= windows.FILE_ATTRIBUTE_READONLY
		basic.CreationTime, basic.LastAccessTime, basic.LastWriteTime, basic.ChangeTime = 0, 0, 0, 0 // 0 表示不改
		_ = windows.SetFileInformationByHandle(h, fileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic)))
	}
	del := byte(1)
	return windows.SetFileInformationByHandle(h, fileDispositionInfo, &del, 1)
}

// protection 判断 abs 能不能删。block 不为空时坚决不让删；warn 不为空时可以删但要提醒。
func protection(abs string) (block, warn string) {
	clean := filepath.Clean(abs)
	same := func(base string) bool { return base != "" && strings.EqualFold(filepath.Clean(base), clean) }
	under := func(base string) bool {
		if base == "" {
			return false
		}
		rel, err := filepath.Rel(filepath.Clean(base), clean)
		return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, `..\`)
	}
	if vol := filepath.VolumeName(clean); vol != "" && (clean == vol || clean == vol+`\`) {
		return "不能删除整个磁盘。", ""
	}
	sysRoot := os.Getenv("SystemRoot")
	if same(sysRoot) {
		return "这是 Windows 系统文件夹，删了电脑就开不了机了。", ""
	}
	for _, name := range []string{"System32", "SysWOW64", "WinSxS", "Boot", "Fonts"} {
		if sysRoot != "" && same(filepath.Join(sysRoot, name)) {
			return "这是 Windows 的核心文件夹，删了系统会损坏。", ""
		}
	}
	profile := os.Getenv("USERPROFILE")
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramData"), profile, filepath.Dir(profile)} {
		if same(base) {
			return "这是系统的重要文件夹，不能整个删除。可以进到里面删具体的内容。", ""
		}
	}
	switch {
	case under(os.TempDir()) || under(filepath.Join(sysRoot, "Temp")):
		// 临时文件夹里的东西本来就是可以删的
	case under(sysRoot):
		warn = "这是 Windows 系统文件夹里的东西。除非你清楚它是干什么的（比如 Temp 里的临时文件），否则删除可能让系统出问题。"
	case under(os.Getenv("ProgramFiles")) || under(os.Getenv("ProgramFiles(x86)")):
		warn = "这是已安装软件的文件。直接删除会让这个软件打不开，卸载软件最好用「设置 → 应用」。"
	case under(os.Getenv("ProgramData")):
		warn = "这是软件的公共数据，删除后对应的软件可能出问题。"
	case under(filepath.Join(profile, "AppData")):
		warn = "这是软件保存的设置和数据（缓存一般可以放心删）。删除后对应的软件可能需要重新登录或设置。"
	}
	return "", warn
}
