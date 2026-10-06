//go:build darwin

package main

// Mac 版的界面逻辑：起始页、扫描、进出文件夹、选中、右键菜单、移到废纸篓、永久删除。
// 和 Windows 版的 actions.go 是同一套流程，窗口和控件由 cocoa_darwin.go / *_darwin.m 负责。
// 这里的方法都在主线程上调用（Objective-C 回调进来，或者 uiPost 转回来）。

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// 命令编号，和 cocoa_darwin.h 里的 DL_CMD_* 一一对应
const (
	mcHome         = 1
	mcBack         = 3
	mcForward      = 4
	mcUp           = 5
	mcStopOrRescan = 6
	mcRescanAll    = 7
	mcStop         = 8
	mcSide         = 9
	mcPick         = 10
	mcDetail0      = 11
	mcOpen         = 20
	mcReveal       = 21
	mcCopyPath     = 22
	mcTrash        = 23
	mcDelete       = 24
	mcRescanItem   = 25
	mcDeselect     = 27
	mcFDA          = 30
	mcHelp         = 31
	mcEnter        = 40 // 只在右键菜单里用
	mcOpenItem     = 41
)

// 树图鼠标事件，和 DL_MOUSE_* 对应
const (
	mouseMove = iota
	mouseDown
	mouseRight
	mouseExit
	mouseBack
	mouseForward
)

// 「完全磁盘访问权限」那一页系统设置
const fdaSettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"

type toolbarState struct {
	home                                    string // 「换个磁盘」「回到扫描结果」，空字符串表示禁用
	back, fwd, up, busy, rescan, side, view bool
	detail                                  int
}

type volInfo struct {
	path, name, format               string
	total, avail, important          int64
	root, removable, internal, local bool
}

type volCard struct {
	path, title, badge, sub string
	used                    float64
	low                     bool
}

type mapTip struct {
	name, size, share string
	lines             []string
	path, hint        string
}

type sideInfo struct{ name, size, meta, note, link, icon string }

type alertSpec struct {
	style            int // 0 普通、1 警告、2 错误
	title, msg       string
	buttons          []string
	def, destructive int
	check            string
	needCheck        int
	details          string
}

type menuItem struct {
	tag     int
	title   string
	key     string // 快捷键：一个字母，或者 up、down、del
	mods    int    // 4 ⌘、8 ⌥、16 ⇧
	enabled bool
	sep     bool
}

type listRowData struct {
	name, size, files, path string
	share                   float64
	flags                   int
}

type macApp struct {
	mode      int
	scan      *Scan
	diskTotal int64
	diskFree  int64
	volRoot   bool       // 扫描的是一整个磁盘
	path      []string   // 当前查看的文件夹（相对扫描起点）
	back, fwd [][]string // 后退、前进
	selected  []string   // 选中的项，nil 表示没选
	menuParts []string   // 右键菜单是对哪一项弹出的
	hover     *tile
	mouse     point // 鼠标在树图上的位置（像素）
	tree      *tile
	flat      []*tile
	scale     float64
	viewRoot  *VNode
	list      []*VNode
	lastState string
	detail    int
	sideOpen  bool
	ticks     int
	timerOn   bool
	del       *deleteJob
	delName   string
	vols      []volInfo
	volsBusy  bool
	fda       bool // 有「完全磁盘访问权限」
	initial   string
	crumbs    string // 路径栏上现在显示的是什么，没变就不重新设置
}

var mac = &macApp{sideOpen: true, detail: 1, mouse: point{-1, -1}}

func runApp(initial string) {
	mac.initial = initial
	uiRun(shotFile != "")
}

func showError(msg string) { fmt.Fprintln(os.Stderr, msg) }

// onLaunched 在窗口建好之后调用；path 是从访达「打开方式」或拖到程序图标上传进来的
func (a *macApp) onLaunched(path string) {
	a.fda = hasFullDiskAccess()
	var names []string
	var colors []uint32
	for i, c := range categories {
		names = append(names, c.name)
		colors = append(colors, uint32(catColor(i)))
	}
	uiSetLegend(names, colors)
	if path == "" {
		path = a.initial
	}
	if path != "" {
		a.openPath(path)
	}
	if a.scan == nil {
		a.showHome()
	}
	a.scheduleShot()
	if shotFile == "" && translocated() {
		uiPost(func() {
			uiAlert(alertSpec{
				title: "建议把文件清理助手放进「应用程序」文件夹",
				msg: "现在它是从下载的压缩包里直接打开的，macOS 会把它放在一个临时的位置运行。\n\n" +
					"退出后，在访达里把「文件清理助手」拖到左边的「应用程序」里，再从「应用程序」打开。" +
					"这样以后找得到它，给它「完全磁盘访问权限」也更方便。",
				buttons: []string{"知道了"}, destructive: -1, needCheck: -1,
			})
		})
	}
}

