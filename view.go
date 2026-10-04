package main

import (
	"container/heap"
	"slices"
)

// VNode 是发给网页的一个方块：文件夹或文件。字段名都很短，一次刷新可能有好几千个。
type VNode struct {
	N string   `json:"n"`           // 名字
	S int64    `json:"s"`           // 字节数
	D bool     `json:"d,omitempty"` // 是文件夹
	F int64    `json:"f,omitempty"` // 文件夹里一共多少个文件
	P bool     `json:"p,omitempty"` // 还在扫描
	X bool     `json:"x,omitempty"` // 没有权限打开
	C []*VNode `json:"c,omitempty"` // 画得下的子项，从大到小
	R int64    `json:"r,omitempty"` // 太小没发过来的子项合计多少字节
	K int      `json:"k,omitempty"` // 太小没发过来的子项有几个
}

type ViewOpts struct {
	W, H    float64 // 树图的像素尺寸
	MinArea float64 // 小于这个面积（平方像素）的方块不画
	Max     int     // 最多发多少个方块
}

func (o *ViewOpts) normalize() {
	o.W = min(max(o.W, 50), 8000)
	o.H = min(max(o.H, 50), 8000)
	if o.MinArea <= 0 {
		o.MinArea = 24
	}
	o.MinArea = max(o.MinArea, 4)
	if o.Max <= 0 || o.Max > 20000 {
		o.Max = 8000
	}
}

// 文件夹至少要有这么大（平方像素）才往里展开，再小就画不出标题和内容了
const minExpandArea = 900

// 文件夹的标题栏和边距大约占掉的面积比例
const innerRatio = 0.8

// View 生成 parts 这个文件夹的树图数据：按面积从大到小一层层展开，
// 直到方块小得画不出来或者数量到了上限。parts 不存在时退回到最近的上级，
// 返回值 at 是实际显示的路径。list 是这个文件夹的直接子项（给侧边栏用），最多 listMax 个。
func (s *Scan) View(parts []string, o ViewOpts, listMax int) (root *VNode, at []string, list []*VNode) {
	o.normalize()
	scanning := s.Busy()

	s.mu.RLock()
	defer s.mu.RUnlock()

	at = parts
	d := s.find(parts)
	for d == nil && len(at) > 0 {
		at = at[:len(at)-1]
		d = s.find(at)
	}
	root = dirNode(d, scanning)

	pq := &expandQueue{{root, d, o.W * o.H / innerRatio * 0.98}}
	count := 1
	for pq.Len() > 0 && count < o.Max {
		it := heap.Pop(pq).(expandItem)
		if it.area < minExpandArea {
			continue
		}
		inner := it.area * innerRatio
		v := it.v
		total := max(v.S, 1)
		var shown int64
		n := 0
		eachChild(it.d, func(name string, size int64, sub *Dir) bool {
			a := float64(size) / float64(total) * inner
			if a < o.MinArea || count >= o.Max {
				return false
			}
			var c *VNode
			if sub != nil {
				c = dirNode(sub, scanning)
				heap.Push(pq, expandItem{c, sub, a})
			} else {
				c = &VNode{N: name, S: size}
			}
			v.C = append(v.C, c)
			shown += size
			n++
			count++
			return true
		})
		v.R = max(v.S-shown, 0)
		v.K = len(it.d.subs) + len(it.d.list) - n
	}

	eachChild(d, func(name string, size int64, sub *Dir) bool {
		if len(list) >= listMax {
			return false
		}
		if sub != nil {
			list = append(list, dirNode(sub, scanning))
		} else {
			list = append(list, &VNode{N: name, S: size})
		}
		return true
	})
	return root, at, list
}

func dirNode(d *Dir, scanning bool) *VNode {
	return &VNode{N: d.Name, S: d.size.Load(), D: true, F: d.files.Load(), P: scanning && !d.Done(), X: d.denied}
}

// eachChild 按大小从大到小遍历 d 的子文件夹和文件，f 返回 false 时停止。
// 文件在扫描时已经排好序；子文件夹的大小还在变，每次现排。
func eachChild(d *Dir, f func(name string, size int64, sub *Dir) bool) {
	type sized struct {
		d    *Dir
		size int64
	}
	subs := make([]sized, len(d.subs))
	for i, sub := range d.subs {
		subs[i] = sized{sub, sub.size.Load()}
	}
	slices.SortFunc(subs, func(a, b sized) int {
		switch {
		case a.size > b.size:
			return -1
		case a.size < b.size:
			return 1
		}
		return 0
	})
	i, j := 0, 0
	for i < len(subs) || j < len(d.list) {
		if j >= len(d.list) || (i < len(subs) && subs[i].size >= d.list[j].Size) {
			if !f(subs[i].d.Name, subs[i].size, subs[i].d) {
				return
			}
			i++
		} else {
			if !f(d.list[j].Name, d.list[j].Size, nil) {
				return
			}
			j++
		}
	}
}

type expandItem struct {
	v    *VNode
	d    *Dir
	area float64
}

// expandQueue 是按面积排的大顶堆：先展开最大的文件夹，数量到上限时丢掉的都是最小的细节
type expandQueue []expandItem

func (q expandQueue) Len() int           { return len(q) }
func (q expandQueue) Less(i, j int) bool { return q[i].area > q[j].area }
func (q expandQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *expandQueue) Push(x any)        { *q = append(*q, x.(expandItem)) }
func (q *expandQueue) Pop() any {
	old := *q
	it := old[len(old)-1]
	*q = old[:len(old)-1]
	return it
}
