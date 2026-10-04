//go:build windows

package main

// 右侧的文件列表：系统自带的 ListView 控件（和资源管理器里的一样），虚拟模式，
// 几十万项也不卡。图标用系统的文件类型图标，「占比」一栏自己画一条小进度条。

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	lvmSetBkColor         = 0x1001
	lvmSetImageList       = 0x1003
	lvmGetNextItem        = 0x100C
	lvmEnsureVisible      = 0x1013
	lvmSetColumnWidth     = 0x101E
	lvmSetTextBkColor     = 0x1026
	lvmSetItemState       = 0x102B
	lvmSetItemCount       = 0x102F
	lvmSetExtendedStyle   = 0x1036
	lvmGetSubItemRect     = 0x1038
	lvmInsertColumnW      = 0x1061
	lvmGetHeader          = 0x101F
	lvnItemChanged        = ^uint32(100) // -101
	lvnColumnClick        = ^uint32(107) // -108
	lvnKeyDown            = ^uint32(154) // -155
	lvnGetDispInfoW       = ^uint32(176) // -177
	lvnOdFindItemW        = ^uint32(178) // -179
	nmClick               = ^uint32(1)   // -2
	nmDblClk              = ^uint32(2)   // -3
	nmReturn              = ^uint32(3)   // -4
	nmRClick              = ^uint32(4)   // -5
	nmCustomDraw          = ^uint32(11)  // -12
	cddsPrepaint          = 0x1
	cddsItemPrepaint      = 0x10001
	cddsSubItemPrepaint   = 0x30001
	cddsSubItemPostpaint  = 0x30002
	cdrfDoDefault         = 0x0
	cdrfNotifyPostPaint   = 0x10
	cdrfNotifyItemDraw    = 0x20
	cdrfNotifySubItemDraw = 0x20
	lvisSelected          = 0x2
	lvisFocused           = 0x1
)

type lvColumn struct {
	Mask      uint32
	Fmt       int32
	Cx        int32
	Text      *uint16
	TextMax   int32
	SubItem   int32
	Image     int32
	Order     int32
	CxMin     int32
	CxDefault int32
	CxIdeal   int32
}

type lvItem struct {
	Mask      uint32
	Item      int32
	SubItem   int32
	State     uint32
	StateMask uint32
	Text      *uint16
	TextMax   int32
	Image     int32
	Param     uintptr
	Indent    int32
	GroupID   int32
	Columns   uint32
	PuColumns uintptr
	PiColFmt  uintptr
	Group     int32
}

type nmLVDispInfo struct {
	Hdr  nmhdr
	Item lvItem
}

type nmListView struct {
	Hdr      nmhdr
	Item     int32
	SubItem  int32
	NewState uint32
	OldState uint32
	Changed  uint32
	Action   point
	Param    uintptr
}

type nmLVCustomDraw struct {
	Hdr       nmhdr
	DrawStage uint32
	Hdc       uintptr
	Rc        rect
	ItemSpec  uintptr
	ItemState uint32
	ItemParam uintptr
	TextColor uint32
	TextBk    uint32
	SubItem   int32
}

// row 是列表里的一行
type row struct {
	name     string
	size     int64
	dir      bool
	files    int64
	denied   bool
	scanning bool
}

type listView struct {
	hwnd       uintptr
	rows       []row
	total      int64 // 当前文件夹的总大小，算占比用
	icons      map[string]int32
	dirIcon    int32
	buf        []uint16
	scale      float64
	font       uintptr
	barBg, bar rgb
	// 程序改选中项时不要再回调「选中项变了」
	quiet bool
}

