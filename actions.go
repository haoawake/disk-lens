//go:build windows

package main

// 用户操作：选盘扫描、进出文件夹、右键菜单、移到回收站、永久删除。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"
)

// ---------------------------------------------------------------- 起始页和扫描

func (a *app) showHome() {
	a.mode = modeHome
	a.hover = nil
	a.homeScroll = 0
	a.loadDrives()
	a.layout()
	invalidate(a.hwnd, nil)
}

func (a *app) loadDrives() {
	if a.drivesBusy {
		return
	}
	a.drivesBusy = true
	go func() {
		d := listDrives()
		// 交回界面线程再改数据
		pendingDrives.Store(&d)
		postMessage(a.hwnd, wmDrivesLoaded, 0, 0)
	}()
}

func (a *app) startScan(path string) {
	if a.del != nil {
		a.setToast("正在删除文件，删完再扫描吧", true)
		return
	}
	abs, err := filepath.Abs(strings.Trim(strings.TrimSpace(path), `"`))
	if err == nil {
		if vol := filepath.VolumeName(abs); vol != "" && abs == vol {
			abs += `\` // "C:" 是「C 盘的当前目录」，要的是 "C:\"
		}
	}
	var s *Scan
	if err == nil {
		s, err = NewScan(abs)
	}
	if err != nil {
		showMessage(a.hwnd, tdErrorIcon, appName, "没法扫描这个位置", abs+"\n\n"+friendlyError(err))
		return
	}
	if a.scan != nil {
		a.scan.Stop()
	}
	a.scan = s
	a.diskTotal, a.diskFree, _ = diskSpace(abs)
	a.path, a.back, a.fwd, a.selected, a.hover = nil, nil, nil, nil, nil
	a.lastState = ""
	a.mode = modeView
	a.lv.selectName("", false)
	a.layout()
	a.refresh(true)
	a.startActivity()
	invalidate(a.hwnd, nil)
}

// rescanAll 重新扫描整个盘，尽量停在当前文件夹
func (a *app) rescanAll() {
	if a.scan == nil || a.del != nil {
		return
	}
	path, back := a.path, a.back
	a.startScan(a.scan.RootPath)
	a.path, a.back = path, back
	a.refresh(true)
}

func (a *app) scanTypedPath() {
	p := strings.TrimSpace(windowText(a.edit))
	if p == "" {
		a.setToast("先输入一个文件夹路径", true)
		pSetFocus.Call(a.edit)
		return
	}
	a.startScan(p)
}

func (a *app) pickAndScan() {
	p, err := pickFolder("选择要分析的文件夹")
	if err != nil {
		showMessage(a.hwnd, tdErrorIcon, appName, err.Error(), "")
		return
	}
	if p != "" {
		a.startScan(p)
	}
}

func (a *app) relaunchAdmin() {
	var args []string
	if a.scan != nil && a.mode == modeView {
		args = append(args, a.scan.RootPath)
	}
	err := relaunchAsAdmin(args)
	if errors.Is(err, errCancelled) {
		return
	}
	if err != nil {
		showMessage(a.hwnd, tdErrorIcon, appName, "没能以管理员身份启动", err.Error())
		return
	}
	// 新的管理员窗口已经在启动了，这个可以关掉
	if a.scan != nil {
		a.scan.Stop()
	}
	pDestroyWindow.Call(a.hwnd)
}

// ---------------------------------------------------------------- 刷新画面

// refresh 从扫描结果重新取当前文件夹的数据，重新排版、画底图、更新列表。
// newFolder 为 true 表示换了文件夹，列表滚回顶部。
func (a *app) refresh(newFolder bool) {
	if a.scan == nil || a.mode != modeView || a.rMap.W() <= 0 || a.rMap.H() <= 0 {
		return
	}
	area := detailArea[a.detail] * a.s * a.s
	root, at, list := a.scan.View(a.path, ViewOpts{W: float64(a.rMap.W()), H: float64(a.rMap.H()), MinArea: area, Max: detailMax[a.detail]}, 1<<30)
	if !samePath(at, a.path) {
		a.path = at
		newFolder = true
	}
	a.viewRoot = root
	a.tree = layoutTree(root, rect{0, 0, a.rMap.W(), a.rMap.H()}, mapMetrics{header: a.scale(19), pad: a.scale(3)})
	a.renderMap()

	scanning := a.scan.Busy()
	rows := make([]row, len(list))
	for i, v := range list {
		rows[i] = row{name: v.N, size: v.S, dir: v.D, files: v.F, denied: v.X, scanning: scanning && v.P}
	}
	a.lv.setRows(rows, root.S, !newFolder)
	// 虚拟列表按行号记选中项；扫描中顺序会变，所以每次按名字重新选一遍
	name := ""
	if len(a.selected) > len(a.path) && hasPrefix(a.selected, a.path) {
		name = a.selected[len(a.path)]
	}
	a.lv.selectName(name, newFolder)
	if a.mouseIn {
		a.hover = a.hitMap(a.mouse.X, a.mouse.Y)
	}
	if a.sideOpen && a.sideHeadHeight() != a.rSideHead.H() {
		a.layout() // 摘要下面多了或少了提示，列表要跟着挪
	}
	invalidate(a.hwnd, nil)
}

func (a *app) renderMap() {
	if a.tree == nil {
		return
	}
	dc, _, _ := pGetDC.Call(a.hwnd)
	a.mapBuf.ensure(dc, a.rMap.W(), a.rMap.H())
	pReleaseDC.Call(a.hwnd, dc)
	a.style.scanningNow = a.scan != nil && a.scan.Busy()
	renderTree(a.mapBuf.dc, a.tree, &a.style)
}

// ---------------------------------------------------------------- 定时器

func (a *app) startActivity() {
	pSetTimer.Call(a.hwnd, timerActivity, 100, 0)
}

func (a *app) onTimer(id uintptr) {
	switch id {
	case timerActivity:
		a.frame++
		a.ticks++
		active := false
		if a.scan != nil {
			st := a.scan.Status().State
			if st == "scanning" {
				active = true
				if a.ticks%4 == 0 {
					a.refresh(false)
				}
			} else if a.lastState == "scanning" || a.lastState == "" {
				a.refresh(false) // 刚扫完：最后完整刷新一次
			}
			a.lastState = st
		}
		if a.del != nil {
			active = true
		}
		invalidate(a.hwnd, &a.rTop)
		if !active {
			pKillTimer.Call(a.hwnd, timerActivity)
			invalidate(a.hwnd, nil)
		}
	case timerToast:
		pKillTimer.Call(a.hwnd, timerToast)
		a.toast = ""
		invalidate(a.hwnd, nil)
	case timerAnim:
		if a.anim == nil || time.Since(a.anim.start) >= a.anim.dur {
			a.anim = nil
			pKillTimer.Call(a.hwnd, timerAnim)
			if a.mouseIn {
				a.hover = a.hitMap(a.mouse.X, a.mouse.Y)
			}
		}
		invalidate(a.hwnd, &a.rMap)
	}
}

func (a *app) setToast(msg string, isErr bool) {
	a.toast, a.toastErr = msg, isErr
	pSetTimer.Call(a.hwnd, timerToast, 3500, 0)
	invalidate(a.hwnd, nil)
}

// ---------------------------------------------------------------- 导航

// navigate 进入 parts 这个文件夹。zoom 不为 nil 时播放「钻进去」的动画（zoom 是它在树图上的位置）。
func (a *app) navigate(parts []string, push bool, zoom *rect) {
	if a.scan == nil || samePath(parts, a.path) {
		return
	}
	old := a.path
	if push {
		a.back = append(a.back, old)
		a.fwd = nil
	}
	full := rect{0, 0, a.rMap.W(), a.rMap.H()}
	if zoom != nil && a.mapBuf.dc != 0 {
		// 钻进去：旧底图里这个文件夹的区域放大到整个画面
		dc, _, _ := pGetDC.Call(a.hwnd)
		a.oldBuf.ensure(dc, a.mapBuf.w, a.mapBuf.h)
		pReleaseDC.Call(a.hwnd, dc)
		blit(a.oldBuf.dc, 0, 0, a.mapBuf.w, a.mapBuf.h, a.mapBuf.dc, 0, 0)
		a.anim = &zoomAnim{from: full, to: *zoom, useOld: true, start: time.Now(), dur: 170 * time.Millisecond}
	}
	a.path = clonePath(parts)
	a.hover = nil
	a.refresh(true)
	if zoom == nil && len(parts) < len(old) && hasPrefix(old, parts) {
		// 退出来：新底图里刚才那个文件夹的区域缩回去
		a.selected = clonePath(old)
		a.lv.selectName(old[len(parts)], true)
		if t := findTile(a.tree, old[len(parts):]); t != nil && t.r.W() > 4 && t.r.H() > 4 {
			a.anim = &zoomAnim{from: t.r, to: full, start: time.Now(), dur: 170 * time.Millisecond}
		}
	}
	if a.anim != nil {
		pSetTimer.Call(a.hwnd, timerAnim, 15, 0)
	}
	invalidate(a.hwnd, nil)
}

func (a *app) goUp() {
	if a.mode == modeView && len(a.path) > 0 {
		a.navigate(a.path[:len(a.path)-1], true, nil)
	}
}

func (a *app) goBack() {
	if a.mode != modeView || len(a.back) == 0 {
		return
	}
	p := a.back[len(a.back)-1]
	a.back = a.back[:len(a.back)-1]
	a.fwd = append(a.fwd, a.path)
	a.navigate(p, false, nil)
}

func (a *app) goForward() {
	if a.mode != modeView || len(a.fwd) == 0 {
		return
	}
	p := a.fwd[len(a.fwd)-1]
	a.fwd = a.fwd[:len(a.fwd)-1]
	a.back = append(a.back, a.path)
	a.navigate(p, false, nil)
}

// click 处理按钮、卡片、路径的点击
func (a *app) click(id int) {
	switch {
	case id == hHome:
		a.showHome()
	case id == hResume:
		if a.scan != nil {
			a.mode = modeView
			a.layout()
			a.refresh(true)
			if a.scan.Busy() {
				a.startActivity()
			}
		}
	case id == hBack:
		a.goBack()
	case id == hUp:
		a.goUp()
	case id == hStop:
		if a.del != nil {
			a.del.stop.Store(true)
		} else if a.scan != nil {
			a.scan.Stop()
		}
	case id == hRescan:
		a.rescanAll()
	case id == hAdmin || id == hAdminLink:
		a.relaunchAdmin()
	case id == hSide:
		a.sideOpen = !a.sideOpen
		a.layout()
		a.refresh(false)
	case id == hPick:
		a.pickAndScan()
	case id == hScanPath:
		a.scanTypedPath()
	case id >= hDrive && id < hDrive+100:
		if i := id - hDrive; i < len(a.drives) && a.drives[i].Ready {
			a.startScan(a.drives[i].Path)
		}
	case id >= hCrumb:
		if i := id - hCrumb; i <= len(a.path) {
			a.navigate(a.path[:i], true, nil)
		}
	case id >= hDetail && id < hDetail+3:
		a.detail = id - hDetail
		a.refresh(false)
	}
	invalidate(a.hwnd, nil)
}

// ---------------------------------------------------------------- 选中、打开

func (a *app) fullParts(t *tile) []string {
	if t == nil {
		return nil
	}
	if t.kind == tileRest {
		return append(clonePath(a.path), t.parent.parts()...)
	}
	return append(clonePath(a.path), t.parts()...)
}

func (a *app) selectTile(t *tile) {
	if t == nil || t.kind == tileRest {
		a.selected = nil
		a.lv.selectName("", false)
	} else {
		a.selected = a.fullParts(t)
		// 列表里只有当前文件夹的直接子项：选中的是更深的，就选中它所在的那一项
		a.lv.selectName(a.selected[len(a.path)], true)
	}
	a.invalidateMap()
	invalidate(a.hwnd, &a.rFoot)
}

// openTile 双击方块：文件夹就钻进去，文件和「小项目」进入它所在的文件夹
func (a *app) openTile(t *tile) {
	if t == nil {
		return
	}
	target := t
	if t.kind != tileDir {
		target = t.parent
	}
	if target == nil || target == a.tree {
		return
	}
	r := target.r
	a.navigate(a.fullParts(target), true, &r)
}

// openParts 打开一项：文件夹进入，文件交给系统默认程序
func (a *app) openParts(parts []string) {
	if a.isDir(parts) {
		var zoom *rect
		if len(parts) > len(a.path) && hasPrefix(parts, a.path) {
			if t := findTile(a.tree, parts[len(a.path):]); t != nil {
				r := t.r
				zoom = &r
			}
		}
		a.navigate(parts, true, zoom)
		return
	}
	if err := shellOpen(a.scan.AbsPath(parts)); err != nil {
		a.setToast("打不开："+err.Error(), true)
	}
}

func (a *app) isDir(parts []string) bool {
	_, _, _, dir, _ := a.scan.Info(parts)
	return dir
}

func (a *app) copyPath(parts []string) {
	if setClipboardText(a.hwnd, a.scan.AbsPath(parts)) {
		a.setToast("已复制路径", false)
	}
}

// ---------------------------------------------------------------- 右键菜单

const (
	cmdEnter = iota + 1
	cmdOpen
	cmdReveal
	cmdCopy
	cmdProps
	cmdRescan
	cmdTrash
	cmdDelete
)

func (a *app) tileMenu(t *tile) {
	if t.kind == tileRest {
		if t.parent != a.tree {
			m := newMenu()
			m.add(cmdEnter, "进入所在的文件夹", true)
			if m.show(a.hwnd) == cmdEnter {
				a.openTile(t)
			}
		}
		return
	}
	a.itemMenu(a.fullParts(t))
}

func (a *app) itemMenu(parts []string) {
	_, _, _, dir, ok := a.scan.Info(parts)
	if !ok {
		return
	}
	busy := a.scan.Busy() || a.del != nil
	m := newMenu()
	if dir {
		m.add(cmdEnter, "进入", true)
		m.add(cmdOpen, "用资源管理器打开", true)
	} else {
		m.add(cmdOpen, "打开", true)
	}
	m.add(cmdReveal, "在资源管理器中显示", true)
	m.add(cmdCopy, "复制路径\tCtrl+C", true)
	m.add(cmdProps, "属性", true)
	if dir {
		m.sep()
		m.add(cmdRescan, "重新扫描这个文件夹", !busy)
	}
	m.sep()
	suffix := ""
	if busy {
		suffix = "（等扫描完成）"
	}
	m.add(cmdTrash, "移到回收站"+suffix+"\tDelete", !busy)
	m.add(cmdDelete, "永久删除…"+suffix+"\tShift+Delete", !busy)
	if dir {
		pSetMenuDefaultItem.Call(m.h, cmdEnter, 0)
	}
	abs := a.scan.AbsPath(parts)
	switch m.show(a.hwnd) {
	case cmdEnter:
		a.openParts(parts)
	case cmdOpen:
		if err := shellOpen(abs); err != nil {
			a.setToast("打不开："+err.Error(), true)
		}
	case cmdReveal:
		_ = revealFile(abs)
	case cmdCopy:
		a.copyPath(parts)
	case cmdProps:
		showProperties(abs)
	case cmdRescan:
		if err := a.scan.Rescan(parts); err != nil {
			a.setToast(err.Error(), true)
		} else {
			a.lastState = "scanning"
			a.startActivity()
			a.refresh(false)
		}
	case cmdTrash:
		a.trash(parts)
	case cmdDelete:
		a.deletePermanently(parts)
	}
}

type menu struct{ h uintptr }

func newMenu() *menu {
	h, _, _ := pCreatePopupMenu.Call()
	return &menu{h}
}

func (m *menu) add(id int, label string, enabled bool) {
	flags := uintptr(0)
	if !enabled {
		flags |= 0x1 // MF_GRAYED
	}
	pAppendMenuW.Call(m.h, flags, uintptr(id), uintptr(unsafe.Pointer(u16(label))))
}

func (m *menu) sep() { pAppendMenuW.Call(m.h, 0x800, 0, 0) }

// show 在鼠标位置弹出菜单，返回选中的命令（没选返回 0）
func (m *menu) show(hwnd uintptr) int {
	defer pDestroyMenu.Call(m.h)
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	r, _, _ := pTrackPopupMenuEx.Call(m.h, 0x100|0x2 /* TPM_RETURNCMD|TPM_RIGHTBUTTON */, uintptr(pt.X), uintptr(pt.Y), hwnd, 0)
	return int(r)
}

// ---------------------------------------------------------------- 删除

// describe 给确认框用：「大小：…（N 个文件）\n位置：…」
func (a *app) describe(parts []string) (name string, size int64, body string, ok bool) {
	name, size, files, dir, ok := a.scan.Info(parts)
	if !ok {
		return
	}
	sz := humanSize(size)
	if dir {
		sz += fmt.Sprintf("（%s 个文件）", formatCount(files))
	}
	body = "大小：" + sz + "\n位置：" + a.scan.AbsPath(parts)
	return
}

func (a *app) canModify() bool {
	if a.scan == nil {
		return false
	}
	if a.del != nil {
		a.setToast("正在删除别的东西，等它删完", true)
		return false
	}
	if a.scan.Busy() {
		a.setToast("还在扫描，等扫描完成再删除", true)
		return false
	}
	return true
}

func (a *app) trash(parts []string) {
	if !a.canModify() {
		return
	}
	name, size, body, ok := a.describe(parts)
	if !ok {
		return
	}
	abs := a.scan.AbsPath(parts)
	block, warn := protection(abs)
	if block != "" {
		showMessage(a.hwnd, tdErrorIcon, appName, "不能删除「"+name+"」", block)
		return
	}
	td := taskDialog{
		title:       appName,
		instruction: "把「" + name + "」移到回收站？",
		content:     body + "\n\n删错了可以从回收站还原。",
		footer:      "注意：回收站里的东西仍然占着这个盘的空间，清空回收站后才会真正腾出来。想马上腾出空间，请用「永久删除」。",
		buttons:     []tdButton{{100, "移到回收站"}},
		cancel:      true,
		defaultBtn:  100,
	}
	if warn != "" {
		td.icon = tdWarningIcon
		td.content += "\n\n" + warn
		td.defaultBtn = idCancel
	}
	if r, _ := td.show(a.hwnd); r != 100 {
		return
	}
	err := moveToTrash(abs)
	if errors.Is(err, errCancelled) {
		return
	}
	if a.afterDelete(parts, abs) {
		a.setToast("已移到回收站：「"+name+"」 "+humanSize(size), false)
	} else if err != nil {
		showMessage(a.hwnd, tdErrorIcon, appName, "没能移到回收站", err.Error())
	} else {
		a.setToast("只删掉了一部分，有些文件可能正被使用", true)
	}
}

func (a *app) deletePermanently(parts []string) {
	if !a.canModify() {
		return
	}
	name, _, body, ok := a.describe(parts)
	if !ok {
		return
	}
	abs := a.scan.AbsPath(parts)
	block, warn := protection(abs)
	if block != "" {
		showMessage(a.hwnd, tdErrorIcon, appName, "不能删除「"+name+"」", block)
		return
	}
	content := body + "\n\n永久删除不进回收站，删了就找不回来了。"
	if warn != "" {
		content += "\n\n" + warn
	}
	footer := "提示：以管理员身份运行时，没有权限的文件也能删掉。"
	if isAdmin() {
		footer = "当前是管理员身份：没有权限的、只读的文件也会一并删除。"
	}
	td := taskDialog{
		title:       appName,
		instruction: "永久删除「" + name + "」？",
		content:     content,
		footer:      footer,
		verify:      "我确定要永久删除，不需要恢复",
		icon:        tdWarningIcon,
		buttons:     []tdButton{{101, "永久删除"}},
		cancel:      true,
		defaultBtn:  idCancel,
		needVerify:  101,
	}
	if r, verified := td.show(a.hwnd); r != 101 || (!verified && pTaskDialogIndirect.Find() == nil) {
		return
	}
	a.delName = name
	a.del = startDelete(abs, clonePath(parts), func() { postMessage(a.hwnd, wmDeleteDone, 0, 0) })
	a.startActivity()
	invalidate(a.hwnd, nil)
}

func (a *app) onDeleteDone() {
	j := a.del
	if j == nil {
		return
	}
	a.del = nil
	stopped := j.stop.Load()
	gone := a.afterDelete(j.parts, j.path)
	freed := humanSize(j.bytes.Load())
	switch {
	case gone:
		a.setToast(fmt.Sprintf("已永久删除「%s」，腾出了 %s", a.delName, freed), false)
	case stopped:
		a.setToast(fmt.Sprintf("已停止删除。删掉了 %s 个文件，腾出了 %s", formatCount(j.files.Load()), freed), false)
	default:
		j.mu.Lock()
		details := strings.Join(j.errs, "\n")
		j.mu.Unlock()
		if n := j.failed.Load(); n > int64(len(j.errs)) {
			details += fmt.Sprintf("\n……一共 %s 项", formatCount(n))
		}
		hint := "正被使用的文件，关掉对应的程序后再删一次就行。"
		if !isAdmin() {
			hint += "没有权限的，可以点顶栏的「管理员」以管理员身份运行后再删。"
		}
		(&taskDialog{
			title:       appName,
			instruction: "有些东西没删掉",
			content:     fmt.Sprintf("删掉了 %s 个文件，腾出了 %s；有 %s 项删不掉。\n\n%s", formatCount(j.files.Load()), freed, formatCount(j.failed.Load()), hint),
			expanded:    details,
			icon:        tdWarningIcon,
			buttons:     []tdButton{{idOK, "知道了"}},
		}).show(a.hwnd)
	}
	invalidate(a.hwnd, nil)
}

// afterDelete 删除（或者删了一半）之后同步扫描结果。返回 true 表示已经完全删掉了。
func (a *app) afterDelete(parts []string, abs string) bool {
	if _, err := os.Lstat(abs); err == nil {
		// 还在：删掉了一部分，重新扫一下这个文件夹，让数字对得上
		if a.isDir(parts) && a.scan.Rescan(parts) == nil {
			a.lastState = "scanning"
			a.startActivity()
		}
		a.refresh(false)
		return false
	}
	if len(parts) == 0 {
		// 整个扫描的文件夹都删了
		a.scan = nil
		a.showHome()
		return true
	}
	a.scan.Remove(parts)
	if hasPrefix(a.path, parts) {
		a.path = clonePath(parts[:len(parts)-1])
	}
	if a.selected != nil && hasPrefix(a.selected, parts) {
		a.selected = nil
	}
	a.refresh(true)
	return true
}

// ---------------------------------------------------------------- 列表

func (a *app) onListNotify(lp uintptr) (uintptr, bool) {
	if r, ok := a.lv.onNotify(lp); ok {
		return r, true
	}
	hdr := (*nmhdr)(ptr(lp))
	switch hdr.Code {
	case lvnItemChanged:
		nm := (*nmListView)(ptr(lp))
		if a.lv.quiet || nm.Changed&0x8 == 0 /* LVIF_STATE */ || nm.NewState&lvisSelected == 0 {
			return 0, true
		}
		if i := int(nm.Item); i >= 0 && i < len(a.lv.rows) {
			a.selected = append(clonePath(a.path), a.lv.rows[i].name)
			a.invalidateMap()
			invalidate(a.hwnd, &a.rFoot)
		}
		return 0, true
	case nmDblClk, nmReturn:
		if i := a.lv.selected(); i >= 0 && i < len(a.lv.rows) {
			parts := append(clonePath(a.path), a.lv.rows[i].name)
			if a.lv.rows[i].dir {
				a.openParts(parts)
			} else {
				_ = revealFile(a.scan.AbsPath(parts))
			}
		}
		return 0, true
	case nmRClick:
		item := *(*int32)(ptr(lp + unsafe.Sizeof(nmhdr{})))
		if i := int(item); i >= 0 && i < len(a.lv.rows) {
			parts := append(clonePath(a.path), a.lv.rows[i].name)
			a.selected = parts
			a.invalidateMap()
			a.itemMenu(parts)
		}
		return 0, true
	case lvnKeyDown:
		vk := *(*uint16)(ptr(lp + unsafe.Sizeof(nmhdr{})))
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
		case 'C':
			if keyDown(vkCtrl) && a.selected != nil {
				a.copyPath(a.selected)
			}
		}
		return 0, true
	}
	return 0, false
}
