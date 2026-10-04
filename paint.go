//go:build windows

package main

// 界面绘制：顶栏、起始页（磁盘卡片）、树图上的高亮和提示框、右侧摘要、底栏。

import (
	"fmt"
	"strings"
	"time"
	"unsafe"
)

// 配色：洁白、浅灰，强调色是蓝色
const (
	cBg         rgb = 0xF3F5F8
	cBar        rgb = 0xFBFCFD
	cLine       rgb = 0xE2E6ED
	cText       rgb = 0x15171C
	cText2      rgb = 0x5C616D
	cText3      rgb = 0x979CA8
	cAccent     rgb = 0x0A6CFF
	cAccentSoft rgb = 0xE6F0FF
	cBtn        rgb = 0xFFFFFF
	cBtnHover   rgb = 0xF2F5F9
	cBtnPress   rgb = 0xE6EAF0
	cBtnBorder  rgb = 0xD8DDE6
	cOK         rgb = 0x0E9F57
	cWarn       rgb = 0xB86200
	cDanger     rgb = 0xE5484D
	cSoft       rgb = 0xEEF1F5
)

// 图标字体（Segoe Fluent Icons / Segoe MDL2 Assets）里的字形
const (
	gBack    = ""
	gUp      = ""
	gRefresh = ""
	gStop    = ""
	gDrive   = ""
	gUSB     = ""
	gNetwork = ""
	gFolder  = ""
	gShield  = ""
	gPane    = ""
	gChevron = ""
	gCheck   = ""
	gWarn    = ""
)

func roundRect(dc uintptr, r rect, radius int32, c rgb) {
	g := newGP(dc)
	g.roundRect(r, radius, c, 255)
	g.close()
}

func roundBox(dc uintptr, r rect, radius int32, bg, border rgb) {
	g := newGP(dc)
	g.roundBox(r, radius, bg, border)
	g.close()
}

// ---------------------------------------------------------------- 排版

type homeLayout struct {
	title, sub rect
	cards      []rect
	pick, scan rect
	adminTip   rect
	adminBtn   rect
	hint       rect
}

var home homeLayout

func (a *app) measure(font uintptr, s string) int32 {
	if a.buf.dc == 0 {
		dc, _, _ := pGetDC.Call(a.hwnd)
		a.buf.ensure(dc, 1, 1)
		pReleaseDC.Call(a.hwnd, dc)
	}
	return textWidth(a.buf.dc, font, s)
}

func (a *app) btnWidth(label, glyph string) int32 {
	w := a.measure(a.f.ui, label) + a.scale(24)
	if glyph != "" {
		w += a.scale(22)
	}
	return w
}

// layout 根据窗口大小摆放各个区域和子窗口
func (a *app) layout() {
	if a.hwnd == 0 || a.s == 0 {
		return
	}
	cr := clientRect(a.hwnd)
	W, H := cr.W(), cr.H()
	a.rTop = rect{0, 0, W, a.scale(52)}
	a.rFoot = rect{0, H - a.scale(28), W, H}
	a.rMain = rect{0, a.rTop.Bottom, W, a.rFoot.Top}

	if a.mode == modeView {
		sideW := int32(0)
		if a.sideOpen {
			sideW = min(a.scale(430), W*42/100)
		}
		a.rMap = rect{0, a.rMain.Top, W - sideW, a.rMain.Bottom}
		a.rSide = rect{W - sideW, a.rMain.Top, W, a.rMain.Bottom}
		a.rSideHead = rect{a.rSide.Left, a.rSide.Top, W, a.rSide.Top + a.sideHeadHeight()}
		a.rList = rect{a.rSide.Left + 1, a.rSideHead.Bottom, W, a.rSide.Bottom}
		if a.sideOpen {
			pMoveWindow.Call(a.lv.hwnd, uintptr(a.rList.Left), uintptr(a.rList.Top), uintptr(a.rList.W()), uintptr(a.rList.H()), 1)
			a.lv.fitColumns(a.rList.W())
			pShowWindow.Call(a.lv.hwnd, swShow)
		} else {
			pShowWindow.Call(a.lv.hwnd, 0)
		}
		pShowWindow.Call(a.edit, 0)
		return
	}

	a.rMap, a.rSide, a.rList = rect{}, rect{}, rect{}
	pShowWindow.Call(a.lv.hwnd, 0)
	for pass := 0; pass < 2; pass++ {
		a.layoutHome()
		maxScroll := max(0, a.homeHeight-a.rMain.H())
		clamped := min(max(a.homeScroll, 0), maxScroll)
		if clamped == a.homeScroll {
			break
		}
		a.homeScroll = clamped
	}
	// 输入框跟着页面滚动；滚出可见区域时藏起来
	box := a.editBox
	eh := a.scale(20)
	ex := rect{box.Left + a.scale(10), box.Top + (box.H()-eh)/2, box.Right - a.scale(10), box.Top + (box.H()-eh)/2 + eh}
	pMoveWindow.Call(a.edit, uintptr(ex.Left), uintptr(ex.Top), uintptr(ex.W()), uintptr(ex.H()), 1)
	if box.Top >= a.rMain.Top && box.Bottom <= a.rMain.Bottom {
		pShowWindow.Call(a.edit, swShow)
	} else {
		pShowWindow.Call(a.edit, 0)
	}
}

