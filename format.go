package main

import (
	"fmt"
	"strconv"
)

// humanSize 把字节数写成 1.23 GB 这种样子：小于 10 保留两位小数，小于 100 保留一位
func humanSize(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	f := float64(n) / 1024
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	switch {
	case f < 10:
		return fmt.Sprintf("%.2f %s", f, units[i])
	case f < 100:
		return fmt.Sprintf("%.1f %s", f, units[i])
	}
	return fmt.Sprintf("%.0f %s", f, units[i])
}

// formatCount 给数字加千分位：1636235 → 1,636,235
func formatCount(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := n < 0
	if neg {
		s = s[1:]
	}
	out := make([]byte, 0, len(s)+len(s)/3)
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func formatPercent(f float64) string {
	p := f * 100
	switch {
	case p <= 0:
		return "0%"
	case p < 0.1:
		return "<0.1%"
	case p < 10:
		return fmt.Sprintf("%.1f%%", p)
	}
	return fmt.Sprintf("%.0f%%", p)
}

func formatSeconds(s float64) string {
	if s < 60 {
		return fmt.Sprintf("用时 %.1f 秒", s)
	}
	return fmt.Sprintf("用时 %d 分 %d 秒", int(s)/60, int(s)%60)
}