// translocated 表示程序是被 macOS 的「App Translocation」放在临时位置运行的
// （从「下载」里直接打开、没有拖进「应用程序」的、来自网上的程序）
func translocated() bool {
	exe, err := os.Executable()
	return err == nil && strings.Contains(exe, "/AppTranslocation/")
}

// ---------------------------------------------------------------- 起始页和扫描

func (a *macApp) showHome() {
	a.mode = modeHome
	a.hover = nil
	uiSetMode(modeHome)
	uiSetHomeTip(!a.fda)
	a.loadVolumes()
	a.updateChrome()
}

func (a *macApp) loadVolumes() {
	if a.volsBusy {
		return
	}
	a.volsBusy = true
	if len(a.vols) == 0 {
		uiSetCards(nil, true)
	}
	go func() {
		v := uiListVolumes() // 网络位置可能要等一会儿，放到后台
		uiPost(func() {
			a.vols = v
			a.volsBusy = false
			a.showCards()
		})
	}()
}

func (a *macApp) showCards() {
	var cards []volCard
	for _, v := range a.vols {
		c := volCard{path: v.path, title: v.name}
		switch {
		case v.root:
			c.badge = "启动磁盘"
		case !v.local:
			c.badge = "网络"
		case v.removable || !v.internal:
			c.badge = "外置"
		}
		// 「可用」和访达一致：包括系统随时可以清掉的「可清除」空间
		free := v.important
		if free <= 0 {
			free = v.avail
		}
		if v.total > 0 {
			c.used = float64(v.total-free) / float64(v.total)
			c.low = float64(free)/float64(v.total) < 0.1
		}
		c.sub = fmt.Sprintf("可用 %s，共 %s", humanSize(free), humanSize(v.total))
		if c.low {
			c.sub += " · 快满了"
		}
		if v.format != "" {
			c.sub += " · " + v.format
		}
		cards = append(cards, c)
	}
	uiSetCards(cards, a.volsBusy)
}

func (a *macApp) cardClicked(i int) {
	if i >= 0 && i < len(a.vols) {
		a.startScan(a.vols[i].path)
	}
}

// openPath 扫描传进来的路径：输入框、选择文件夹、拖进窗口、访达「打开方式」都走这里。文件就扫它所在的文件夹
func (a *macApp) openPath(p string) {
	p = strings.Trim(strings.TrimSpace(p), `"'`)
	if p == "" {
		uiToast("先输入一个文件夹路径", true)
		return
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		p = filepath.Dir(p)
	}
	a.startScan(p)
}

func (a *macApp) startScan(path string) {
	if a.del != nil {
		uiToast("正在删除文件，删完再扫描吧", true)
		return
	}
	abs, err := filepath.Abs(path)
	var s *Scan
	if err == nil {
		s, err = NewScan(abs)
	}
	if err != nil {
		uiAlert(alertSpec{style: 2, title: "没法扫描这个位置", msg: abs + "\n\n" + friendlyError(err), buttons: []string{"好"}, destructive: -1, needCheck: -1})
		return
	}
	if a.scan != nil {
		a.scan.Stop()
	}
	a.scan = s
	a.diskTotal, a.diskFree, _ = diskSpace(abs)
	a.volRoot = isVolumeRoot(abs)
	a.path, a.back, a.fwd, a.selected, a.hover = nil, nil, nil, nil, nil
	a.lastState = ""
	a.mode = modeView
	uiSetMode(modeView)
	a.refresh(true)
	a.startActivity()
}

// rescanAll 重新扫描整个位置，尽量停在当前文件夹
func (a *macApp) rescanAll() {
	if a.scan == nil || a.del != nil {
		return
	}
	path, back := a.path, a.back
	a.startScan(a.scan.RootPath)
	a.path, a.back = path, back
	a.refresh(true)
}

// ---------------------------------------------------------------- 刷新画面

