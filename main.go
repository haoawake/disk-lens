// 文件清理助手：像 SpaceSniffer 一样，选一个盘飞快地扫一遍，用一块块方块把所有文件夹和文件
// 按大小画出来，边扫边更新。双击文件夹钻进去，一眼看出空间都被谁占了；
// 右键可以把不要的东西移到回收站或者永久删除。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"time"
)

// 发版时由 -ldflags "-X main.version=x.y.z" 写入
var version = "dev"

const (
	appName = "文件清理助手"
	repoURL = "https://github.com/haoawake/disk-lens"
)

func init() {
	// 窗口和它的消息循环必须一直待在同一个系统线程上
	runtime.LockOSThread()
}

func main() {
	bench := flag.String("bench", "", "只扫描这个路径并打印用时，不打开窗口（测速用）")
	workers := flag.Int("workers", 0, "扫描并发数")
	showVersion := flag.Bool("version", false, "显示版本号")
	flag.StringVar(&shotFile, "shot", "", "")
	flag.StringVar(&shotSteps, "steps", "", "")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *workers > 0 {
		forceWorkers = *workers
	}
	if *bench != "" {
		runBench(*bench)
		return
	}
	if isAdmin() {
		enablePrivileges()
	}
	// 把文件夹拖到程序图标上，或者以管理员身份重新打开时，直接扫描传进来的路径
	runApp(flag.Arg(0))
}

func fatal(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	showError(msg)
	os.Exit(1)
}

// friendlyError 把常见的系统错误换成看得懂的话
func friendlyError(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "找不到这个文件夹，请检查路径。"
	case errors.Is(err, fs.ErrPermission):
		return "没有权限访问它，可以试试以管理员身份运行。"
	}
	return err.Error()
}

// runBench 扫描一个路径，打印进度和用时
func runBench(path string) {
	start := time.Now()
	s, err := NewScan(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("扫描 %s，并发 %d\n", s.RootPath, workersFor())
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-done:
			st := s.Status()
			fmt.Printf("完成：%d 个文件，%d 个文件夹，%s，%d 个打不开，用时 %.2f 秒\n",
				st.Files, st.Dirs, humanSize(st.Bytes), st.Denied, time.Since(start).Seconds())
			var m runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&m)
			fmt.Printf("内存：%s\n", humanSize(int64(m.HeapAlloc)))
			runtime.KeepAlive(s)
			return
		case <-tick.C:
			st := s.Status()
			fmt.Printf("  %d 个文件，%s\n", st.Files, humanSize(st.Bytes))
		}
	}
}
