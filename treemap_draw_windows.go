//go:build windows

package main

// 把排好的树图画到一张底图上（GDI）。鼠标悬停、选中的高亮另外画在底图上面，不用每次重排重画。

import "unsafe"

// ---------------------------------------------------------------- 绘制

type mapStyle struct {
	bg, dirBorder, dirHeader, dirHeader2, dirFill, dirText, dirText2 rgb
	tileText, tileText2, rest, restLine, scanning                    rgb
	font, fontBold, fontSmall                                        uintptr
	hatch                                                            uintptr // 斜线画刷
	scale                                                            float64
	scanningNow                                                      bool
}

func renderTree(hdc uintptr, root *tile, st *mapStyle) {
	fill(hdc, root.r, st.bg)
	for _, k := range root.kids {
		renderTile(hdc, k, st)
	}
}

func renderTile(hdc uintptr, t *tile, st *mapStyle) {
	r := t.r
	switch t.kind {
	case tileFile:
		c := catColor(t.cat)
		in := rect{r.Left, r.Top, r.Right - 1, r.Bottom - 1}
		if in.W() <= 0 || in.H() <= 0 {
			fill(hdc, r, c)
			return
		}
		if in.W() >= 6 && in.H() >= 6 {
			gradientV(hdc, in, c.mix(0xFFFFFF, 0.3), c.mix(0x000000, 0.06))
		} else {
			fill(hdc, in, c)
		}
		labelTile(hdc, t, in, st.tileText, st.tileText2, st)

	case tileRest:
		in := rect{r.Left, r.Top, r.Right - 1, r.Bottom - 1}
		// 斜线纹理：一眼看出这块是「一堆小东西」
		pSetBkMode.Call(hdc, 2) // OPAQUE：斜线之间的底色
		pSetBkColor.Call(hdc, st.rest.colorref())
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&in)), st.hatch)
		labelTile(hdc, t, in, st.dirText2, st.dirText2, st)

	case tileDir:
		fill(hdc, r, st.dirBorder)
		in := rect{r.Left, r.Top, r.Right - 1, r.Bottom - 1}
		headerColor := st.dirHeader
		if t.depth%2 == 0 {
			headerColor = st.dirHeader2
		}
		fill(hdc, in, headerColor)
		if t.header {
			hr := rect{in.Left + int32(4*st.scale), in.Top, in.Right - int32(4*st.scale), t.inner.Top}
			name := t.v.N
			if t.v.X {
				name += "（无权限）"
			}
			size := humanSize(t.v.S)
			if st.scanningNow && t.v.P {
				size += " · 扫描中"
			}
			sw := textWidth(hdc, st.fontSmall, size)
			nw := textWidth(hdc, st.fontBold, name)
			if hr.W() > nw+sw+int32(10*st.scale) {
				textLine(hdc, st.fontBold, name, rect{hr.Left, hr.Top, hr.Right - sw - int32(6*st.scale), hr.Bottom}, st.dirText, dtLeft)
				c := st.dirText2
				if st.scanningNow && t.v.P {
					c = st.scanning
				}
				textLine(hdc, st.fontSmall, size, rect{hr.Right - sw, hr.Top, hr.Right, hr.Bottom}, c, dtLeft)
			} else {
				textLine(hdc, st.fontBold, name, hr, st.dirText, dtLeft)
			}
		}
		if len(t.kids) == 0 {
			// 没展开（太小或者是空文件夹）：画成一块实心的
			c := st.dirFill
			fill(hdc, t.inner, c)
			if !t.header {
				labelTile(hdc, t, t.inner, st.dirText, st.dirText2, st)
			}
			return
		}
		fill(hdc, t.inner, st.dirFill)
		for _, k := range t.kids {
			renderTile(hdc, k, st)
		}
	}
}

// labelTile 在方块里写名字和大小，地方不够就不写
func labelTile(hdc uintptr, t *tile, r rect, c1, c2 rgb, st *mapStyle) {
	s := st.scale
	if r.W() < int32(34*s) || r.H() < int32(15*s) {
		return
	}
	pad := int32(4 * s)
	lineH := int32(16 * s)
	in := rect{r.Left + pad, r.Top + int32(2*s), r.Right - pad, r.Bottom}
	if r.H() >= 2*lineH+int32(4*s) {
		textLine(hdc, st.font, t.v.N, rect{in.Left, in.Top, in.Right, in.Top + lineH}, c1, dtLeft)
		textLine(hdc, st.fontSmall, humanSize(t.v.S), rect{in.Left, in.Top + lineH, in.Right, in.Top + 2*lineH}, c2, dtLeft)
	} else {
		textLine(hdc, st.font, t.v.N, rect{in.Left, in.Top, in.Right, in.Top + lineH}, c1, dtLeft)
	}
}