// refresh 从扫描结果重新取当前文件夹的数据，重新排版、画树图、更新列表。
// newFolder 为 true 表示换了文件夹，列表滚回顶部。
func (a *macApp) refresh(newFolder bool) {
	if a.scan == nil || a.mode != modeView {
		return
	}
	w, h, s := uiMapSize()
	if w <= 0 || h <= 0 {
		return
	}
	a.scale = s
	area := detailArea[a.detail] * s * s
	root, at, list := a.scan.View(a.path, ViewOpts{W: float64(w), H: float64(h), MinArea: area, Max: detailMax[a.detail]}, 1<<30)
	if !samePath(at, a.path) {
		a.path = at
		newFolder = true
	}
	a.viewRoot = root
	a.tree = layoutTree(root, rect{0, 0, w, h}, mapMetrics{header: a.px(19), pad: a.px(3)})
	a.flat = a.flat[:0]
	var walk func(t *tile)
	walk = func(t *tile) {
		for _, k := range t.kids {
			a.flat = append(a.flat, k)
			walk(k)
		}
	}
	walk(a.tree)
	scanning := a.scan.Busy()
	uiMapSetTiles(a.flat, scanning, s)
	a.list = list
	uiListReload(len(list), newFolder)
	a.syncListSelection(newFolder)
	a.hover = a.hitMap(a.mouse.X, a.mouse.Y)
	a.updateMarks()
	a.updateMessage()
	a.updateChrome()
}

func (a *macApp) px(v float64) int32 { return int32(math.Round(v * a.scale)) }

// syncListSelection 列表按行号选中；扫描中顺序会变，所以每次按名字重新选一遍
func (a *macApp) syncListSelection(scroll bool) {
	row := -1
	if len(a.selected) > len(a.path) && hasPrefix(a.selected, a.path) {
		name := a.selected[len(a.path)]
		for i, v := range a.list {
			if v.N == name {
				row = i
				break
			}
		}
	}
	uiListSelect(row, scroll)
}

func (a *macApp) mapResized() {
	if a.mode == modeView {
		a.refresh(false)
	}
}

// labels 是方块上写的两行字
func (t *tile) labels(scanning bool) (name, size string) {
	name, size = t.v.N, humanSize(t.v.S)
	if t.kind == tileDir {
		if t.v.X {
			name += "（无权限）"
		}
		if scanning && t.v.P {
			size += " · 扫描中"
		}
	}
	return name, size
}

func (t *tile) drawFlags(scanning bool) int {
	f := 0
	if t.header {
		f |= 1
	}
	if len(t.kids) > 0 {
		f |= 2
	}
	if scanning && t.v.P {
		f |= 4
	}
	if t.depth%2 == 0 {
		f |= 8
	}
	return f
}

func (a *macApp) updateMarks() {
	var sel, hov *rect
	if t := a.selectedTile(); t != nil {
		r := t.r
		sel = &r
	}
	if a.hover != nil {
		r := a.hover.r
		hov = &r
	}
	uiMapSetMarks(sel, hov)
	if a.hover == nil {
		uiMapSetTip(nil)
	} else {
		uiMapSetTip(a.tipFor(a.hover))
	}
	a.updateFooter()
}

func (a *macApp) tipFor(t *tile) *mapTip {
	var viewSize int64
	if a.viewRoot != nil {
		viewSize = a.viewRoot.S
	}
	tt := t.tip(viewSize)
	tip := &mapTip{name: t.v.N, size: humanSize(t.v.S), share: tt.share, lines: tt.lines, hint: tt.hint}
	if t.kind != tileRest {
		tip.path = a.scan.AbsPath(append(clonePath(a.path), t.parts()...))
	}
	return tip
}

func (a *macApp) updateMessage() {
	msg := ""
	if v := a.viewRoot; v != nil && len(v.C) == 0 && v.R == 0 {
		msg = "这个文件夹是空的"
		switch {
		case v.X:
			msg = "没有权限查看这个文件夹"
			if !a.fda {
				msg += "。如果它受 macOS 的隐私保护，可以给文件清理助手「完全磁盘访问权限」"
			}
		case a.scan != nil && a.scan.Busy():
			msg = "正在扫描…"
		case v.S > 0:
			msg = "窗口太小，画不下了"
		}
	}
	uiMapSetMessage(msg)
}

func (a *macApp) updateFooter() {
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
	}
	uiSetFooter(info)
}

// updateChrome 更新标题栏、工具栏、路径栏和右侧摘要
func (a *macApp) updateChrome() {
	busy := (a.scan != nil && a.scan.Busy()) || a.del != nil
	tb := toolbarState{
		back: a.mode == modeView && len(a.back) > 0, fwd: a.mode == modeView && len(a.fwd) > 0, up: a.mode == modeView && len(a.path) > 0,
		busy: busy, rescan: a.scan != nil && a.mode == modeView, detail: a.detail, side: a.sideOpen, view: a.mode == modeView,
	}
	switch {
	case a.mode == modeView:
		tb.home = "换个磁盘"
	case a.scan != nil:
		tb.home = "回到扫描结果"
	}
	uiSetToolbar(tb)

	if a.mode != modeView || a.scan == nil {
		uiSetTitle(appName, "")
		return
	}
	names := append([]string{a.scan.RootName()}, a.path...)
	if key := a.scan.RootPath + "\x00" + strings.Join(names, "\x00"); key != a.crumbs {
		a.crumbs = key
		paths := make([]string, len(names))
		for i := range names {
			paths[i] = a.scan.AbsPath(a.path[:i])
		}
		uiSetCrumbs(names, paths)
	}

	var parts []string
	if a.del != nil {
		parts = a.del.statusParts(a.delName)
	} else {
		parts = a.scan.Status().parts()
	}
	uiSetTitle(names[len(names)-1], strings.Join(parts, " · "))
	a.updateSide()
	a.updateFooter()
}