func (a *app) layoutHome() {
	m := a.rMain
	gap := a.scale(14)
	cw := min(a.scale(1000), m.W()-2*a.scale(32))
	x0 := m.Left + (m.W()-cw)/2
	y := m.Top + a.scale(34) - a.homeScroll
	top := y
	home.title = rect{x0, y, x0 + cw, y + a.scale(36)}
	y += a.scale(40)
	home.sub = rect{x0, y, x0 + cw, y + a.scale(22)}
	y += a.scale(22) + a.scale(24)

	cols := max(1, (cw+gap)/(a.scale(290)+gap))
	cardW := (cw - (cols-1)*gap) / cols
	cardH := a.scale(96)
	home.cards = home.cards[:0]
	n := len(a.drives)
	for i := 0; i < n; i++ {
		cx := x0 + int32(i)%cols*(cardW+gap)
		cy := y + int32(i)/cols*(cardH+gap)
		home.cards = append(home.cards, rect{cx, cy, cx + cardW, cy + cardH})
	}
	rows := (int32(n) + cols - 1) / cols
	if n == 0 {
		rows = 1 // 「正在读取磁盘…」占一行
	}
	y += rows*(cardH+gap) + a.scale(10)

	bh := a.scale(36)
	pw := a.btnWidth("选择文件夹…", gFolder)
	home.pick = rect{x0, y, x0 + pw, y + bh}
	sw := a.btnWidth("扫描", "")
	home.scan = rect{x0 + cw - sw, y, x0 + cw, y + bh}
	a.editBox = rect{home.pick.Right + a.scale(10), y, home.scan.Left - a.scale(8), y + bh}
	y += bh + a.scale(26)

	home.adminTip, home.adminBtn = rect{}, rect{}
	if !isAdmin() {
		th := a.scale(66)
		home.adminTip = rect{x0, y, x0 + cw, y + th}
		bw := a.btnWidth("以管理员身份运行", gShield)
		home.adminBtn = rect{x0 + cw - a.scale(16) - bw, y + (th-bh)/2, x0 + cw - a.scale(16), y + (th-bh)/2 + bh}
		y += th + a.scale(18)
	}
	home.hint = rect{x0, y, x0 + cw, y + a.scale(20)}
	y += a.scale(20) + a.scale(30)
	a.homeHeight = y - top
}

// ---------------------------------------------------------------- 总入口

func (a *app) paint() {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(a.hwnd, uintptr(unsafe.Pointer(&ps)))
	cr := clientRect(a.hwnd)
	a.buf.ensure(hdc, cr.W(), cr.H())
	dc := a.buf.dc
	a.hot = a.hot[:0]

	fill(dc, cr, cBg)
	if a.mode == modeView {
		a.paintMap(dc)
		if a.sideOpen {
			a.paintSideHead(dc)
		}
	} else {
		a.paintHome(dc)
	}
	a.paintTop(dc)
	a.paintFoot(dc)
	a.paintTips(dc)

	p := ps.Paint
	blit(hdc, p.Left, p.Top, p.W(), p.H(), dc, p.Left, p.Top)
	pEndPaint.Call(a.hwnd, uintptr(unsafe.Pointer(&ps)))
}

func (a *app) addHot(id int, r rect, enabled bool, tip string) {
	a.hot = append(a.hot, hotspot{id: id, r: r, enabled: enabled, tip: tip})
}

// button 画一个圆角按钮，返回它的宽度
func (a *app) button(dc uintptr, x, y int32, label, glyph string, id int, primary, enabled bool) int32 {
	w := a.btnWidth(label, glyph)
	h := a.scale(32)
	r := rect{x, y, x + w, y + h}
	a.drawButton(dc, r, label, glyph, id, primary, enabled)
	return w
}

func (a *app) drawButton(dc uintptr, r rect, label, glyph string, id int, primary, enabled bool) {
	hot, pressed := a.hotID == id, a.pressID == id && a.hotID == id
	bg, border, fg := cBtn, cBtnBorder, cText
	switch {
	case primary:
		bg, border, fg = cAccent, cAccent, 0xFFFFFF
		if hot {
			bg = bg.mix(0xFFFFFF, 0.1)
			border = bg
		}
		if pressed {
			bg = bg.mix(0x000000, 0.1)
			border = bg
		}
	case pressed:
		bg = cBtnPress
	case hot:
		bg = cBtnHover
	}
	if !enabled {
		fg = cText3
	}
	roundBox(dc, r, a.scale(8), bg, border)
	x := r.Left + a.scale(12)
	if glyph != "" {
		gc := cText2
		if primary {
			gc = 0xFFFFFF
		}
		if !enabled {
			gc = cText3
		}
		textLine(dc, a.f.icon, glyph, rect{x, r.Top, x + a.scale(18), r.Bottom}, gc, dtLeft)
		x += a.scale(22)
	}
	textLine(dc, a.f.ui, label, rect{x, r.Top, r.Right - a.scale(8), r.Bottom}, fg, dtLeft)
	a.addHot(id, r, enabled, "")
}