func newListView(parent uintptr, font uintptr, scale float64) *listView {
	const (
		lvsReport, lvsSingleSel, lvsShowSelAlways, lvsShareImageLists, lvsOwnerData = 0x1, 0x4, 0x8, 0x40, 0x1000
		lvsExFullRowSelect, lvsExDoubleBuffer, lvsExLabelTip                        = 0x20, 0x10000, 0x4000
	)
	hwnd, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16("SysListView32"))), 0,
		wsChild|wsVisible|wsTabStop|lvsReport|lvsSingleSel|lvsShowSelAlways|lvsShareImageLists|lvsOwnerData,
		0, 0, 100, 100, parent, 100, moduleHandle(), 0)
	lv := &listView{hwnd: hwnd, icons: map[string]int32{}, buf: make([]uint16, 512), scale: scale, font: font,
		barBg: 0xE6EAF0, bar: 0x5B9BF8}
	pSetWindowTheme.Call(hwnd, uintptr(unsafe.Pointer(u16("Explorer"))), 0)
	sendMessage(hwnd, lvmSetExtendedStyle, 0, lvsExFullRowSelect|lvsExDoubleBuffer|lvsExLabelTip)
	sendMessage(hwnd, wmSetFont, font, 0)

	// 系统的小图标列表：和资源管理器里看到的文件图标一样
	var sfi shFileInfo
	il, _, _ := pSHGetFileInfoW.Call(uintptr(unsafe.Pointer(u16("C:\\"))), 0, uintptr(unsafe.Pointer(&sfi)), unsafe.Sizeof(sfi), shgfiSysIconIndex|shgfiSmallIcon)
	if il != 0 {
		sendMessage(hwnd, lvmSetImageList, 1 /* LVSIL_SMALL */, il)
	}
	lv.dirIcon = fileIcon("folder", true)

	cols := []struct {
		name  string
		width int32
		right bool
	}{{"名称", 190, false}, {"大小", 78, true}, {"占比", 96, false}, {"文件数", 70, true}}
	for i, c := range cols {
		col := lvColumn{Mask: 0x1 | 0x2 | 0x4 | 0x8, Cx: int32(float64(c.width) * scale), Text: u16(c.name), SubItem: int32(i)}
		if c.right {
			col.Fmt = 1 // LVCFMT_RIGHT
		}
		sendMessage(hwnd, lvmInsertColumnW, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}
	return lv
}

func (lv *listView) setFont(font uintptr, scale float64) {
	lv.font, lv.scale = font, scale
	sendMessage(lv.hwnd, wmSetFont, font, 1)
}

// fitColumns 让「名称」一栏占满剩下的宽度
func (lv *listView) fitColumns(width int32) {
	s := lv.scale
	fixed := int32(78*s) + int32(96*s) + int32(70*s)
	scroll := int32(20 * s)
	sendMessage(lv.hwnd, lvmSetColumnWidth, 0, uintptr(max(int32(120*s), width-fixed-scroll)))
	sendMessage(lv.hwnd, lvmSetColumnWidth, 1, uintptr(int32(78*s)))
	sendMessage(lv.hwnd, lvmSetColumnWidth, 2, uintptr(int32(96*s)))
	sendMessage(lv.hwnd, lvmSetColumnWidth, 3, uintptr(int32(70*s)))
}

// setRows 换成新的内容。sameFolder 为 true 时保留滚动位置（扫描中的实时刷新）。
func (lv *listView) setRows(rows []row, total int64, sameFolder bool) {
	lv.rows, lv.total = rows, total
	flags := uintptr(0)
	if sameFolder {
		flags = 0x2 // LVSICF_NOSCROLL
	}
	sendMessage(lv.hwnd, lvmSetItemCount, uintptr(len(rows)), flags)
	invalidate(lv.hwnd, nil)
}

func (lv *listView) selected() int {
	r := sendMessage(lv.hwnd, lvmGetNextItem, ^uintptr(0), 0x2 /* LVNI_SELECTED */)
	return int(int32(r))
}

// selectName 选中名字为 name 的一行，name 为空时取消选中。show 为 true 时滚动到这一行。
func (lv *listView) selectName(name string, show bool) {
	lv.quiet = true
	defer func() { lv.quiet = false }()
	item := lvItem{StateMask: lvisSelected | lvisFocused}
	sendMessage(lv.hwnd, lvmSetItemState, ^uintptr(0), uintptr(unsafe.Pointer(&item)))
	if name == "" {
		return
	}
	for i, r := range lv.rows {
		if r.name == name {
			item.State = lvisSelected | lvisFocused
			sendMessage(lv.hwnd, lvmSetItemState, uintptr(i), uintptr(unsafe.Pointer(&item)))
			if show {
				sendMessage(lv.hwnd, lvmEnsureVisible, uintptr(i), 0)
			}
			return
		}
	}
}