func (a *macApp) updateSide() {
	v := a.viewRoot
	if v == nil {
		return
	}
	meta := formatCount(v.F) + " 个文件"
	if a.diskTotal > 0 && v.S > 0 {
		meta += " · 占整个磁盘 " + formatPercent(float64(v.S)/float64(a.diskTotal))
	}
	if a.scan.Busy() && v.P {
		meta += " · 还在扫描"
	}
	note, link := a.sideNote()
	s := sideInfo{name: v.N, size: humanSize(v.S), meta: meta, note: note, icon: a.scan.AbsPath(a.path)}
	if link {
		s.link = "打开「完全磁盘访问权限」设置 ›"
	}
	uiSetSide(s)
}

// sideNote 是右侧摘要下面的提示：有多少空间没统计到、有多少文件夹被隐私保护挡住了
func (a *macApp) sideNote() (note string, link bool) {
	if a.scan == nil || a.viewRoot == nil || len(a.path) > 0 || a.scan.Busy() {
		return "", false
	}
	st := a.scan.Status()
	if a.volRoot && a.diskTotal > 0 {
		used := a.diskTotal - a.diskFree
		gap := used - a.viewRoot.S
		if gap >= 1<<30 && float64(gap) >= float64(used)*0.02 {
			note = fmt.Sprintf("这个磁盘一共用了 %s，扫描到 %s。另外 %s 在扫描不到的地方：APFS 快照（比如时间机器的本地快照）、"+
				"系统可以随时清除的空间、恢复系统等隐藏的宗卷，以及没有权限读取的文件夹。",
				humanSize(used), humanSize(a.viewRoot.S), humanSize(gap))
		} else if over := -gap; over >= 1<<30 && float64(over) >= float64(used)*0.02 {
			// APFS 克隆：每个文件报的都是完整大小，共享的那部分数据被数了好几遍
			note = fmt.Sprintf("扫描到 %s，比这个磁盘实际用掉的 %s 还多 %s。APFS 上用「复制」得到的文件（克隆）和原件共用同一份数据，"+
				"但每一份都按完整大小统计，所以加起来会多算；删掉其中一份腾不出多少空间。",
				humanSize(a.viewRoot.S), humanSize(used), humanSize(over))
		}
	}
	if st.Privacy > 0 && !a.fda {
		if note != "" {
			note += "\n\n"
		}
		note += fmt.Sprintf("有 %s 个文件夹受 macOS 的隐私保护，没能读取（比如邮件、信息、废纸篓和其他应用的数据）。", formatCount(st.Privacy))
		link = true
	}
	return note, link
}

// ---------------------------------------------------------------- 定时器

func (a *macApp) startActivity() {
	if !a.timerOn {
		a.timerOn = true
		uiStartTimer()
	}
}

func (a *macApp) onTick() {
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
	a.updateChrome()
	if !active {
		a.timerOn = false
		uiStopTimer()
	}
}

// ---------------------------------------------------------------- 导航

// navigate 进入 parts 这个文件夹。zoom 不为 nil 时播放「钻进去」的动画（zoom 是它在树图上的位置）
func (a *macApp) navigate(parts []string, push bool, zoom *rect) {
	if a.scan == nil || samePath(parts, a.path) {
		return
	}
	old := a.path
	if push {
		a.back = append(a.back, old)
		a.fwd = nil
	}
	full := rect{0, 0, 1, 1}
	if a.tree != nil {
		full = a.tree.r
	}
	if zoom != nil {
		uiMapZoom(full, *zoom, true) // 钻进去：旧图里这个文件夹的区域放大到整个画面
	}
	a.path = clonePath(parts)
	a.hover = nil
	a.refresh(true)
	if zoom == nil && len(parts) < len(old) && hasPrefix(old, parts) {
		// 退出来：新图里刚才那个文件夹的区域缩回去，并且选中它
		a.selected = clonePath(old)
		a.syncListSelection(true)
		a.updateMarks()
		if t := findTile(a.tree, old[len(parts):]); t != nil && t.r.W() > 4 && t.r.H() > 4 {
			uiMapZoom(t.r, a.tree.r, false)
		}
	}
	a.updateChrome()
}