func (a *app) iconButton(dc uintptr, x, y int32, glyph string, id int, enabled, on bool, tip string) int32 {
	sz := a.scale(32)
	r := rect{x, y, x + sz, y + sz}
	fg := cText2
	if a.hotID == id && enabled {
		bg := cBtnHover.mix(0x000000, 0.02)
		if a.pressID == id {
			bg = cBtnPress
		}
		roundRect(dc, r, a.scale(8), bg)
		fg = cText
	}
	if on {
		roundRect(dc, r, a.scale(8), cAccentSoft)
		fg = cAccent
	}
	if !enabled {
		fg = 0xC4C8D0
	}
	textLine(dc, a.f.icon, glyph, r, fg, dtCenter)
	a.addHot(id, r, enabled, tip)
	return sz
}

// ---------------------------------------------------------------- 顶栏

func (a *app) paintTop(dc uintptr) {
	r := a.rTop
	fill(dc, r, cBar)
	fill(dc, rect{r.Left, r.Bottom - 1, r.Right, r.Bottom}, cLine)
	cy := r.Top + (r.H()-a.scale(32))/2

	// 左边：图标和名字
	x := a.scale(16)
	if a.appIcon == 0 {
		a.appIcon, _, _ = pLoadImageW.Call(moduleHandle(), 1, 1, uintptr(a.scale(20)), uintptr(a.scale(20)), 0)
	}
	if a.appIcon != 0 {
		user32.NewProc("DrawIconEx").Call(dc, uintptr(x), uintptr(r.Top+(r.H()-a.scale(20))/2), a.appIcon, uintptr(a.scale(20)), uintptr(a.scale(20)), 0, 0, 3)
	}
	x += a.scale(28)
	nw := a.measure(a.f.uiBold, appName)
	textLine(dc, a.f.uiBold, appName, rect{x, r.Top, x + nw + 2, r.Bottom}, cText, dtLeft)
	x += nw + a.scale(8)
	if isAdmin() {
		bw := a.measure(a.f.small, "管理员") + a.scale(14)
		br := rect{x, r.Top + (r.H()-a.scale(20))/2, x + bw, r.Top + (r.H()-a.scale(20))/2 + a.scale(20)}
		roundRect(dc, br, a.scale(6), 0xFFF1DC)
		textLine(dc, a.f.small, "管理员", br, cWarn, dtCenter)
		x += bw + a.scale(8)
	}
	x += a.scale(8)

	right := r.Right - a.scale(12)
	if a.mode == modeHome {
		if !isAdmin() {
			w := a.btnWidth("以管理员身份运行", gShield)
			right -= w
			a.button(dc, right, cy, "以管理员身份运行", gShield, hAdmin, false, true)
			right -= a.scale(8)
		}
		if a.scan != nil {
			w := a.btnWidth("回到扫描结果", gBack)
			right -= w
			a.button(dc, right, cy, "回到扫描结果", gBack, hResume, false, true)
		}
		return
	}

	// 查看模式：换个盘、后退、上一级、路径
	x += a.button(dc, x, cy, "换个盘", gDrive, hHome, false, true) + a.scale(8)
	x += a.iconButton(dc, x, cy, gBack, hBack, len(a.back) > 0, false, "后退（Alt + ←）")
	x += a.iconButton(dc, x, cy, gUp, hUp, len(a.path) > 0, false, "上一级（Backspace）") + a.scale(6)

	// 右边：从右往左排
	right -= a.iconButton(dc, right-a.scale(32), cy, gPane, hSide, true, a.sideOpen, "显示或隐藏右侧列表")
	right -= a.scale(8)
	if r.W() > a.scale(1180) {
		labels := []string{"粗略", "适中", "精细"}
		var ws []int32
		total := a.scale(4)
		for _, l := range labels {
			w := a.measure(a.f.ui, l) + a.scale(18)
			ws = append(ws, w)
			total += w
		}
		seg := rect{right - total, cy, right, cy + a.scale(32)}
		roundRect(dc, seg, a.scale(9), 0xEBEEF3)
		sx := seg.Left + a.scale(2)
		for i, l := range labels {
			br := rect{sx, seg.Top + a.scale(2), sx + ws[i], seg.Bottom - a.scale(2)}
			c := cText2
			if a.detail == i {
				roundBox(dc, br, a.scale(7), 0xFFFFFF, 0xDDE1E8)
				c = cText
			} else if a.hotID == hDetail+i {
				c = cText
			}
			textLine(dc, a.f.ui, l, br, c, dtCenter)
			a.addHot(hDetail+i, br, true, "方块画得多细：越精细，小文件也越容易看到")
			sx += ws[i]
		}
		right = seg.Left - a.scale(8)
	}
	if !isAdmin() && r.W() > a.scale(1000) {
		w := a.btnWidth("管理员", gShield)
		right -= w
		a.button(dc, right, cy, "管理员", gShield, hAdmin, false, true)
		a.hot[len(a.hot)-1].tip = "以管理员身份重新打开：能统计到更多系统文件夹，也能删除没有权限的文件"
		right -= a.scale(8)
	}
	busy := a.scan != nil && a.scan.Busy()
	if busy || a.del != nil {
		w := a.btnWidth("停止", gStop)
		right -= w
		a.button(dc, right, cy, "停止", gStop, hStop, false, true)
	} else {
		w := a.btnWidth("重新扫描", gRefresh)
		right -= w
		a.button(dc, right, cy, "重新扫描", gRefresh, hRescan, false, a.scan != nil)
		a.hot[len(a.hot)-1].tip = "重新扫描整个盘（F5）"
	}
	right -= a.scale(12)

	// 状态文字
	right = a.paintStatus(dc, rect{x + a.scale(120), r.Top, right, r.Bottom})

	// 中间：面包屑路径
	a.paintCrumbs(dc, rect{x, r.Top, right - a.scale(12), r.Bottom})
}

