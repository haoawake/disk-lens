//go:build windows

package main

// Windows 上扫描不需要额外处理：目录联接、符号链接本来就不会被当成文件夹跟进去，
// 文件大小用 diskSize（云端占位文件算 0）。
func newScanHooks(string) scanHooks { return nil }