func (a *macApp) goUp() {
	if a.mode == modeView && len(a.path) > 0 {
		a.navigate(a.path[:len(a.path)-1], true, nil)
	}
}

func (a *macApp) goBack() {
	if a.mode != modeView || len(a.back) == 0 {
		return
	}
	p := a.back[len(a.back)-1]
	a.back = a.back[:len(a.back)-1]
	a.fwd = append(a.fwd, a.path)
	a.navigate(p, false, nil)
}

func (a *macApp) goForward() {
	if a.mode != modeView || len(a.fwd) == 0 {
		return
	}
	p := a.fwd[len(a.fwd)-1]
	a.fwd = a.fwd[:len(a.fwd)-1]
	a.back = append(a.back, a.path)
	a.navigate(p, false, nil)
}

func (a *macApp) crumbClicked(i int) {
	if a.mode == modeView && i >= 0 && i <= len(a.path) {
		a.navigate(a.path[:i], true, nil)
	}
}

// ---------------------------------------------------------------- 命令

func (a *macApp) command(cmd int) {
	switch {
	case cmd == mcHome:
		if a.mode == modeView {
			a.showHome()
		} else if a.scan != nil {
			a.mode = modeView
			uiSetMode(modeView)
			a.refresh(true)
			if a.scan.Busy() {
				a.startActivity()
			}
		}
	case cmd == mcBack:
		a.goBack()
	case cmd == mcForward:
		a.goForward()
	case cmd == mcUp:
		a.goUp()
	case cmd == mcStopOrRescan:
		if (a.scan != nil && a.scan.Busy()) || a.del != nil {
			a.stop()
		} else {
			a.rescanAll()
		}
	case cmd == mcStop:
		a.stop()
	case cmd == mcRescanAll:
		a.rescanAll()
	case cmd == mcSide:
		a.sideOpen = !a.sideOpen
		a.updateChrome() // 右侧列表收起或展开后树图的尺寸变了，会自己调 refresh 重新排
		a.refresh(false)
	case cmd == mcPick:
		if a.del == nil {
			uiPickFolder()
		}
	case cmd >= mcDetail0 && cmd < mcDetail0+3:
		a.detail = cmd - mcDetail0
		a.refresh(false)
	case cmd == mcOpen:
		if a.selected != nil {
			a.openParts(a.selected)
		}
	case cmd == mcReveal:
		if a.selected != nil {
			uiReveal(a.scan.AbsPath(a.selected))
		}
	case cmd == mcCopyPath:
		if a.selected != nil {
			a.copyPath(a.selected)
		}
	case cmd == mcTrash:
		if a.selected != nil {
			a.trash(a.selected)
		}
	case cmd == mcDelete:
		if a.selected != nil {
			a.deletePermanently(a.selected)
		}
	case cmd == mcRescanItem:
		if a.selected != nil {
			a.rescanItem(a.selected)
		}
	case cmd == mcDeselect:
		a.selected = nil
		uiListSelect(-1, false)
		a.updateMarks()
	case cmd == mcFDA:
		a.explainFDA()
	case cmd == mcHelp:
		uiOpenURL(repoURL + "#readme")
	}
}

// validate 告诉菜单栏一个命令现在能不能用（第 1 位）、要不要打勾（第 2 位）
func (a *macApp) validate(cmd int) int {
	view := a.mode == modeView && a.scan != nil
	busy := (a.scan != nil && a.scan.Busy()) || a.del != nil
	on := func(enabled, checked bool) int {
		r := 0
		if enabled {
			r |= 1
		}
		if checked {
			r |= 2
		}
		return r
	}
	switch {
	case cmd == mcHome:
		return on(view || a.scan != nil, false)
	case cmd == mcBack:
		return on(view && len(a.back) > 0, false)
	case cmd == mcForward:
		return on(view && len(a.fwd) > 0, false)
	case cmd == mcUp:
		return on(view && len(a.path) > 0, false)
	case cmd == mcStop:
		return on(busy, false)
	case cmd == mcRescanAll:
		return on(view && !busy, false)
	case cmd == mcStopOrRescan:
		return on(view, false)
	case cmd == mcSide:
		return on(view, a.sideOpen)
	case cmd >= mcDetail0 && cmd < mcDetail0+3:
		return on(view, a.detail == cmd-mcDetail0)
	case cmd == mcPick:
		return on(a.del == nil, false)
	case cmd == mcOpen || cmd == mcReveal || cmd == mcCopyPath:
		return on(view && a.selected != nil, false)
	case cmd == mcTrash || cmd == mcDelete:
		return on(view && a.selected != nil && !busy, false)
	case cmd == mcRescanItem:
		return on(view && a.selected != nil && !busy && a.isDir(a.selected), false)
	}
	return 1
}

