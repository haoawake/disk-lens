//go:build darwin && !cgo

package main

// 没有 cgo 时（比如在 Windows 上 GOOS=darwin CGO_ENABLED=0 go vet）用的空实现：
// Mac 版的界面必须用 cgo 编译，这里只是让其余的 Go 代码也能被检查。

import (
	"fmt"
	"os"
)

func uiRun(bool) {
	fmt.Fprintln(os.Stderr, "Mac 版的界面需要用 cgo 编译（CGO_ENABLED=1）。可以用 -bench 路径 测试扫描速度。")
	os.Exit(1)
}

func uiPost(f func())                      {}
func uiAfter(int, func())                  {}
func uiTerminate()                         {}
func uiStartTimer()                        {}
func uiStopTimer()                         {}
func uiRunLoop(int)                        {}
func uiSetMode(int)                        {}
func uiSetTitle(string, string)            {}
func uiSetToolbar(toolbarState)            {}
func uiSetCrumbs([]string, []string)       {}
func uiSetFooter(string)                   {}
func uiSetLegend([]string, []uint32)       {}
func uiToast(string, bool)                 {}
func uiSetCards([]volCard, bool)           {}
func uiSetHomeTip(bool)                    {}
func uiListVolumes() []volInfo             { return nil }
func uiMapSize() (int32, int32, float64)   { return 0, 0, 1 }
func uiMapSetTiles([]*tile, bool, float64) {}
func uiMapSetMarks(*rect, *rect)           {}
func uiMapSetTip(*mapTip)                  {}
func uiMapSetMessage(string)               {}
func uiMapZoom(rect, rect, bool)           {}
func uiMapShotMouse(float64, float64)      {}
func uiListReload(int, bool)               {}
func uiListSelect(int, bool)               {}
func uiSetSide(sideInfo)                   {}
func uiAlert(alertSpec) (int, bool)        { return -1, false }
func uiPickFolder()                        {}
func uiPopupMenu([]menuItem)               {}
func uiTrash(string) string                { return "需要 cgo" }
func uiOpenFile(string) bool               { return false }
func uiReveal(string)                      {}
func uiCopyText(string)                    {}
func uiOpenURL(string)                     {}
func uiLog(s string)                       { fmt.Println(s) }
func uiCapture(string) bool                { return false }
