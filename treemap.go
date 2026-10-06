package main

// 矩形树图：把 View 给出的树排成一块块方块（squarified 算法，方块尽量接近正方形）。
// 怎么画由各个平台自己负责（treemap_draw_windows.go、Mac 版的 Objective-C 代码）。

import (
	"fmt"
	"math"
	"strings"
)

const (
	tileDir = iota
	tileFile
	tileRest // 一堆太小画不出来的文件合成的一块
)

type tile struct {
	v      *VNode
	kind   int
	r      rect
	inner  rect // 文件夹里放子方块的区域
	header bool // 文件夹有没有标题栏
	depth  int
	cat    int
	parent *tile
	kids   []*tile
}

// parts 是这个方块相对当前视图根目录的路径
func (t *tile) parts() []string {
	var p []string
	for x := t; x != nil && x.parent != nil; x = x.parent {
		if x.kind == tileRest {
			continue
		}
		p = append(p, x.v.N)
	}
	for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
		p[i], p[j] = p[j], p[i]
	}
	return p
}

// ---------------------------------------------------------------- 文件类型

type category struct {
	name string
	hue  float64
	sat  float64
}

var categories = []category{
	{"视频", 6, 0.78},
	{"图片", 32, 0.86},
	{"音乐", 48, 0.82},
	{"压缩包/镜像", 268, 0.58},
	{"文档", 212, 0.76},
	{"程序", 170, 0.52},
	{"代码", 122, 0.42},
	{"缓存/日志", 220, 0.14},
	{"其他", 30, 0.16},
}

const catOther = 8

var extCategory = func() map[string]int {
	m := map[string]int{}
	add := func(cat int, exts string) {
		for _, e := range strings.Fields(exts) {
			m[e] = cat
		}
	}
	add(0, "mp4 mkv avi mov wmv flv webm m4v ts mts m2ts rmvb rm 3gp mpg mpeg vob f4v")
	add(1, "jpg jpeg png gif bmp webp heic heif tif tiff psd raw cr2 cr3 nef arw dng svg ico avif jfif")
	add(2, "mp3 flac wav aac m4a ogg wma ape opus alac aiff mid")
	add(3, "zip rar 7z tar gz tgz bz2 xz zst iso img dmg cab wim esd vhd vhdx vmdk vdi qcow2 apk xapk ipa")
	add(4, "pdf doc docx xls xlsx ppt pptx txt md csv rtf odt ods odp epub mobi pages key numbers one chm")
	add(5, "exe dll sys msi msix msixbundle appx appxbundle drv ocx so dylib jar bin com scr mui")
	add(6, "js ts jsx tsx py go c cc cpp h hpp cs java kt rs rb php html htm css scss json xml yml yaml toml ini sh ps1 bat cmd lua swift dart vue sql ipynb")
	add(7, "tmp temp log etl dmp mdmp cache db sqlite sqlite3 ldb dat bak old evtx pf blf regtrans-ms edb jrs chk")
	return m
}()

func categoryOf(name string) int {
	i := strings.LastIndexByte(name, '.')
	if i < 0 || i == len(name)-1 {
		return catOther
	}
	ext := strings.ToLower(name[i+1:])
	if c, ok := extCategory[ext]; ok {
		return c
	}
	return catOther
}

func catColor(c int) rgb {
	k := categories[c]
	return hsl(k.hue, k.sat, 0.68)
}

// ---------------------------------------------------------------- 排版

type mapMetrics struct {
	header int32 // 文件夹标题栏高度
	pad    int32 // 文件夹边框和子方块之间的空隙
}

// layoutTree 把 root 排进 r。root 本身就是当前视图，不画标题栏。
func layoutTree(root *VNode, r rect, m mapMetrics) *tile {
	t := &tile{v: root, kind: tileDir, r: r, depth: 0}
	t.inner = rect{r.Left + 2, r.Top + 2, r.Right - 2, r.Bottom - 2}
	layoutKids(t, m)
	return t
}