func (a *macApp) stop() {
	if a.del != nil {
		a.del.stop.Store(true)
	} else if a.scan != nil {
		a.scan.Stop()
	}
}

func (a *macApp) explainFDA() {
	msg := "这样文件清理助手就能读到邮件、信息、废纸篓和其他应用的数据，统计得更完整。\n\n" +
		"1. 点「打开系统设置」，会打开「隐私与安全性 → 完全磁盘访问权限」。\n" +
		"2. 在列表里找到「文件清理助手」，打开它右边的开关（需要输入开机密码）。" +
		"列表里没有的话，点下面的「＋」，在「应用程序」里选中「文件清理助手」。\n" +
		"3. 系统提示「退出并重新打开」时点它（或者自己退出后重新打开文件清理助手）。"
	r, _ := uiAlert(alertSpec{title: "给文件清理助手「完全磁盘访问权限」", msg: msg, buttons: []string{"打开系统设置", "取消"}, destructive: -1, needCheck: -1})
	if r == 0 {
		uiOpenURL(fdaSettingsURL)
	}
}

func (a *macApp) shouldClose() bool {
	if a.del != nil {
		r, _ := uiAlert(alertSpec{style: 1, title: "正在删除文件，确定要退出吗？", msg: "退出后删除会中断，已经删掉的不会恢复，剩下的保留不动。",
			buttons: []string{"退出", "取消"}, def: 1, destructive: 0, needCheck: -1})
		if r != 0 {
			return false
		}
		a.del.stop.Store(true)
	}
	if a.scan != nil {
		a.scan.Stop()
	}
	return true
}

// ---------------------------------------------------------------- 树图上的鼠标

func (a *macApp) hitMap(x, y int32) *tile {
	if a.tree == nil {
		return nil
	}
	t := hitTest(a.tree, x, y)
	if t == a.tree {
		return nil
	}
	return t
}

func (a *macApp) mapMouse(kind int, x, y int32, clicks int) {
	if a.mode != modeView {
		return
	}
	switch kind {
	case mouseMove:
		a.mouse = point{x, y}
		if t := a.hitMap(x, y); t != a.hover {
			a.hover = t
			a.updateMarks()
		}
	case mouseExit:
		a.mouse = point{-1, -1}
		if a.hover != nil {
			a.hover = nil
			a.updateMarks()
		}
	case mouseDown:
		t := a.hitMap(x, y)
		if clicks >= 2 {
			a.openTile(t)
		} else {
			a.selectTile(t)
		}
	case mouseRight:
		t := a.hitMap(x, y)
		a.selectTile(t)
		if t != nil {
			a.tileMenu(t)
		}
	case mouseBack:
		a.goBack()
	case mouseForward:
		a.goForward()
	}
}

// animDone：动画放完了，按鼠标现在的位置重新找悬停的方块
func (a *macApp) animDone() {
	a.hover = a.hitMap(a.mouse.X, a.mouse.Y)
	a.updateMarks()
}

func (a *macApp) selectedTile() *tile {
	if a.selected == nil || a.tree == nil || len(a.selected) <= len(a.path) || !hasPrefix(a.selected, a.path) {
		return nil
	}
	return findTile(a.tree, a.selected[len(a.path):])
}

func (a *macApp) fullParts(t *tile) []string {
	if t == nil {
		return nil
	}
	if t.kind == tileRest {
		return append(clonePath(a.path), t.parent.parts()...)
	}
	return append(clonePath(a.path), t.parts()...)
}

func (a *macApp) selectTile(t *tile) {
	if t == nil || t.kind == tileRest {
		a.selected = nil
	} else {
		a.selected = a.fullParts(t)
	}
	// 列表里只有当前文件夹的直接子项：选中的是更深的，就选中它所在的那一项
	a.syncListSelection(true)
	a.updateMarks()
}