// paintStatus 在 r 的右边画扫描进度，返回它左边界
func (a *app) paintStatus(dc uintptr, r rect) int32 {
	if a.scan == nil {
		return r.Right
	}
	var glyph string
	var gc rgb
	var parts []string
	spin := false
	if a.del != nil {
		spin = true
		parts = append(parts, "正在删除「"+a.delName+"」", fmt.Sprintf("已删除 %s 个文件", formatCount(a.del.files.Load())), "释放 "+humanSize(a.del.bytes.Load()))
	} else {
		st := a.scan.Status()
		secs := float64(st.Elapsed) / 1000
		switch st.State {
		case "scanning":
			spin = true
			parts = append(parts, "正在扫描", formatCount(st.Files)+" 个文件", humanSize(st.Bytes), fmt.Sprintf("%.0f 秒", secs))
		case "stopped":
			glyph, gc = gStop, cText3
			parts = append(parts, "已停止", formatCount(st.Files)+" 个文件", humanSize(st.Bytes))
		default:
			glyph, gc = gCheck, cOK
			parts = append(parts, "扫描完成", formatCount(st.Files)+" 个文件", humanSize(st.Bytes), formatSeconds(secs))
		}
	}
	text := strings.Join(parts, " · ")
	tw := a.measure(a.f.ui, text)
	avail := r.W() - a.scale(24)
	for tw > avail && len(parts) > 1 {
		parts = parts[:len(parts)-1]
		text = strings.Join(parts, " · ")
		tw = a.measure(a.f.ui, text)
	}
	if tw > avail {
		return r.Right
	}
	x := r.Right - tw
	textLine(dc, a.f.ui, text, rect{x, r.Top, r.Right, r.Bottom}, cText2, dtLeft)
	ix := x - a.scale(22)
	cy := r.Top + r.H()/2
	if spin {
		g := newGP(dc)
		g.spinner(ix+a.scale(8), cy, a.scale(6), max(a.scale(3), 2), a.frame, cAccent)
		g.close()
	} else if glyph != "" {
		textLine(dc, a.f.icon, glyph, rect{ix, r.Top, ix + a.scale(18), r.Bottom}, gc, dtLeft)
	}
	return ix
}

func (a *app) paintCrumbs(dc uintptr, r rect) {
	if a.scan == nil || r.W() < a.scale(60) {
		return
	}
	names := append([]string{a.scan.RootName()}, a.path...)
	sepW := a.scale(18)
	widths := make([]int32, len(names))
	for i, n := range names {
		font := a.f.ui
		if i == len(names)-1 {
			font = a.f.uiBold // 最后一段（当前文件夹）用粗体
		}
		widths[i] = min(a.measure(font, n), a.scale(220)) + a.scale(14)
	}
	// 放不下时从前面开始省略
	first := 0
	total := func(from int) int32 {
		t := int32(0)
		for i := from; i < len(names); i++ {
			t += widths[i] + sepW
		}
		if from > 0 {
			t += a.measure(a.f.ui, "…") + a.scale(12) + sepW
		}
		return t
	}
	for first < len(names)-1 && total(first) > r.W() {
		first++
	}
	x := r.Left
	cy := r.Top + (r.H()-a.scale(28))/2
	if first > 0 {
		w := a.measure(a.f.ui, "…") + a.scale(12)
		br := rect{x, cy, x + w, cy + a.scale(28)}
		a.crumb(dc, br, "…", hCrumb+first-1, false)
		x += w
		textLine(dc, a.f.iconSmall, gChevron, rect{x, r.Top, x + sepW, r.Bottom}, cText3, dtCenter)
		x += sepW
	}
	for i := first; i < len(names); i++ {
		w := min(widths[i], r.Right-x)
		if w <= a.scale(16) {
			break
		}
		last := i == len(names)-1
		a.crumb(dc, rect{x, cy, x + w, cy + a.scale(28)}, names[i], hCrumb+i, last)
		x += w
		if !last {
			textLine(dc, a.f.iconSmall, gChevron, rect{x, r.Top, x + sepW, r.Bottom}, cText3, dtCenter)
			x += sepW
		}
	}
}

