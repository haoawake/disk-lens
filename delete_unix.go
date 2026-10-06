//go:build !windows

package main

// macOS（以及其他类 Unix 系统）上的永久删除：unlink / rmdir，不跟随符号链接。
// 和 Windows 版忽略「只读」属性一样，确认过「我确定」之后，自己的东西上的小障碍顺手清掉：
// 访达里「已锁定」的文件先解锁；所在文件夹自己没有写权限（比如 Go 的模块缓存）时，
// 只要文件夹属于当前用户，就先给自己加上权限再删。别人的、系统的文件不碰。

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func isDirNotEmpty(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST)
}

// deleteEntry 删除一个文件、一个空文件夹或者一个链接（只删链接本身）
func deleteEntry(path string) error {
	err := os.Remove(path)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EPERM) && clearUserLock(path) {
		if err = os.Remove(path); err == nil {
			return nil
		}
	}
	if errors.Is(err, syscall.EACCES) && makeOwnDirWritable(filepath.Dir(path)) {
		err = os.Remove(path)
	}
	return err
}

// unlockDir 在打不开要删的文件夹时（自己的文件夹却没有读权限）给自己加上权限，返回要不要再试一次
func unlockDir(path string, err error) bool {
	return errors.Is(err, syscall.EACCES) && makeOwnDirWritable(path)
}

// makeOwnDirWritable 给属于当前用户、但自己没有读写权限的文件夹加上 rwx，改了返回 true
func makeOwnDirWritable(dir string) bool {
	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() {
		return false
	}
	mode := fi.Mode().Perm()
	if mode&0o700 == 0o700 {
		return false
	}
	return os.Chmod(dir, mode|0o700) == nil
}

func deleteErrorText(err error) string {
	switch {
	case errors.Is(err, syscall.EACCES):
		return "没有权限（属于系统或者其他用户）"
	case errors.Is(err, syscall.EPERM):
		return "被 macOS 保护（系统文件，或者需要「完全磁盘访问权限」）"
	case errors.Is(err, syscall.ENOENT):
		return "已经不存在"
	case errors.Is(err, syscall.ENOTEMPTY), errors.Is(err, syscall.EEXIST):
		return "文件夹里还有删不掉的文件"
	case errors.Is(err, syscall.EBUSY):
		return "正被使用"
	case errors.Is(err, syscall.EROFS):
		return "这个磁盘是只读的（比如 NTFS 格式的移动硬盘，Mac 只能读不能写）"
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
