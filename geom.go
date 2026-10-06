package main

// 和平台无关的几何与颜色小工具：树图排版用的整数像素矩形，以及 0xRRGGBB 颜色的混合、HSL 换算。

import "math"

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) W() int32 { return r.Right - r.Left }
func (r rect) H() int32 { return r.Bottom - r.Top }
func (r rect) has(x, y int32) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}
func mkRect(x, y, w, h int32) rect { return rect{x, y, x + w, y + h} }

type point struct{ X, Y int32 }

// rgb 是 0xRRGGBB 形式的颜色
type rgb uint32

func (c rgb) r() float64 { return float64(c >> 16 & 0xFF) }
func (c rgb) g() float64 { return float64(c >> 8 & 0xFF) }
func (c rgb) b() float64 { return float64(c & 0xFF) }

// mix 按 t（0~1）把 c 往 d 靠
func (c rgb) mix(d rgb, t float64) rgb {
	m := func(a, b float64) uint32 { return uint32(math.Round(a + (b-a)*t)) }
	return rgb(m(c.r(), d.r())<<16 | m(c.g(), d.g())<<8 | m(c.b(), d.b()))
}

func hsl(h, s, l float64) rgb {
	h = math.Mod(h, 360) / 360
	f := func(t float64) uint32 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		var q float64
		if l < 0.5 {
			q = l * (1 + s)
		} else {
			q = l + s - l*s
		}
		p := 2*l - q
		var v float64
		switch {
		case t < 1.0/6:
			v = p + (q-p)*6*t
		case t < 0.5:
			v = q
		case t < 2.0/3:
			v = p + (q-p)*(2.0/3-t)*6
		default:
			v = p
		}
		return uint32(math.Round(math.Max(0, math.Min(1, v)) * 255))
	}
	return rgb(f(h+1.0/3)<<16 | f(h)<<8 | f(h-1.0/3))
}