// openTile 双击方块：文件夹就钻进去，文件和「小项目」进入它所在的文件夹
func (a *macApp) openTile(t *tile) {
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

// openParts 打开一项：文件夹进入，文件交给默认的应用
func (a *macApp) openParts(parts []string) {
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
	a.openItem(parts)
}

func (a *macApp) openItem(parts []string) {
	if !uiOpenFile(a.scan.AbsPath(parts)) {
		uiToast("打不开这个文件", true)
	}
}

func (a *macApp) isDir(parts []string) bool {
	if a.scan == nil {
		return false
	}
	_, _, _, dir, _ := a.scan.Info(parts)
	return dir
}

func (a *macApp) copyPath(parts []string) {
	uiCopyText(a.scan.AbsPath(parts))
	a.toast("已拷贝路径", false)
}

func (a *macApp) toast(msg string, isErr bool) {
	logf("TOAST %s", msg)
	uiToast(msg, isErr)
}

func (a *macApp) rescanItem(parts []string) {
	if err := a.scan.Rescan(parts); err != nil {
		a.toast(err.Error(), true)
		return
	}
	a.lastState = "scanning"
	a.startActivity()
	a.refresh(false)
}

// ---------------------------------------------------------------- 右侧列表

func (a *macApp) listRow(i int) listRowData {
	if i < 0 || i >= len(a.list) {
		return listRowData{}
	}
	v := a.list[i]
	r := listRowData{name: v.N, size: humanSize(v.S), path: a.scan.AbsPath(append(clonePath(a.path), v.N))}
	if a.viewRoot != nil && a.viewRoot.S > 0 {
		r.share = float64(v.S) / float64(a.viewRoot.S)
	}
	scanning := a.scan.Busy()
	if v.D {
		r.flags |= 1
		r.files = formatCount(v.F)
	}
	if v.X {
		r.flags |= 2
	}
	if scanning && v.P {
		r.flags |= 4
		r.size = "… " + r.size
	}
	return r
}

func (a *macApp) listSelected(row int) {
	if row < 0 || row >= len(a.list) {
		return
	}
	a.selected = append(clonePath(a.path), a.list[row].N)
	a.updateMarks()
}

// listOpen 双击列表：文件夹进去，文件在访达中显示（和 Windows 版一样，不直接打开）
func (a *macApp) listOpen(row int) {
	if row < 0 || row >= len(a.list) {
		return
	}
	parts := append(clonePath(a.path), a.list[row].N)
	if a.list[row].D {
		a.openParts(parts)
	} else {
		uiReveal(a.scan.AbsPath(parts))
	}
}

func (a *macApp) listMenu(row int) {
	if row < 0 || row >= len(a.list) {
		return
	}
	parts := append(clonePath(a.path), a.list[row].N)
	a.selected = parts
	a.updateMarks()
	a.itemMenu(parts)
}

// ---------------------------------------------------------------- 右键菜单

func (a *macApp) tileMenu(t *tile) {
	if t.kind == tileRest {
		if t.parent != a.tree {
			a.menuParts = a.fullParts(t)
			uiPopupMenu([]menuItem{{tag: mcEnter, title: "进入所在的文件夹", enabled: true}})
		}
		return
	}
	a.itemMenu(a.fullParts(t))
}

func (a *macApp) itemMenu(parts []string) {
	_, _, _, dir, ok := a.scan.Info(parts)
	if !ok {
		return
	}
	busy := a.scan.Busy() || a.del != nil
	const cmd, opt = 4, 8
	var items []menuItem
	if dir {
		items = append(items, menuItem{tag: mcEnter, title: "进入", key: "down", mods: cmd, enabled: true},
			menuItem{tag: mcOpenItem, title: "在访达中打开", enabled: true})
	} else {
		items = append(items, menuItem{tag: mcOpenItem, title: "打开", key: "down", mods: cmd, enabled: true})
	}
	items = append(items,
		menuItem{tag: mcReveal, title: "在访达中显示", key: "r", mods: cmd | 16, enabled: true},
		menuItem{tag: mcCopyPath, title: "拷贝路径", key: "c", mods: cmd | opt, enabled: true})
	if dir {
		items = append(items, menuItem{sep: true}, menuItem{tag: mcRescanItem, title: "重新扫描这个文件夹", enabled: !busy})
	}
	suffix := ""
	if busy {
		suffix = "（等扫描完成）"
	}
	items = append(items, menuItem{sep: true},
		menuItem{tag: mcTrash, title: "移到废纸篓" + suffix, key: "del", mods: cmd, enabled: !busy},
		menuItem{tag: mcDelete, title: "永久删除…" + suffix, key: "del", mods: cmd | opt, enabled: !busy})
	a.menuParts = parts
	uiPopupMenu(items)
}

// menuPicked：右键菜单选了一项
func (a *macApp) menuPicked(tag int) {
	parts := a.menuParts
	if a.scan == nil || parts == nil {
		return
	}
	switch tag {
	case mcEnter:
		a.openParts(parts)
	case mcOpenItem:
		a.openItem(parts)
	case mcReveal:
		uiReveal(a.scan.AbsPath(parts))
	case mcCopyPath:
		a.copyPath(parts)
	case mcRescanItem:
		a.rescanItem(parts)
	case mcTrash:
		a.trash(parts)
	case mcDelete:
		a.deletePermanently(parts)
	}
}

// ---------------------------------------------------------------- 删除

// describe 给确认框用：「大小：…（N 个文件）\n位置：…」
func (a *macApp) describe(parts []string) (name string, size int64, body string, ok bool) {
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

func (a *macApp) canModify() bool {
	if a.scan == nil {
		return false
	}
	if a.del != nil {
		a.toast("正在删除别的东西，等它删完", true)
		return false
	}
	if a.scan.Busy() {
		a.toast("还在扫描，等扫描完成再删除", true)
		return false
	}
	return true
}

func (a *macApp) trash(parts []string) {
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
		uiAlert(alertSpec{style: 2, title: "不能删除「" + name + "」", msg: block, buttons: []string{"好"}, destructive: -1, needCheck: -1})
		return
	}
	spec := alertSpec{
		title: "把「" + name + "」移到废纸篓？",
		msg: body + "\n\n删错了可以从废纸篓里放回原处。\n注意：废纸篓里的东西仍然占着磁盘空间，清空废纸篓后才会真正腾出来。" +
			"想马上腾出空间，请用「永久删除」。",
		buttons: []string{"移到废纸篓", "取消"}, destructive: -1, needCheck: -1,
	}
	if warn != "" {
		spec.style = 1
		spec.msg += "\n\n" + warn
		spec.def = 1
	}
	if r, _ := uiAlert(spec); r != 0 {
		return
	}
	errText := uiTrash(abs)
	if a.afterDelete(parts, abs) {
		a.toast("已移到废纸篓：「"+name+"」 "+humanSize(size)+"，清空废纸篓后腾出空间", false)
	} else if errText != "" {
		uiAlert(alertSpec{style: 2, title: "没能移到废纸篓", msg: abs + "\n\n" + errText, buttons: []string{"好"}, destructive: -1, needCheck: -1})
	} else {
		a.toast("只移走了一部分，有些文件可能正被使用", true)
	}
}

func (a *macApp) deletePermanently(parts []string) {
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
		uiAlert(alertSpec{style: 2, title: "不能删除「" + name + "」", msg: block, buttons: []string{"好"}, destructive: -1, needCheck: -1})
		return
	}
	msg := body + "\n\n永久删除不进废纸篓，删了就找不回来了。"
	if warn != "" {
		msg += "\n\n" + warn
	}
	r, checked := uiAlert(alertSpec{
		style: 1, title: "永久删除「" + name + "」？", msg: msg,
		buttons: []string{"永久删除", "取消"}, def: 1, destructive: 0,
		check: "我确定要永久删除，不需要恢复", needCheck: 0,
	})
	if r != 0 || !checked {
		return
	}
	a.delName = name
	a.del = startDelete(abs, clonePath(parts), func() { uiPost(a.onDeleteDone) })
	a.startActivity()
	a.updateChrome()
}