func (a *app) crumb(dc uintptr, r rect, name string, id int, last bool) {
	if a.hotID == id && !last {
		roundRect(dc, r, a.scale(6), cBtnHover.mix(0x000000, 0.03))
	}
	font, c := a.f.ui, cText2
	if last {
		font, c = a.f.uiBold, cText
	}
	textLine(dc, font, name, rect{r.Left + a.scale(6), r.Top, r.Right - a.scale(6), r.Bottom}, c, dtLeft)
	if !last {
		a.addHot(id, r, true, "")
	}
}

// ---------------------------------------------------------------- 起始页

func (a *app) paintHome(dc uintptr) {
	save := saveDC(dc)
	clip(dc, a.rMain)
	textLine(dc, a.f.title, "选一个盘，看看空间都去哪了", home.title, cText, dtLeft)
	textLine(dc, a.f.ui, "扫描时边扫边画：方块越大，占的空间越多。双击文件夹钻进去，右键可以打开、定位或删除。", home.sub, cText2, dtLeft)

	if len(a.drives) == 0 {
		msg := "正在读取磁盘…"
		if !a.drivesBusy {
			msg = "没有找到磁盘"
		}
		textLine(dc, a.f.ui, msg, rect{home.title.Left, home.sub.Bottom + a.scale(24), home.title.Right, home.sub.Bottom + a.scale(60)}, cText3, dtLeft)
	}
	for i, d := range a.drives {
		if i >= len(home.cards) {
			break
		}
		a.paintDrive(dc, home.cards[i], d, hDrive+i)
	}

	a.drawButton(dc, home.pick, "选择文件夹…", gFolder, hPick, true, true)
	box := a.editBox
	roundBox(dc, box, a.scale(8), 0xFFFFFF, cBtnBorder)
	a.drawButton(dc, home.scan, "扫描", "", hScanPath, false, true)

	if home.adminTip.W() > 0 {
		t := home.adminTip
		roundRect(dc, t, a.scale(12), 0xEAEEF4)
		tx := t.Left + a.scale(18)
		tr := home.adminBtn.Left - a.scale(16)
		textLine(dc, a.f.uiBold, "想统计得更完整、删得更彻底？", rect{tx, t.Top + a.scale(12), tr, t.Top + a.scale(32)}, cText, dtLeft)
		textLine(dc, a.f.ui, "以管理员身份运行：能读到系统还原点、其他用户的文件夹，也能删除没有权限的文件。", rect{tx, t.Top + a.scale(33), tr, t.Top + a.scale(54)}, cText2, dtLeft)
		a.drawButton(dc, home.adminBtn, "以管理员身份运行", gShield, hAdmin, false, true)
	}
	textLine(dc, a.f.ui, "提示：也可以把文件夹直接拖进这个窗口。", home.hint, cText3, dtLeft)
	restoreDC(dc, save)
}

func driveTitle(d Drive) string {
	kind := "本地磁盘"
	switch d.Kind {
	case "removable":
		kind = "U 盘"
	case "network":
		kind = "网络位置"
	case "cdrom":
		kind = "光驱"
	}
	if d.Label != "" {
		kind = d.Label
	}
	return fmt.Sprintf("%s (%s)", kind, d.Name)
}

func (a *app) paintDrive(dc uintptr, r rect, d Drive, id int) {
	hot := a.hotID == id && d.Ready
	bg := rgb(0xFFFFFF)
	border := rgb(0xE1E5EC)
	if hot {
		border = 0xB9CCF0
		bg = 0xFAFCFF
	}
	roundBox(dc, r, a.scale(14), bg, border)
	pad := a.scale(16)
	ib := rect{r.Left + pad, r.Top + (r.H()-a.scale(46))/2, r.Left + pad + a.scale(46), r.Top + (r.H()-a.scale(46))/2 + a.scale(46)}
	roundRect(dc, ib, a.scale(12), cAccentSoft)
	glyph := gDrive
	switch d.Kind {
	case "removable":
		glyph = gUSB
	case "network":
		glyph = gNetwork
	}
	textLine(dc, a.f.iconBig, glyph, ib, cAccent, dtCenter)

	x := ib.Right + a.scale(14)
	right := r.Right - pad
	y := r.Top + a.scale(14)
	title := driveTitle(d)
	tw := a.measure(a.f.uiBold, title)
	badge := ""
	if d.System {
		badge = "系统盘"
	}
	bw := int32(0)
	if badge != "" {
		bw = a.measure(a.f.small, badge) + a.scale(12)
	}
	tr := min(x+tw, right-bw-a.scale(6))
	textLine(dc, a.f.uiBold, title, rect{x, y, tr, y + a.scale(22)}, cText, dtLeft)
	if badge != "" {
		br := rect{tr + a.scale(6), y + a.scale(2), tr + a.scale(6) + bw, y + a.scale(20)}
		roundRect(dc, br, a.scale(5), cSoft)
		textLine(dc, a.f.small, badge, br, cText2, dtCenter)
	}
	y += a.scale(28)
	mr := rect{x, y, right, y + a.scale(7)}
	if !d.Ready {
		textLine(dc, a.f.small, "无法读取（没插好或者没有放光盘）", rect{x, y - a.scale(4), right, y + a.scale(16)}, cText3, dtLeft)
		return
	}
	roundRect(dc, mr, a.scale(4), 0xE6E9EF)
	used := float64(d.Total-d.Free) / float64(max(d.Total, 1))
	low := d.Total > 0 && float64(d.Free)/float64(d.Total) < 0.1
	mc := cAccent
	if low {
		mc = cDanger
	}
	if w := int32(float64(mr.W()) * used); w > 0 {
		roundRect(dc, rect{mr.Left, mr.Top, mr.Left + max(w, mr.H()), mr.Bottom}, a.scale(4), mc)
	}
	y += a.scale(14)
	sub := fmt.Sprintf("可用 %s，共 %s", humanSize(d.Free), humanSize(d.Total))
	c := cText2
	if low {
		sub += " · 快满了"
		c = cDanger
	}
	if d.FS != "" {
		sub += " · " + d.FS
	}
	textLine(dc, a.f.small, sub, rect{x, y, right, y + a.scale(18)}, c, dtLeft)
	a.addHot(id, r, true, "")
}

