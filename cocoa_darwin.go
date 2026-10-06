//go:build darwin && cgo

package main

// Mac 版界面的 Go 一侧：把 Go 的数据交给 Objective-C（*_darwin.m），以及接住那边的回调。
// 接口说明见 cocoa_darwin.h。

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=12.0 -Wno-deprecated-declarations
#cgo LDFLAGS: -mmacosx-version-min=12.0 -framework Cocoa -framework QuartzCore -framework UniformTypeIdentifiers
#include "cocoa_darwin.h"
*/
import "C"

import (
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

// cstr 把 Go 字符串转成 C 字符串，调用方负责 free
func cstr(s string) *C.char { return C.CString(s) }

func cfree(p *C.char) { C.free(unsafe.Pointer(p)) }

// withC 把几个 Go 字符串转成 C 字符串交给 f，用完释放
func withC(f func(ps []*C.char), ss ...string) {
	ps := make([]*C.char, len(ss))
	for i, s := range ss {
		ps[i] = cstr(s)
	}
	defer func() {
		for _, p := range ps {
			cfree(p)
		}
	}()
	f(ps)
}

// ---------------------------------------------------------------- 主线程上的任务

var (
	postMu   sync.Mutex
	postNext int64
	posted   = map[int64]func(){}
)

func register(f func()) int64 {
	postMu.Lock()
	defer postMu.Unlock()
	postNext++
	posted[postNext] = f
	return postNext
}

// uiPost 让 f 在主线程上执行，哪个线程都可以调
func uiPost(f func()) { C.dlPost(C.int64_t(register(f))) }

// uiAfter 过 ms 毫秒后在主线程上执行 f
func uiAfter(ms int, f func()) { C.dlAfter(C.int(ms), C.int64_t(register(f))) }

//export goPosted
func goPosted(tag C.int64_t) {
	postMu.Lock()
	f := posted[int64(tag)]
	delete(posted, int64(tag))
	postMu.Unlock()
	if f != nil {
		f()
	}
}

// ---------------------------------------------------------------- 应用

func uiRun(shot bool)  { C.dlRun(C.bool(shot)) }
func uiTerminate()     { C.dlTerminate() }
func uiStartTimer()    { C.dlStartTimer() }
func uiStopTimer()     { C.dlStopTimer() }
func uiRunLoop(ms int) { C.dlRunLoopFor(C.int(ms)) }
func uiSetMode(m int)  { C.dlSetMode(C.int(m)) }

func uiSetTitle(title, subtitle string) {
	withC(func(p []*C.char) { C.dlSetTitle(p[0], p[1]) }, title, subtitle)
}

func uiSetToolbar(t toolbarState) {
	withC(func(p []*C.char) {
		C.dlSetToolbar(p[0], C.bool(t.back), C.bool(t.fwd), C.bool(t.up), C.bool(t.busy), C.bool(t.rescan), C.int(t.detail), C.bool(t.side), C.bool(t.view))
	}, t.home)
}

func uiSetCrumbs(names, paths []string) {
	withC(func(p []*C.char) { C.dlSetCrumbs(p[0], p[1]) }, strings.Join(names, "\n"), strings.Join(paths, "\n"))
}

func uiSetFooter(s string) { withC(func(p []*C.char) { C.dlSetFooter(p[0]) }, s) }

func uiSetLegend(names []string, colors []uint32) {
	if len(colors) == 0 {
		return
	}
	cs := make([]C.uint32_t, len(colors))
	for i, c := range colors {
		cs[i] = C.uint32_t(c)
	}
	withC(func(p []*C.char) { C.dlSetLegend(p[0], &cs[0], C.int(len(cs))) }, strings.Join(names, "\n"))
}

func uiToast(msg string, isErr bool) {
	withC(func(p []*C.char) { C.dlToast(p[0], C.bool(isErr)) }, msg)
}

// ---------------------------------------------------------------- 起始页

func uiSetCards(cards []volCard, loading bool) {
	n := len(cards)
	arr := (*C.DLCard)(C.calloc(C.size_t(max(n, 1)), C.size_t(unsafe.Sizeof(C.DLCard{}))))
	cs := unsafe.Slice(arr, max(n, 1))
	for i, c := range cards {
		cs[i] = C.DLCard{path: cstr(c.path), title: cstr(c.title), badge: cstr(c.badge), sub: cstr(c.sub), used: C.double(c.used), low: C.bool(c.low)}
	}
	C.dlSetCards(arr, C.int(n), C.bool(loading))
	for i := range cards {
		cfree(cs[i].path)
		cfree(cs[i].title)
		cfree(cs[i].badge)
		cfree(cs[i].sub)
	}
	C.free(unsafe.Pointer(arr))
}

func uiSetHomeTip(show bool) { C.dlSetHomeTip(C.bool(show)) }

// uiListVolumes 列出起始页上的磁盘，哪个线程都可以调
func uiListVolumes() []volInfo {
	var n C.int
	arr := C.dlListVolumes(&n)
	if arr == nil {
		return nil
	}
	defer C.dlFreeVolumes(arr, n)
	var out []volInfo
	for _, v := range unsafe.Slice(arr, int(n)) {
		out = append(out, volInfo{
			path: C.GoString(v.path), name: C.GoString(v.name), format: C.GoString(v.format),
			total: int64(v.total), avail: int64(v.avail), important: int64(v.important),
			root: bool(v.root), removable: bool(v.removable), internal: bool(v.internal), local: bool(v.local),
		})
	}
	return out
}

// ---------------------------------------------------------------- 树图

func uiMapSize() (w, h int32, scale float64) {
	var cw, ch C.int32_t
	var cs C.double
	C.dlMapSize(&cw, &ch, &cs)
	return int32(cw), int32(ch), float64(cs)
}

// uiMapSetTiles 把排好的方块（按先父后子的顺序）交给界面去画
func uiMapSetTiles(flat []*tile, scanning bool, scale float64) {
	n := len(flat)
	arr := (*C.DLTile)(C.calloc(C.size_t(max(n, 1)), C.size_t(unsafe.Sizeof(C.DLTile{}))))
	ts := unsafe.Slice(arr, max(n, 1))
	var text []byte
	put := func(s string) (C.int32_t, C.int32_t) {
		off := len(text)
		text = append(text, s...)
		return C.int32_t(off), C.int32_t(len(s))
	}
	for i, t := range flat {
		name, size := t.labels(scanning)
		ct := &ts[i]
		ct.x0, ct.y0, ct.x1, ct.y1 = C.int32_t(t.r.Left), C.int32_t(t.r.Top), C.int32_t(t.r.Right), C.int32_t(t.r.Bottom)
		ct.ix0, ct.iy0, ct.ix1, ct.iy1 = C.int32_t(t.inner.Left), C.int32_t(t.inner.Top), C.int32_t(t.inner.Right), C.int32_t(t.inner.Bottom)
		ct.kind = C.int32_t(t.kind)
		ct.flags = C.int32_t(t.drawFlags(scanning))
		if t.kind == tileFile {
			ct.color = C.uint32_t(catColor(t.cat))
		}
		ct.name, ct.nameLen = put(name)
		ct.size, ct.sizeLen = put(size)
	}
	ctext := (*C.char)(C.CBytes(append(text, 0)))
	C.dlMapSetTiles(arr, C.int(n), ctext, C.bool(scanning), C.double(scale))
}

func uiMapSetMarks(sel, hover *rect) {
	var s, h rect
	if sel != nil {
		s = *sel
	}
	if hover != nil {
		h = *hover
	}
	C.dlMapSetMarks(C.bool(sel != nil), C.int32_t(s.Left), C.int32_t(s.Top), C.int32_t(s.Right), C.int32_t(s.Bottom),
		C.bool(hover != nil), C.int32_t(h.Left), C.int32_t(h.Top), C.int32_t(h.Right), C.int32_t(h.Bottom))
}

func uiMapSetTip(tip *mapTip) {
	if tip == nil {
		C.dlMapSetTip(nil, nil, nil, nil, nil, nil)
		return
	}
	withC(func(p []*C.char) { C.dlMapSetTip(p[0], p[1], p[2], p[3], p[4], p[5]) },
		tip.name, tip.size, tip.share, strings.Join(tip.lines, "\n"), tip.path, tip.hint)
}

func uiMapSetMessage(msg string) { withC(func(p []*C.char) { C.dlMapSetMessage(p[0]) }, msg) }

func uiMapShotMouse(x, y float64) { C.dlMapShotMouse(C.double(x), C.double(y)) }

func uiMapZoom(from, to rect, useOld bool) {
	C.dlMapZoom(C.int32_t(from.Left), C.int32_t(from.Top), C.int32_t(from.Right), C.int32_t(from.Bottom),
		C.int32_t(to.Left), C.int32_t(to.Top), C.int32_t(to.Right), C.int32_t(to.Bottom), C.bool(useOld))
}

// ---------------------------------------------------------------- 列表和摘要

func uiListReload(n int, newFolder bool) { C.dlListReload(C.int(n), C.bool(newFolder)) }
func uiListSelect(row int, scroll bool)  { C.dlListSelect(C.int(row), C.bool(scroll)) }

func uiSetSide(s sideInfo) {
	withC(func(p []*C.char) { C.dlSetSide(p[0], p[1], p[2], p[3], p[4], p[5], C.bool(true)) },
		s.name, s.size, s.meta, s.note, s.link, s.icon)
}

// ---------------------------------------------------------------- 对话框和菜单

func uiAlert(a alertSpec) (button int, checked bool) {
	shotName := ""
	click, check := -1, false
	if shotFile != "" {
		shotAlerts++
		shotName = strings.TrimSuffix(shotFile, ".png") + "-alert" + strconv.Itoa(shotAlerts) + ".png"
		click, check = shotClick, shotCheck
		shotClick, shotCheck = -1, false
		logf("ALERT %s | %s", a.title, strings.ReplaceAll(a.msg, "\n", " / "))
	}
	var cchecked C.bool
	withC(func(p []*C.char) {
		button = int(C.dlAlert(C.int(a.style), p[0], p[1], p[2], C.int(a.def), C.int(a.destructive), p[3], C.int(a.needCheck), p[4], &cchecked,
			p[5], C.int(click), C.bool(check)))
	}, a.title, a.msg, strings.Join(a.buttons, "\n"), a.check, a.details, shotName)
	return button, bool(cchecked)
}

func uiPickFolder() { C.dlPickFolder() }

func uiPopupMenu(items []menuItem) {
	var b strings.Builder
	for _, it := range items {
		flags := 0
		if it.enabled {
			flags |= 1
		}
		if it.sep {
			flags |= 2
		}
		flags |= it.mods
		b.WriteString(strconv.Itoa(it.tag) + "\t" + strconv.Itoa(flags) + "\t" + it.key + "\t" + it.title + "\n")
	}
	withC(func(p []*C.char) { C.dlPopupMenu(p[0]) }, b.String())
}

// ---------------------------------------------------------------- 文件操作

// uiTrash 把文件或文件夹移到废纸篓，失败时返回原因
func uiTrash(path string) string {
	var r *C.char
	withC(func(p []*C.char) { r = C.dlTrash(p[0]) }, path)
	if r == nil {
		return ""
	}
	defer cfree(r)
	return C.GoString(r)
}

func uiOpenFile(path string) bool {
	var ok C.bool
	withC(func(p []*C.char) { ok = C.dlOpenFile(p[0]) }, path)
	return bool(ok)
}

func uiReveal(path string) { withC(func(p []*C.char) { C.dlReveal(p[0]) }, path) }
func uiCopyText(s string)  { withC(func(p []*C.char) { C.dlCopyText(p[0]) }, s) }
func uiOpenURL(u string)   { withC(func(p []*C.char) { C.dlOpenURL(p[0]) }, u) }
func uiLog(s string)       { withC(func(p []*C.char) { C.dlLog(p[0]) }, s) }
func uiCapture(file string) bool {
	var ok C.bool
	withC(func(p []*C.char) { ok = C.dlCapture(p[0]) }, file)
	return bool(ok)
}

// ---------------------------------------------------------------- 回调

func goStr(p *C.char) string {
	if p == nil {
		return ""
	}
	return C.GoString(p)
}

//export goLaunched
func goLaunched(path *C.char) { mac.onLaunched(goStr(path)) }

//export goOpenPath
func goOpenPath(path *C.char) { mac.openPath(goStr(path)) }

//export goCommand
func goCommand(cmd C.int) { mac.command(int(cmd)) }

//export goValidate
func goValidate(cmd C.int) C.int { return C.int(mac.validate(int(cmd))) }

//export goCard
func goCard(i C.int) { mac.cardClicked(int(i)) }

//export goCrumb
func goCrumb(i C.int) { mac.crumbClicked(int(i)) }

//export goMapResized
func goMapResized() { mac.mapResized() }

//export goMapMouse
func goMapMouse(kind C.int, x, y C.double, clicks C.int) {
	mac.mapMouse(int(kind), int32(x), int32(y), int(clicks))
}

//export goMapAnimDone
func goMapAnimDone() { mac.animDone() }

//export goListRow
func goListRow(i C.int, out *C.DLRow) {
	r := mac.listRow(int(i))
	out.name, out.size, out.files, out.path = cstr(r.name), cstr(r.size), cstr(r.files), cstr(r.path)
	out.share = C.double(r.share)
	out.flags = C.int32_t(r.flags)
}

//export goListSelect
func goListSelect(row C.int) { mac.listSelected(int(row)) }

//export goListOpen
func goListOpen(row C.int) { mac.listOpen(int(row)) }

//export goListMenu
func goListMenu(row C.int) { mac.listMenu(int(row)) }

//export goMenuPick
func goMenuPick(tag C.int) { mac.menuPicked(int(tag)) }

//export goTick
func goTick() { mac.onTick() }

//export goShouldClose
func goShouldClose() C.int {
	if mac.shouldClose() {
		return 1
	}
	return 0
}
