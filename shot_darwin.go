//go:build darwin

package main

// 截图模式（开发和自动测试用）：-shot 文件.png 在窗口打开、扫描结束后，按 -steps 依次操作，
// 再把窗口画成 PNG 存下来并退出。和 Windows 版的 shot_windows.go 用法一样，坐标是树图里的点。比如：
//
//	-shot a.png -steps "hover:300:200,shot:hover,dbl:300:200,wait:400" ~/Downloads
//
// 截图模式里弹出的对话框会自动截下来（文件名带 -alert1、-alert2…），然后替用户点「取消」；
// 带 ! 的 trash!、delete! 会勾上「我确定」并点确认，用来测试删除的整个流程。
// 每一步做了什么、提示条说了什么都打印到标准输出，测试脚本靠它检查结果。

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	shotFile, shotSteps string
	shotAlerts          int
	shotClick           = -1 // 下一个对话框点哪个按钮（-1 = 最后一个，一般是「取消」）
	shotCheck           bool // 下一个对话框要不要勾上勾选框
)

// logf 在截图模式下把一行日志写到标准输出
func logf(format string, args ...any) {
	if shotFile != "" {
		uiLog(fmt.Sprintf(format, args...))
	}
}

func (a *macApp) scheduleShot() {
	if shotFile != "" {
		uiAfter(500, a.shotPoll)
	}
}

func (a *macApp) idle() bool {
	return (a.scan == nil || !a.scan.Busy()) && a.del == nil && !a.volsBusy
}

func (a *macApp) shotPoll() {
	if !a.idle() {
		uiAfter(200, a.shotPoll)
		return
	}
	uiRunLoop(300) // 让最后一次刷新画出来
	for _, step := range strings.Split(shotSteps, ",") {
		step = strings.TrimSpace(step)
		if step == "" {
			continue
		}
		logf("STEP %s", step)
		a.shotStep(step)
	}
	uiRunLoop(300)
	ok := uiCapture(shotFile)
	logf("SHOT %s %v", shotFile, ok)
	uiTerminate()
}

func (a *macApp) waitUntil(cond func() bool, limit time.Duration) {
	deadline := time.Now().Add(limit)
	for !cond() && time.Now().Before(deadline) {
		uiRunLoop(100)
	}
}

func shotXY(arg string) (float64, float64) {
	xy := strings.Split(arg, ":")
	if len(xy) < 2 {
		return 0, 0
	}
	x, _ := strconv.ParseFloat(xy[0], 64)
	y, _ := strconv.ParseFloat(xy[1], 64)
	return x, y
}

func (a *macApp) shotStep(step string) {
	cmd, arg, _ := strings.Cut(step, ":")
	parts := func() []string {
		if arg == "" {
			return nil
		}
		return strings.Split(arg, "/")
	}
	switch cmd {
	case "open":
		a.openPath(arg)
		a.waitUntil(a.idle, 5*time.Minute)
	case "waitscan", "waitdel":
		a.waitUntil(a.idle, 5*time.Minute)
	case "cd":
		a.navigate(parts(), true, nil)
	case "select":
		a.selected = append(clonePath(a.path), parts()...)
		a.syncListSelection(true)
		a.updateMarks()
	case "hover", "click", "dbl", "right":
		x, y := shotXY(arg)
		uiMapShotMouse(x, y)
		px, py := int32(x*a.scale), int32(y*a.scale)
		a.mapMouse(mouseMove, px, py, 0)
		switch cmd {
		case "click":
			a.mapMouse(mouseDown, px, py, 1)
		case "dbl":
			a.mapMouse(mouseDown, px, py, 2)
		}
	case "up":
		a.goUp()
	case "back":
		a.goBack()
	case "detail":
		n, _ := strconv.Atoi(arg)
		a.command(mcDetail0 + n)
	case "side":
		if (arg != "0") != a.sideOpen {
			a.command(mcSide)
		}
	case "home":
		a.showHome()
		a.waitUntil(a.idle, 30*time.Second)
	case "resume":
		a.command(mcHome)
	case "fda":
		a.explainFDA()
	case "trash", "trash!", "delete", "delete!":
		if strings.HasSuffix(cmd, "!") {
			shotClick, shotCheck = 0, true
		}
		if strings.HasPrefix(cmd, "trash") {
			a.trash(parts())
		} else {
			a.deletePermanently(parts())
		}
		a.waitUntil(a.idle, 5*time.Minute)
	case "wait":
		ms, _ := strconv.Atoi(arg)
		uiRunLoop(ms)
	case "shot":
		ok := uiCapture(strings.TrimSuffix(shotFile, ".png") + "-" + arg + ".png")
		logf("SHOT %s %v", arg, ok)
	case "log":
		a.logState()
	}
}

// logState 把当前的状态打印成一行 JSON，测试脚本用它检查扫描和删除的结果
func (a *macApp) logState() {
	st := map[string]any{"mode": a.mode, "fda": a.fda}
	if a.scan != nil {
		s := a.scan.Status()
		st["root"] = a.scan.RootPath
		st["rootName"] = a.scan.RootName()
		st["path"] = a.path
		st["selected"] = a.selected
		st["files"] = s.Files
		st["dirs"] = s.Dirs
		st["bytes"] = s.Bytes
		st["denied"] = s.Denied
		st["privacy"] = s.Privacy
		st["state"] = s.State
		if a.viewRoot != nil {
			st["viewBytes"] = a.viewRoot.S
		}
		st["listRows"] = len(a.list)
		st["tiles"] = len(a.flat)
	}
	var vols []string
	for _, v := range a.vols {
		vols = append(vols, v.name+"="+v.path)
	}
	st["volumes"] = vols
	b, _ := json.Marshal(st)
	logf("STATE %s", b)
}