// ---------------------------------------------------------------- 树图上的高亮和提示

func (a *app) paintMap(dc uintptr) {
	r := a.rMap
	if r.W() <= 0 {
		return
	}
	if an := a.anim; an != nil {
		src := &a.mapBuf
		if an.useOld {
			src = &a.oldBuf
		}
		t := float64(time.Since(an.start)) / float64(an.dur)
		t = min(max(t, 0), 1)
		t = 1 - (1-t)*(1-t)*(1-t)
		lerp := func(p, q int32) int32 { return p + int32(float64(q-p)*t) }
		sr := rect{lerp(an.from.Left, an.to.Left), lerp(an.from.Top, an.to.Top), lerp(an.from.Right, an.to.Right), lerp(an.from.Bottom, an.to.Bottom)}
		pSetStretchBltMode.Call(dc, 4) // HALFTONE
		pStretchBlt.Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.W()), uintptr(r.H()),
			src.dc, uintptr(sr.Left), uintptr(sr.Top), uintptr(max(sr.W(), 1)), uintptr(max(sr.H(), 1)), srcCopy)
		return
	}
	if a.mapBuf.dc != 0 {
		blit(dc, r.Left, r.Top, min(r.W(), a.mapBuf.w), min(r.H(), a.mapBuf.h), a.mapBuf.dc, 0, 0)
	}
	save := saveDC(dc)
	clip(dc, r)
	defer restoreDC(dc, save)

	if v := a.viewRoot; v != nil && len(v.C) == 0 && v.R == 0 {
		msg := "这个文件夹是空的"
		switch {
		case v.X:
			msg = "没有权限查看这个文件夹"
			if !isAdmin() {
				msg += "，可以试试以管理员身份运行"
			}
		case a.scan != nil && a.scan.Busy():
			msg = "正在扫描…"
		case v.S > 0:
			msg = "窗口太小，画不下了"
		}
		textLine(dc, a.f.ui, msg, r, cText3, dtCenter)
	}

	off := func(t *tile) rect {
		return rect{t.r.Left + r.Left, t.r.Top + r.Top, t.r.Right + r.Left, t.r.Bottom + r.Top}
	}
	if sel := a.selectedTile(); sel != nil {
		sr := off(sel)
		frame(dc, sr, a.scale(2), 0x15171C)
		frame(dc, rect{sr.Left + a.scale(2), sr.Top + a.scale(2), sr.Right - a.scale(2), sr.Bottom - a.scale(2)}, 1, 0xFFFFFF)
	}
	if h := a.hover; h != nil {
		hr := off(h)
		frame(dc, hr, a.scale(2), cAccent)
		a.paintTileTip(dc, h)
	}
	if a.toast != "" {
		a.paintToast(dc, r)
	}
}

func (a *app) selectedTile() *tile {
	if a.selected == nil || a.tree == nil || len(a.selected) <= len(a.path) || !hasPrefix(a.selected, a.path) {
		return nil
	}
	return findTile(a.tree, a.selected[len(a.path):])
}

