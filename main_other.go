//go:build !windows

package main

import (
	"fmt"
	"io/fs"
	"os"
)

var shotFile, shotSteps string

// 界面只有 Windows 版；其他系统上只能用 -bench 测扫描速度

func runApp(string) {
	fmt.Fprintln(os.Stderr, "文件清理助手只支持 Windows。可以用 -bench 路径 测试扫描速度。")
	os.Exit(1)
}

func isAdmin() bool                   { return os.Geteuid() == 0 }
func enablePrivileges()               {}
func showError(msg string)            { fmt.Fprintln(os.Stderr, msg) }
func diskSize(info fs.FileInfo) int64 { return info.Size() }
