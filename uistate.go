package main

// 两个平台的界面共用的状态和文字。

import "fmt"

// 界面的两种模式：起始页（选磁盘）和查看扫描结果
const (
	modeHome = iota
	modeView
)

// 三档细节：最小方块面积（平方像素，按 96 DPI 或者 1 倍屏算）和最多画多少块
var detailArea = [3]float64{90, 30, 10}
var detailMax = [3]int{3000, 8000, 16000}

// parts 是扫描进度的几段话，界面放不下时从后往前去掉：
// 「扫描完成 · 1,234 个文件 · 5.6 GB · 用时 3.2 秒」
func (st Status) parts() []string {
	secs := float64(st.Elapsed) / 1000
	switch st.State {
	case "scanning":
		return []string{"正在扫描", formatCount(st.Files) + " 个文件", humanSize(st.Bytes), fmt.Sprintf("%.0f 秒", secs)}
	case "stopped":
		return []string{"已停止", formatCount(st.Files) + " 个文件", humanSize(st.Bytes)}
	}
	return []string{"扫描完成", formatCount(st.Files) + " 个文件", humanSize(st.Bytes), formatSeconds(secs)}
}

// statusParts 是永久删除进行中的几段话
func (j *deleteJob) statusParts(name string) []string {
	return []string{"正在删除「" + name + "」", fmt.Sprintf("已删除 %s 个文件", formatCount(j.files.Load())), "释放 " + humanSize(j.bytes.Load())}
}