func (a *app) paintTileTip(dc uintptr, t *tile) {
	v := t.v
	var lines []string
	share := ""
	if a.viewRoot != nil && a.viewRoot.S > 0 {
		share = "占当前文件夹 " + formatPercent(float64(v.S)/float64(a.viewRoot.S))
	}
	hint := ""
	switch t.kind {
	case tileDir:
		kind := "文件夹 · " + formatCount(v.F) + " 个文件"
		if v.X {
			kind += " · 没有权限打开"
		}
		lines = append(lines, kind)
		hint = "双击进入 · 右键更多操作"
	case tileFile:
		lines = append(lines, categories[t.cat].name+"文件")
		hint = "右键可以打开、定位或删除"
	case tileRest:
		lines = append(lines, "这些项目太小，画不出来")
		hint = "双击进入所在的文件夹看列表"
	}
	path := ""
	if t.kind != tileRest {
		path = a.scan.AbsPath(append(clonePath(a.path), t.parts()...))
	}

	pad := a.scale(12)
	maxW := a.scale(380)
	nameW := min(a.measure(a.f.uiBold, v.N), maxW)
	sizeText := humanSize(v.S)
	w := max(nameW, a.measure(a.f.big, sizeText)+a.scale(10)+a.measure(a.f.small, share))
	for _, l := range lines {
		w = max(w, a.measure(a.f.small, l))
	}
	w = min(max(w, a.scale(180)), maxW)
	pathH := int32(0)
	if path != "" {
		pathH = min(textHeight(a.buf.dc, a.f.small, path, w), a.scale(54))
	}
	h := pad*2 + a.scale(20) + a.scale(32) + int32(len(lines))*a.scale(18) + a.scale(4) + pathH + a.scale(22)
	w += pad * 2

	x := a.mouse.X + a.scale(16)
	y := a.mouse.Y + a.scale(18)
	m := a.rMap
	if x+w > m.Right-a.scale(4) {
		x = a.mouse.X - w - a.scale(12)
	}
	if y+h > m.Bottom-a.scale(4) {
		y = a.mouse.Y - h - a.scale(12)
	}
	x = max(x, m.Left+a.scale(4))
	y = max(y, m.Top+a.scale(4))
	box := rect{x, y, x + w, y + h}
	g := newGP(dc)
	g.roundRect(rect{box.Left, box.Top + a.scale(2), box.Right, box.Bottom + a.scale(3)}, a.scale(12), 0x000000, 24) // 影子
	g.roundBox(box, a.scale(12), 0xFFFFFF, 0xDADFE7)
	g.close()

	cx, cy := x+pad, y+pad
	textLine(dc, a.f.uiBold, v.N, rect{cx, cy, box.Right - pad, cy + a.scale(20)}, cText, dtLeft)
	cy += a.scale(20)
	sw := a.measure(a.f.big, sizeText)
	textLine(dc, a.f.big, sizeText, rect{cx, cy, cx + sw + 2, cy + a.scale(32)}, cText, dtLeft)
	textLine(dc, a.f.small, share, rect{cx + sw + a.scale(10), cy + a.scale(6), box.Right - pad, cy + a.scale(30)}, cText2, dtLeft)
	cy += a.scale(32)
	for _, l := range lines {
		textLine(dc, a.f.small, l, rect{cx, cy, box.Right - pad, cy + a.scale(18)}, cText2, dtLeft)
		cy += a.scale(18)
	}
	cy += a.scale(4)
	if path != "" {
		text(dc, a.f.small, path, rect{cx, cy, box.Right - pad, cy + pathH}, cText3, dtWordBreak|dtEndEllipsis)
		cy += pathH
	}
	textLine(dc, a.f.small, hint, rect{cx, cy, box.Right - pad, cy + a.scale(22)}, cAccent, dtLeft)
}

func (a *app) paintToast(dc uintptr, area rect) {
	w := min(a.measure(a.f.ui, a.toast)+a.scale(56), area.W()-a.scale(40))
	h := a.scale(40)
	x := area.Left + (area.W()-w)/2
	y := area.Bottom - h - a.scale(20)
	bg := rgb(0x1F242E)
	if a.toastErr {
		bg = cDanger
	}
	g := newGP(dc)
	g.roundRect(rect{x, y + a.scale(2), x + w, y + h + a.scale(4)}, a.scale(12), 0x000000, 30)
	g.roundRect(rect{x, y, x + w, y + h}, a.scale(12), bg, 245)
	g.close()
	textLine(dc, a.f.ui, a.toast, rect{x + a.scale(18), y, x + w - a.scale(18), y + h}, 0xFFFFFF, dtCenter)
}

// ---------------------------------------------------------------- 右侧摘要

// sideNote 是右侧摘要下面的提示（比如有多少空间没统计到）
func (a *app) sideNote() (string, bool) {
	if a.scan == nil || a.viewRoot == nil || len(a.path) > 0 || a.scan.Busy() || a.diskTotal <= 0 {
		return "", false
	}
	root := a.scan.RootPath
	if len(root) > 3 { // 只在扫描整个盘时提示
		return "", false
	}
	used := a.diskTotal - a.diskFree
	gap := used - a.viewRoot.S
	if gap < 1<<30 || float64(gap) < float64(used)*0.02 {
		return "", false
	}
	msg := fmt.Sprintf("这个盘一共用了 %s，扫描到 %s。另外 %s 在没有权限读取的地方（系统还原点、其他用户的文件夹等）或者是硬链接、压缩带来的差异。",
		humanSize(used), humanSize(a.viewRoot.S), humanSize(gap))
	return msg, !isAdmin()
}

func (a *app) sideHeadHeight() int32 {
	h := a.scale(16) + a.scale(22) + a.scale(36) + a.scale(20) + a.scale(14)
	if note, link := a.sideNote(); note != "" {
		w := min(a.scale(430), a.rMain.W()*42/100) - a.scale(32) - a.scale(24)
		h += textHeight(a.measureDC(), a.f.small, note, w) + a.scale(24) + a.scale(6)
		if link {
			h += a.scale(22)
		}
		h += a.scale(10)
	}
	return h
}

func (a *app) measureDC() uintptr {
	a.measure(a.f.ui, "")
	return a.buf.dc
}

