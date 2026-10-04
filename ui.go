//go:build windows

package main

// 主窗口：窗口过程、消息循环、鼠标键盘。界面除了右侧列表和路径输入框，全部自己画。

import (
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	modeHome = iota
	modeView
)

// 自定义消息
const (
	wmDrivesLoaded = wmApp + 1
	wmDeleteDone   = wmApp + 2
)

// 定时器
const (
	timerActivity = 1 // 扫描、删除进行中：转圈、刷新画面
	timerToast    = 2
	timerAnim     = 3
)

// 三档细节：最小方块面积（平方像素，按 96 DPI 算）和最多画多少块
var detailArea = [3]float64{90, 30, 10}
var detailMax = [3]int{3000, 8000, 16000}

type fonts struct {
	ui, uiBold, small, smallBold, title, big, icon, iconSmall, iconBig uintptr
}

type hotspot struct {
	id      int
	r       rect
	enabled bool
	tip     string
}

// hotspot 的编号
const (
	hHome = iota + 1
	hBack
	hUp
	hStop
	hRescan
	hAdmin
	hSide
	hResume
	hPick
	hScanPath
	hAdminLink
	hDetail = 100   // + 0..2
	hDrive  = 300   // + 序号（最多 26 个盘）
	hCrumb  = 10000 // + 序号，放在最后，路径再深也不会和别的编号撞上
)

type zoomAnim struct {
	from, to rect // 底图上的源区域（本地坐标），从 from 变到 to
	useOld   bool // 用旧底图（钻进去时）还是新底图（退出来时）
	start    time.Time
	dur      time.Duration
}

type app struct {
	hwnd    uintptr
	dpi     int32
	s       float64
	f       fonts
	icons   string // 图标字体
	appIcon uintptr

	mode       int
	scan       *Scan
	diskTotal  int64
	diskFree   int64
	path       []string   // 当前查看的文件夹（相对扫描起点）
	back, fwd  [][]string // 后退、前进
	selected   []string   // 选中的项（相对扫描起点），nil 表示没选
	hover      *tile
	mouse      point
	mouseIn    bool
	tree       *tile
	viewRoot   *VNode
	lastState  string
	detail     int
	sideOpen   bool
	frame      int
	ticks      int
	toast      string
	toastErr   bool
	del        *deleteJob
	delName    string
	drives     []Drive
	drivesBusy bool
	homeScroll int32
	homeHeight int32
	anim       *zoomAnim

	rTop, rMain, rMap, rSide, rSideHead, rList, rFoot rect
	hot                                               []hotspot
	hotID                                             int
	pressID                                           int
	editBox                                           rect

	buf, mapBuf, oldBuf buffer
	lv                  *listView
	edit                uintptr
	editProc            uintptr
	editBrush           uintptr
	style               mapStyle
}

var theApp *app

// 后台读完的磁盘列表，等界面线程来取
var pendingDrives atomic.Pointer[[]Drive]