func (a *macApp) onDeleteDone() {
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
		a.toast(fmt.Sprintf("已永久删除「%s」，腾出了 %s", a.delName, freed), false)
	case stopped:
		a.toast(fmt.Sprintf("已停止删除。删掉了 %s 个文件，腾出了 %s", formatCount(j.files.Load()), freed), false)
	default:
		j.mu.Lock()
		details := strings.Join(j.errs, "\n")
		j.mu.Unlock()
		if n := j.failed.Load(); n > int64(len(j.errs)) {
			details += fmt.Sprintf("\n……一共 %s 项", formatCount(n))
		}
		hint := "正被使用的文件，退出对应的应用后再删一次就行。属于系统或者其他用户的文件，可以在访达里删除（会要求输入密码）。"
		if !a.fda {
			hint += "受隐私保护的，给文件清理助手「完全磁盘访问权限」后再删。"
		}
		uiAlert(alertSpec{
			style: 1, title: "有些东西没删掉",
			msg:     fmt.Sprintf("删掉了 %s 个文件，腾出了 %s；有 %s 项删不掉。\n\n%s", formatCount(j.files.Load()), freed, formatCount(j.failed.Load()), hint),
			buttons: []string{"知道了"}, destructive: -1, needCheck: -1, details: details,
		})
	}
	a.updateChrome()
}

// afterDelete 删除（或者删了一半）之后同步扫描结果。返回 true 表示已经完全删掉了
func (a *macApp) afterDelete(parts []string, abs string) bool {
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