func layoutKids(t *tile, m mapMetrics) {
	v := t.v
	if len(v.C) == 0 && v.R <= 0 {
		return
	}
	in := t.inner
	if in.W() < 2 || in.H() < 2 {
		return
	}
	items := make([]sqItem, 0, len(v.C)+1)
	for _, c := range v.C {
		if c.S > 0 {
			items = append(items, sqItem{node: c, size: float64(c.S)})
		}
	}
	if v.R > 0 {
		items = append(items, sqItem{size: float64(v.R)})
	}
	squarify(items, float64(in.Left), float64(in.Top), float64(in.W()), float64(in.H()))
	for _, it := range items {
		// 四条边都取整，相邻方块正好共用一条边，不会有缝或重叠
		r := rect{int32(math.Round(it.x)), int32(math.Round(it.y)), int32(math.Round(it.x + it.w)), int32(math.Round(it.y + it.h))}
		if r.W() <= 0 || r.H() <= 0 {
			continue
		}
		k := &tile{r: r, parent: t, depth: t.depth + 1}
		switch {
		case it.node == nil:
			k.kind = tileRest
			k.v = &VNode{N: fmt.Sprintf("%d 个较小的项目", v.K), S: v.R}
		case it.node.D:
			k.kind = tileDir
			k.v = it.node
		default:
			k.kind = tileFile
			k.v = it.node
			k.cat = categoryOf(it.node.N)
		}
		t.kids = append(t.kids, k)
		if k.kind == tileDir {
			pad := m.pad
			if r.W() < 4*pad+8 || r.H() < 4*pad+8 {
				pad = 1
			}
			if r.H() >= 2*m.header+4*pad && r.W() >= m.header*2 {
				k.header = true
				k.inner = rect{r.Left + pad, r.Top + m.header, r.Right - pad, r.Bottom - pad}
			} else {
				k.inner = rect{r.Left + pad, r.Top + pad, r.Right - pad, r.Bottom - pad}
			}
			layoutKids(k, m)
		}
	}
}

type sqItem struct {
	node       *VNode
	size       float64
	x, y, w, h float64
}

// squarify 是 Bruls 等人的 squarified treemap：一行一行地放，
// 每加一个方块就看这一行最扁的那个是不是更扁了，更扁就换行。
func squarify(items []sqItem, x, y, w, h float64) {
	var total float64
	for _, it := range items {
		total += it.size
	}
	if total <= 0 || w <= 0 || h <= 0 {
		return
	}
	scale := w * h / total
	i := 0
	for i < len(items) {
		short := math.Min(w, h)
		if short <= 0 {
			return
		}
		j := i
		var sum, minA, maxA float64
		best := math.Inf(1)
		minA = math.Inf(1)
		for j < len(items) {
			a := items[j].size * scale
			s := sum + a
			mn, mx := math.Min(minA, a), math.Max(maxA, a)
			worst := math.Max(short*short*mx/(s*s), s*s/(short*short*mn))
			if j > i && worst > best {
				break
			}
			best, sum, minA, maxA = worst, s, mn, mx
			j++
		}
		thick := sum / short
		if j == len(items) {
			// 最后一行把剩下的空间全部占满，避免浮点误差留下细缝
			if w >= h {
				thick = w
			} else {
				thick = h
			}
		}
		off := 0.0
		for k := i; k < j; k++ {
			a := items[k].size * scale
			l := a / thick
			if k == j-1 {
				l = short - off // 同理，这一行最后一个顶到头
			}
			if w >= h {
				items[k].x, items[k].y, items[k].w, items[k].h = x, y+off, thick, l
			} else {
				items[k].x, items[k].y, items[k].w, items[k].h = x+off, y, l, thick
			}
			off += l
		}
		if w >= h {
			x += thick
			w -= thick
		} else {
			y += thick
			h -= thick
		}
		i = j
	}
}

// hitTest 找到 (x, y) 处最里层的方块
func hitTest(t *tile, x, y int32) *tile {
	if t == nil || !t.r.has(x, y) {
		return nil
	}
	for _, k := range t.kids {
		if k.r.has(x, y) {
			if h := hitTest(k, x, y); h != nil {
				return h
			}
			return k
		}
	}
	return t
}

// findTile 按相对路径找方块（可能因为太小没画出来，返回 nil）
func findTile(root *tile, parts []string) *tile {
	t := root
	for _, p := range parts {
		var next *tile
		for _, k := range t.kids {
			if k.kind != tileRest && k.v.N == p {
				next = k
				break
			}
		}
		if next == nil {
			return nil
		}
		t = next
	}
	return t
}

// tipText 是鼠标指着方块时提示框里的几行字（名字和大小之外的）
type tipText struct {
	share string   // 「占当前文件夹 12%」
	lines []string // 「文件夹 · 1,234 个文件」之类
	hint  string   // 「双击进入 · 右键更多操作」
}

// tip 生成方块的提示文字。viewSize 是当前视图根目录的大小，用来算占比。
func (t *tile) tip(viewSize int64) tipText {
	v := t.v
	var tt tipText
	if viewSize > 0 {
		tt.share = "占当前文件夹 " + formatPercent(float64(v.S)/float64(viewSize))
	}
	switch t.kind {
	case tileDir:
		kind := "文件夹 · " + formatCount(v.F) + " 个文件"
		if v.X {
			kind += " · 没有权限打开"
		}
		tt.lines = append(tt.lines, kind)
		tt.hint = "双击进入 · 右键更多操作"
	case tileFile:
		tt.lines = append(tt.lines, categories[t.cat].name+"文件")
		tt.hint = "右键可以打开、定位或删除"
	case tileRest:
		tt.lines = append(tt.lines, "这些项目太小，画不出来")
		tt.hint = "双击进入所在的文件夹看列表"
	}
	return tt
}