// onNotify 处理列表发来的通知。返回值 handled=false 时交给默认处理。
func (lv *listView) onNotify(lp uintptr) (uintptr, bool) {
	hdr := (*nmhdr)(ptr(lp))
	switch hdr.Code {
	case lvnGetDispInfoW:
		di := (*nmLVDispInfo)(ptr(lp))
		i := int(di.Item.Item)
		if i < 0 || i >= len(lv.rows) {
			return 0, true
		}
		r := lv.rows[i]
		if di.Item.Mask&0x1 != 0 && di.Item.Text != nil && di.Item.TextMax > 0 {
			var s string
			switch di.Item.SubItem {
			case 0:
				s = r.name
				if r.denied {
					s += "（无权限）"
				}
			case 1:
				s = humanSize(r.size)
				if r.scanning {
					s = "… " + s
				}
			case 2:
				s = "" // 自己画
			case 3:
				if r.dir {
					s = formatCount(r.files)
				}
			}
			dst := unsafe.Slice(di.Item.Text, di.Item.TextMax)
			src, _ := windows.UTF16FromString(s)
			n := copy(dst, src)
			dst[min(n, len(dst)-1)] = 0
		}
		if di.Item.Mask&0x2 != 0 {
			if r.dir {
				di.Item.Image = lv.dirIcon
			} else {
				di.Item.Image = lv.iconFor(r.name)
			}
		}
		return 0, true
	case lvnOdFindItemW:
		return ^uintptr(0), true // 不支持按键盘字母跳转
	case nmCustomDraw:
		cd := (*nmLVCustomDraw)(ptr(lp))
		switch cd.DrawStage {
		case cddsPrepaint:
			return cdrfNotifyItemDraw, true
		case cddsItemPrepaint:
			return cdrfNotifySubItemDraw, true
		case cddsSubItemPrepaint:
			if cd.SubItem == 2 {
				return cdrfNotifyPostPaint, true
			}
			return cdrfDoDefault, true
		case cddsSubItemPostpaint:
			if cd.SubItem == 2 {
				lv.drawShare(cd)
			}
			return cdrfDoDefault, true
		}
		return cdrfDoDefault, true
	}
	return 0, false
}

// drawShare 在「占比」一栏画一条小进度条加百分数
func (lv *listView) drawShare(cd *nmLVCustomDraw) {
	i := int(cd.ItemSpec)
	if i < 0 || i >= len(lv.rows) || lv.total <= 0 {
		return
	}
	r := rect{Top: 2, Left: 0} // 第 2 列（从 0 数）的 LVIR_BOUNDS
	sendMessage(lv.hwnd, lvmGetSubItemRect, uintptr(i), uintptr(unsafe.Pointer(&r)))
	share := float64(lv.rows[i].size) / float64(lv.total)
	s := lv.scale
	h := int32(5 * s)
	barW := int32(30 * s)
	bar := rect{r.Left + int32(6*s), r.Top + (r.H()-h)/2, r.Left + int32(6*s) + barW, r.Top + (r.H()-h)/2 + h}
	g := newGP(cd.Hdc)
	g.roundRect(bar, h/2, lv.barBg, 255)
	if w := int32(float64(barW) * share); w > 0 {
		g.roundRect(rect{bar.Left, bar.Top, bar.Left + max(w, h), bar.Bottom}, h/2, lv.bar, 255)
	}
	g.close()
	textLine(cd.Hdc, lv.font, formatPercent(share), rect{bar.Right + int32(6*s), r.Top, r.Right - int32(2*s), r.Bottom}, 0x5C616D, dtLeft)
}

func (lv *listView) iconFor(name string) int32 {
	ext := ""
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		ext = strings.ToLower(name[i:])
	}
	// exe、快捷方式、图标文件每个都不一样，只能用通用图标；其他按扩展名缓存
	if idx, ok := lv.icons[ext]; ok {
		return idx
	}
	idx := fileIcon("x"+ext, false)
	lv.icons[ext] = idx
	return idx
}

type shFileInfo struct {
	Icon        uintptr
	IconIndex   int32
	Attributes  uint32
	DisplayName [260]uint16
	TypeName    [80]uint16
}

const (
	shgfiSysIconIndex      = 0x4000
	shgfiSmallIcon         = 0x1
	shgfiUseFileAttributes = 0x10
)

// fileIcon 取某种文件（按扩展名）或文件夹在系统图标列表里的序号，不读磁盘
func fileIcon(name string, dir bool) int32 {
	attr := uintptr(0x80) // FILE_ATTRIBUTE_NORMAL
	if dir {
		attr = 0x10 // FILE_ATTRIBUTE_DIRECTORY
	}
	var sfi shFileInfo
	pSHGetFileInfoW.Call(uintptr(unsafe.Pointer(u16(name))), attr, uintptr(unsafe.Pointer(&sfi)), unsafe.Sizeof(sfi),
		shgfiSysIconIndex|shgfiSmallIcon|shgfiUseFileAttributes)
	return sfi.IconIndex
}