func runApp(initial string) {
	windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE)
	if pSetProcessDpiAwarenessContext.Find() == nil {
		pSetProcessDpiAwarenessContext.Call(^uintptr(3)) // PER_MONITOR_AWARE_V2；正式版的程序清单里也声明了
	}
	icc := struct{ Size, ICC uint32 }{8, 0x1 | 0x4000} // ICC_LISTVIEW_CLASSES | ICC_STANDARD_CLASSES
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	startGdiplus()

	a := &app{sideOpen: true, detail: 1, hotID: -1, pressID: -1}
	theApp = a
	a.icons = "Segoe Fluent Icons"
	if !fontExists(a.icons) {
		a.icons = "Segoe MDL2 Assets" // Windows 10
	}

	inst := moduleHandle()
	cursor, _, _ := pLoadCursorW.Call(0, 32512)
	icon, _, _ := pLoadImageW.Call(inst, 1, 1 /* IMAGE_ICON */, 0, 0, 0x40|0x8000 /* LR_DEFAULTSIZE|LR_SHARED */)
	cls := wndClassEx{
		Style:     0x8 | 0x1 | 0x2, // CS_DBLCLKS | CS_VREDRAW | CS_HREDRAW
		WndProc:   syscall.NewCallback(wndProc),
		Instance:  inst,
		Icon:      icon,
		Cursor:    cursor,
		ClassName: u16("DiskLensWindow"),
		IconSm:    icon,
	}
	cls.Size = uint32(unsafe.Sizeof(cls))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&cls)))

	title := appName
	if isAdmin() {
		title += "（管理员）"
	}
	// 先建一个默认大小的窗口，拿到所在屏幕的 DPI 和工作区后再摆到中间
	hwnd, _, _ := pCreateWindowExW.Call(0x10 /* WS_EX_ACCEPTFILES */, uintptr(unsafe.Pointer(u16("DiskLensWindow"))),
		uintptr(unsafe.Pointer(u16(title))), wsOverlappedWindow|wsClipChildren,
		0x80000000, 0x80000000, 1200, 800, 0, 0, inst, 0)
	if hwnd == 0 {
		fatal("没法创建窗口")
	}
	a.placeWindow()
	allowDropFromExplorer(hwnd)
	pShowWindow.Call(hwnd, swShow)

	if initial != "" {
		a.startScan(initial)
	} else {
		a.showHome()
	}
	a.scheduleShot()

	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if a.preTranslate(&m) {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// placeWindow 按屏幕大小把窗口放在正中间
func (a *app) placeWindow() {
	mon, _, _ := pMonitorFromWindow.Call(a.hwnd, 2 /* MONITOR_DEFAULTTONEAREST */)
	mi := monitorInfo{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	wa := mi.Work
	w := min(int32(1440*a.s), wa.W()*88/100)
	h := min(int32(920*a.s), wa.H()*88/100)
	pSetWindowPos.Call(a.hwnd, 0, uintptr(wa.Left+(wa.W()-w)/2), uintptr(wa.Top+(wa.H()-h)/2), uintptr(w), uintptr(h), 0x4 /* SWP_NOZORDER */)
}

// 以管理员身份运行时，资源管理器（普通权限）拖进来的文件默认会被系统拦下，要明确放行
func allowDropFromExplorer(hwnd uintptr) {
	p := user32.NewProc("ChangeWindowMessageFilterEx")
	if p.Find() != nil {
		return
	}
	for _, m := range []uintptr{wmDropFiles, 0x004A /* WM_COPYDATA */, 0x0049 /* WM_COPYGLOBALDATA */} {
		p.Call(hwnd, m, 1 /* MSGFLT_ALLOW */, 0)
	}
}

func (a *app) scale(v float64) int32 { return int32(math.Round(v * a.s)) }

func (a *app) setDPI(dpi int32) {
	if dpi <= 0 {
		dpi = 96
	}
	a.dpi = dpi
	a.s = float64(dpi) / 96
	for _, f := range []uintptr{a.f.ui, a.f.uiBold, a.f.small, a.f.smallBold, a.f.title, a.f.big, a.f.icon, a.f.iconSmall, a.f.iconBig} {
		if f != 0 {
			pDeleteObject.Call(f)
		}
	}
	const face = "Microsoft YaHei UI"
	a.f = fonts{
		ui:        newFont(a.scale(13), 400, face),
		uiBold:    newFont(a.scale(13), 700, face),
		small:     newFont(a.scale(12), 400, face),
		smallBold: newFont(a.scale(12), 700, face),
		title:     newFont(a.scale(24), 700, face),
		big:       newFont(a.scale(24), 700, face),
		icon:      newFont(a.scale(16), 400, a.icons),
		iconSmall: newFont(a.scale(11), 400, a.icons),
		iconBig:   newFont(a.scale(24), 400, a.icons),
	}
	if a.lv != nil {
		a.lv.setFont(a.f.ui, a.s)
	}
	if a.edit != 0 {
		sendMessage(a.edit, wmSetFont, a.f.ui, 1)
	}
	hatch := a.style.hatch
	a.style = mapStyle{
		hatch: hatch,
		bg:    0xE3E7EE, dirBorder: 0xC3CAD6, dirHeader: 0xDCE2EB, dirHeader2: 0xE6EAF1, dirFill: 0xEDF0F5,
		dirText: 0x232A38, dirText2: 0x667080, tileText: 0x1A202C, tileText2: 0x485264,
		rest: 0xDCE1E8, restLine: 0xC5CCD7, scanning: 0x0A6CFF,
		font: a.f.small, fontBold: a.f.smallBold, fontSmall: a.f.small, scale: a.s,
	}
	if a.style.hatch == 0 {
		a.style.hatch, _, _ = pCreateHatchBrush.Call(3 /* HS_BDIAGONAL */, rgb(0xC5CCD7).colorref())
	}
}

// ---------------------------------------------------------------- 窗口过程

func wndProc(hwnd, m, wp, lp uintptr) uintptr {
	a := theApp
	switch m {
	case wmCreate:
		a.hwnd = hwnd
		mainWindow.Store(hwnd)
		dpi, _, _ := pGetDpiForWindow.Call(hwnd)
		a.setDPI(int32(dpi))
		a.lv = newListView(hwnd, a.f.ui, a.s)
		a.createEdit()
		return 0

	case wmSize:
		a.layout()
		a.refresh(false)
		invalidate(hwnd, nil)
		return 0

	case wmDpiChanged:
		a.setDPI(hi16(wp))
		r := (*rect)(ptr(lp))
		pSetWindowPos.Call(hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.W()), uintptr(r.H()), 0x4|0x10)
		a.layout()
		a.refresh(false)
		invalidate(hwnd, nil)
		return 0

	case wmGetMinMaxInfo:
		if a.s > 0 {
			mmi := (*[5]point)(ptr(lp))
			mmi[3] = point{a.scale(860), a.scale(560)}
		}
		return 0

	case wmEraseBkgnd:
		return 1

	case wmPaint:
		a.paint()
		return 0

	case wmCtlColorEdit:
		pSetTextColor.Call(wp, rgb(0x15171C).colorref())
		pSetBkColor.Call(wp, rgb(0xFFFFFF).colorref())
		if a.editBrush == 0 {
			a.editBrush, _, _ = pCreateSolidBrush.Call(rgb(0xFFFFFF).colorref())
		}
		return a.editBrush

	case wmSetCursor:
		if lo16(lp) == 1 /* HTCLIENT */ && wp == hwnd {
			id := 32512 // 箭头
			if a.hotID >= 0 {
				id = 32649 // 手
			}
			c, _, _ := pLoadCursorW.Call(0, uintptr(id))
			pSetCursor.Call(c)
			return 1
		}

	case wmMouseMove:
		a.onMouseMove(lo16(lp), hi16(lp))
		return 0

	case wmMouseLeave:
		a.mouseIn = false
		a.setHot(-1)
		if a.hover != nil {
			a.hover = nil
			a.invalidateMap()
			invalidate(hwnd, &a.rFoot)
		}
		return 0

	case wmLButtonDown:
		pSetFocus.Call(hwnd)
		x, y := lo16(lp), hi16(lp)
		if a.hotID >= 0 {
			a.pressID = a.hotID
			pSetCapture.Call(hwnd)
			invalidate(hwnd, nil)
			return 0
		}
		if a.mode == modeView && a.rMap.has(x, y) {
			a.selectTile(a.hitMap(x, y))
		}
		return 0

	case wmLButtonUp:
		if a.pressID >= 0 {
			id := a.pressID
			a.pressID = -1
			pReleaseCapture.Call()
			invalidate(hwnd, nil)
			if id == a.hotID {
				a.click(id)
			}
		}
		return 0

	case wmLButtonDblClk:
		x, y := lo16(lp), hi16(lp)
		if a.mode == modeView && a.rMap.has(x, y) {
			a.openTile(a.hitMap(x, y))
		} else if a.hotID >= 0 {
			a.click(a.hotID)
		}
		return 0

	case wmRButtonUp:
		x, y := lo16(lp), hi16(lp)
		if a.mode == modeView && a.rMap.has(x, y) {
			t := a.hitMap(x, y)
			a.selectTile(t)
			if t != nil {
				a.tileMenu(t)
			}
		}
		return 0

	case wmXButtonUp:
		if hi16(wp) == 1 {
			a.goBack()
		} else {
			a.goForward()
		}
		return 1

	case wmMouseWheel:
		if a.mode == modeHome {
			delta := hi16(wp)
			a.homeScroll -= delta * a.scale(60) / 120
			a.layout()
			invalidate(hwnd, nil)
		}
		return 0

	case wmKeyDown:
		if a.onKey(int(wp)) {
			return 0
		}

	case wmNotify:
		hdr := (*nmhdr)(ptr(lp))
		if a.lv != nil && hdr.HwndFrom == a.lv.hwnd {
			if r, ok := a.onListNotify(lp); ok {
				return r
			}
		}

	case wmTimer:
		if wp == timerShot {
			a.onShotTimer()
			return 0
		}
		if wp == timerDialogShot {
			a.onDialogShotTimer()
			return 0
		}
		a.onTimer(wp)
		return 0

	case wmDropFiles:
		buf := make([]uint16, 32768)
		pDragQueryFileW.Call(wp, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		pDragFinish.Call(wp)
		p := windows.UTF16ToString(buf)
		if fi, err := os.Stat(p); err == nil {
			if !fi.IsDir() {
				p = filepath.Dir(p)
			}
			a.startScan(p)
		}
		return 0

	case wmDrivesLoaded:
		if d := pendingDrives.Load(); d != nil {
			a.drives = *d
		}
		a.drivesBusy = false
		a.layout()
		invalidate(hwnd, nil)
		return 0

	case wmDeleteDone:
		a.onDeleteDone()
		return 0

	case wmClose:
		if a.del != nil {
			r, _ := (&taskDialog{title: appName, instruction: "正在删除文件，确定要退出吗？",
				content: "退出后删除会中断，已经删掉的不会恢复，剩下的保留不动。", icon: tdWarningIcon,
				buttons: []tdButton{{idOK, "退出"}}, cancel: true, defaultBtn: idCancel}).show(hwnd)
			if r != idOK {
				return 0
			}
			a.del.stop.Store(true)
		}
		if a.scan != nil {
			a.scan.Stop()
		}
		pDestroyWindow.Call(hwnd)
		return 0

	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

// preTranslate 处理全局快捷键（焦点在列表上时也要生效的那些）
func (a *app) preTranslate(m *msg) bool {
	if m.Hwnd == a.edit && a.edit != 0 {
		return false
	}
	switch m.Message {
	case wmSysKeyDown:
		switch m.WParam {
		case vkLeft:
			a.goBack()
			return true
		case vkRight:
			a.goForward()
			return true
		case vkUp:
			a.goUp()
			return true
		}
	case wmKeyDown:
		if m.WParam == vkF5 {
			a.rescanAll()
			return true
		}
	}
	return false
}

func (a *app) onKey(vk int) bool {
	if a.mode != modeView {
		return false
	}
	switch vk {
	case vkBack:
		a.goUp()
	case vkDelete:
		if a.selected != nil {
			if keyDown(vkShift) {
				a.deletePermanently(a.selected)
			} else {
				a.trash(a.selected)
			}
		}
	case vkReturn:
		if a.selected != nil {
			a.openParts(a.selected)
		}
	case vkEscape:
		a.selected = nil
		a.lv.selectName("", false)
		a.invalidateMap()
	case 'C':
		if keyDown(vkCtrl) && a.selected != nil {
			a.copyPath(a.selected)
		}
	default:
		return false
	}
	return true
}

func (a *app) onMouseMove(x, y int32) {
	if !a.mouseIn {
		a.mouseIn = true
		tme := trackMouseEvent{Flags: 0x2 /* TME_LEAVE */, HwndTrack: a.hwnd}
		tme.Size = uint32(unsafe.Sizeof(tme))
		pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
	}
	a.mouse = point{x, y}
	id := -1
	for _, h := range a.hot {
		if h.enabled && h.r.has(x, y) {
			id = h.id
			break
		}
	}
	a.setHot(id)
	if a.mode == modeView {
		var t *tile
		if a.rMap.has(x, y) && id < 0 {
			t = a.hitMap(x, y)
		}
		if t != a.hover {
			a.hover = t
			invalidate(a.hwnd, &a.rFoot)
		}
		if t != nil || a.hover != nil {
			a.invalidateMap() // 提示框跟着鼠标走
		}
	}
}

func (a *app) setHot(id int) {
	if id == a.hotID {
		return
	}
	a.hotID = id
	invalidate(a.hwnd, nil)
}

func (a *app) hitMap(x, y int32) *tile {
	if a.tree == nil || a.anim != nil {
		return nil
	}
	t := hitTest(a.tree, x-a.rMap.Left, y-a.rMap.Top)
	if t == a.tree {
		return nil
	}
	return t
}

func (a *app) invalidateMap() { invalidate(a.hwnd, &a.rMap) }

// ---------------------------------------------------------------- 路径输入框

func (a *app) createEdit() {
	const esAutoHScroll = 0x80
	a.edit, _, _ = pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16("EDIT"))), 0,
		wsChild|wsTabStop|esAutoHScroll, 0, 0, 100, 20, a.hwnd, 101, moduleHandle(), 0)
	sendMessage(a.edit, wmSetFont, a.f.ui, 1)
	sendMessage(a.edit, 0x1501 /* EM_SETCUEBANNER */, 1, uintptr(unsafe.Pointer(u16("或者粘贴一个文件夹路径，比如 D:\\Downloads"))))
	// 子类化：按回车就开始扫描
	a.editProc, _, _ = pSetWindowLongPtrW.Call(a.edit, ^uintptr(3) /* GWLP_WNDPROC = -4 */, syscall.NewCallback(editProc))
}

func editProc(hwnd, m, wp, lp uintptr) uintptr {
	a := theApp
	if m == wmKeyDown && wp == vkReturn {
		a.scanTypedPath()
		return 0
	}
	if m == wmChar && (wp == '\r' || wp == '\n') {
		return 0 // 不要「叮」一声
	}
	r, _, _ := pCallWindowProcW.Call(a.editProc, hwnd, m, wp, lp)
	return r
}