func (a *app) paintSideHead(dc uintptr) {
	r := a.rSide
	fill(dc, r, cBar)
	fill(dc, rect{r.Left, r.Top, r.Left + 1, r.Bottom}, cLine)
	h := a.rSideHead
	fill(dc, rect{h.Left, h.Bottom - 1, h.Right, h.Bottom}, cLine)
	v := a.viewRoot
	if v == nil {
		return
	}
	pad := a.scale(16)
	x := h.Left + pad
	right := h.Right - pad
	y := h.Top + a.scale(16)
	textLine(dc, a.f.icon, gFolder, rect{x, y, x + a.scale(20), y + a.scale(22)}, 0xE0A526, dtLeft)
	textLine(dc, a.f.uiBold, v.N, rect{x + a.scale(24), y, right, y + a.scale(22)}, cText, dtLeft)
	y += a.scale(22)
	textLine(dc, a.f.big, humanSize(v.S), rect{x, y, right, y + a.scale(36)}, cText, dtLeft)
	y += a.scale(36)
	meta := formatCount(v.F) + " 个文件"
	if a.diskTotal > 0 && v.S > 0 {
		meta += " · 占整个盘 " + formatPercent(float64(v.S)/float64(a.diskTotal))
	}
	if a.scan != nil && a.scan.Busy() && v.P {
		meta += " · 还在扫描"
	}
	textLine(dc, a.f.small, meta, rect{x, y, right, y + a.scale(20)}, cText2, dtLeft)
	y += a.scale(20) + a.scale(10)

	if note, link := a.sideNote(); note != "" {
		w := right - x - a.scale(24)
		th := textHeight(dc, a.f.small, note, w)
		boxH := th + a.scale(24)
		if link {
			boxH += a.scale(22)
		}
		box := rect{x, y, right, y + boxH}
		roundRect(dc, box, a.scale(10), cSoft)
		text(dc, a.f.small, note, rect{x + a.scale(12), y + a.scale(12), right - a.scale(12), y + a.scale(12) + th}, cText2, dtWordBreak)
		if link {
			lr := rect{x + a.scale(12), y + a.scale(12) + th + a.scale(2), x + a.scale(12) + a.measure(a.f.small, "以管理员身份运行，统计得更完整 ›"), y + a.scale(12) + th + a.scale(22)}
			c := cAccent
			if a.hotID == hAdminLink {
				c = c.mix(0x000000, 0.2)
			}
			textLine(dc, a.f.small, "以管理员身份运行，统计得更完整 ›", lr, c, dtLeft)
			a.addHot(hAdminLink, lr, true, "")
		}
	}
}

// ---------------------------------------------------------------- 底栏

func (a *app) paintFoot(dc uintptr) {
	r := a.rFoot
	fill(dc, r, cBar)
	fill(dc, rect{r.Left, r.Top, r.Right, r.Top + 1}, cLine)
	pad := a.scale(14)
	right := r.Right - pad

	// 右边：颜色图例
	if a.mode == modeView {
		var ws []int32
		total := int32(0)
		for _, c := range categories {
			w := a.scale(14) + a.measure(a.f.small, c.name) + a.scale(12)
			ws = append(ws, w)
			total += w
		}
		if total < r.W()/2 {
			x := right - total
			for i, c := range categories {
				sq := rect{x, r.Top + (r.H()-a.scale(10))/2, x + a.scale(10), r.Top + (r.H()-a.scale(10))/2 + a.scale(10)}
				roundRect(dc, sq, a.scale(3), catColor(i))
				textLine(dc, a.f.small, c.name, rect{sq.Right + a.scale(4), r.Top, x + ws[i], r.Bottom}, cText2, dtLeft)
				x += ws[i]
			}
			right -= total + a.scale(16)
		}
	}

	// 左边：鼠标指着的、选中的、或者当前文件夹
	info := ""
	if a.mode == modeView && a.scan != nil {
		switch {
		case a.hover != nil && a.hover.kind != tileRest:
			info = a.scan.AbsPath(append(clonePath(a.path), a.hover.parts()...)) + "  ·  " + humanSize(a.hover.v.S)
		case a.selected != nil:
			info = a.scan.AbsPath(a.selected)
		default:
			info = a.scan.AbsPath(a.path)
		}
	} else if a.mode == modeHome {
		info = "文件清理助手 " + version
		if isAdmin() {
			info += " · 管理员身份"
		}
	}
	text(dc, a.f.small, info, rect{r.Left + pad, r.Top, right, r.Bottom}, cText2, dtSingleLine|dtVCenter|dtPathEllipsis)
}

// paintTips 画图标按钮的小提示
func (a *app) paintTips(dc uintptr) {
	if a.pressID >= 0 {
		return
	}
	for _, h := range a.hot {
		if h.id != a.hotID || h.tip == "" {
			continue
		}
		w := a.measure(a.f.small, h.tip) + a.scale(20)
		cr := clientRect(a.hwnd)
		x := min(max(h.r.Left+(h.r.W()-w)/2, a.scale(6)), cr.Right-w-a.scale(6))
		y := h.r.Bottom + a.scale(6)
		box := rect{x, y, x + w, y + a.scale(26)}
		roundRect(dc, box, a.scale(6), 0x2A2F3A)
		textLine(dc, a.f.small, h.tip, box, 0xFFFFFF, dtCenter)
	}
}
