//go:build windows

package main

// 截图模式（开发时用）：-shot 文件.png 在窗口打开、扫描结束后，按 -steps 依次操作，
// 再把窗口截图存下来并退出。比如：-shot a.png -steps "cd:Users,hover:400:300" C:\

import (
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

var (
	shotFile      string
	shotSteps     string
	shotConfirm   bool
	shotConfirmID int
)

const (
	timerShot       = 9
	timerDialogShot = 10
)

// onDialogShotTimer 在对话框的模态循环里触发：截下对话框，然后取消它
func (a *app) onDialogShotTimer() {
	pKillTimer.Call(a.hwnd, timerDialogShot)
	dlg, _, _ := user32.NewProc("GetLastActivePopup").Call(a.hwnd)
	if dlg == 0 || dlg == a.hwnd {
		return
	}
	var r rect
	user32.NewProc("GetWindowRect").Call(dlg, uintptr(unsafe.Pointer(&r)))
	saveShot(dlg, r.W(), r.H(), 2, strings.TrimSuffix(shotFile, ".png")+"-dialog.png")
	if shotConfirm {
		sendMessage(dlg, 0x400+113 /* TDM_CLICK_VERIFICATION */, 1, 0)
		sendMessage(dlg, 0x400+102 /* TDM_CLICK_BUTTON */, uintptr(shotConfirmID), 0)
		return
	}
	sendMessage(dlg, 0x400+102 /* TDM_CLICK_BUTTON */, idCancel, 0)
}

func (a *app) scheduleShot() {
	if shotFile != "" {
		pSetTimer.Call(a.hwnd, timerShot, 300, 0)
	}
}

func (a *app) onShotTimer() {
	if a.scan != nil && a.scan.Busy() {
		return // 等扫描结束
	}
	if a.del != nil || a.anim != nil {
		return
	}
	pKillTimer.Call(a.hwnd, timerShot)
	for _, step := range strings.Split(shotSteps, ",") {
		cmd, arg, _ := strings.Cut(strings.TrimSpace(step), ":")
		switch cmd {
		case "cd":
			a.navigate(strings.Split(arg, "/"), true, nil)
			a.anim = nil
		case "hover", "click":
			xy := strings.Split(arg, ":")
			x, _ := strconv.Atoi(xy[0])
			y, _ := strconv.Atoi(xy[1])
			a.onMouseMove(int32(x), int32(y))
			if cmd == "click" {
				a.selectTile(a.hitMap(int32(x), int32(y)))
			}
		case "dbl":
			xy := strings.Split(arg, ":")
			x, _ := strconv.Atoi(xy[0])
			y, _ := strconv.Atoi(xy[1])
			a.openTile(a.hitMap(int32(x), int32(y)))
		case "up":
			a.goUp()
		case "detail":
			a.detail, _ = strconv.Atoi(arg)
			a.refresh(false)
		case "side":
			a.sideOpen = arg != "0"
			a.layout()
			a.refresh(false)
		case "home":
			a.showHome()
		case "trash", "delete", "trash!", "delete!":
			// 弹出确认框，截下对话框后点「取消」（带 ! 的点确认）
			shotConfirm = strings.HasSuffix(cmd, "!")
			shotConfirmID = 101 // 「永久删除」
			if strings.HasPrefix(cmd, "trash") {
				shotConfirmID = 100 // 「移到回收站」
			}
			pSetTimer.Call(a.hwnd, timerDialogShot, 900, 0)
			parts := strings.Split(arg, "/")
			if strings.HasPrefix(cmd, "trash") {
				a.trash(parts)
			} else {
				a.deletePermanently(parts)
			}
		case "wait":
			ms, _ := strconv.Atoi(arg)
			deadline := time.Now().Add(time.Duration(ms) * time.Millisecond)
			for time.Now().Before(deadline) {
				var m msg
				for {
					r, _, _ := user32.NewProc("PeekMessageW").Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 1)
					if r == 0 {
						break
					}
					pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
					pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
	user32.NewProc("UpdateWindow").Call(a.hwnd)
	saveWindowShot(a.hwnd, shotFile)
	pDestroyWindow.Call(a.hwnd)
}

func saveWindowShot(hwnd uintptr, file string) {
	cr := clientRect(hwnd)
	saveShot(hwnd, cr.W(), cr.H(), 1|2, file) // PW_CLIENTONLY | PW_RENDERFULLCONTENT
}

func saveShot(hwnd uintptr, w, h int32, flags uintptr, file string) {
	dc, _, _ := pGetDC.Call(hwnd)
	var b buffer
	b.ensure(dc, w, h)
	pPrintWindow.Call(hwnd, b.dc, flags)
	pReleaseDC.Call(hwnd, dc)
	bi := struct {
		Size                          uint32
		Width, Height                 int32
		Planes, BitCount              uint16
		Compression, SizeImage        uint32
		XPels, YPels, ClrUsed, ClrImp int32
	}{Width: w, Height: -h, Planes: 1, BitCount: 32}
	bi.Size = uint32(unsafe.Sizeof(bi))
	pix := make([]byte, w*h*4)
	pSelectObject.Call(b.dc, b.old)
	pGetDIBits.Call(b.dc, b.bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&pix[0])), uintptr(unsafe.Pointer(&bi)), 0)
	pSelectObject.Call(b.dc, b.bmp)
	b.free()
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	for i := 0; i < len(pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = pix[i+2], pix[i+1], pix[i], 255
	}
	f, err := os.Create(file)
	if err != nil {
		return
	}
	defer f.Close()
	_ = png.Encode(f, img)
}
