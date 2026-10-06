package main

// 永久删除：不进回收站（废纸篓），直接从磁盘上抹掉。子文件夹尽量并行删，边删边记删掉了多少。
// 符号链接、目录联接（junction）只删链接本身，绝不顺着它删到别处去。
// 真正删一项（deleteEntry）、错误怎么说（deleteErrorText）由各个平台自己实现。

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

type deleteJob struct {
	path  string
	parts []string // 在扫描树里的位置

	files  atomic.Int64 // 删掉的文件数
	bytes  atomic.Int64 // 删掉的字节数
	failed atomic.Int64 // 删不掉的个数
	stop   atomic.Bool
	cur    atomic.Pointer[string]

	mu   sync.Mutex
	errs []string // 删不掉的（最多记 50 条）

	sem  chan struct{}
	done chan struct{}
}

// startDelete 在后台永久删除 path，结束时调用 onDone（在后台线程上）。
func startDelete(path string, parts []string, onDone func()) *deleteJob {
	j := &deleteJob{path: path, parts: parts, sem: make(chan struct{}, 16), done: make(chan struct{})}
	go func() {
		defer close(j.done)
		defer onDone()
		fi, err := os.Lstat(path)
		if err != nil {
			j.fail(path, err)
			return
		}
		if fi.IsDir() && fi.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
			j.removeDir(path)
			return
		}
		if j.remove(path) {
			j.files.Add(1)
			j.bytes.Add(diskSize(fi))
		}
	}()
	return j
}

// removeDir 删掉文件夹里的所有东西，再删文件夹本身。子文件夹尽量并行删。
func (j *deleteJob) removeDir(path string) {
	if j.stop.Load() {
		return
	}
	j.cur.Store(&path)
	f, err := os.Open(path)
	if err != nil && unlockDir(path, err) {
		f, err = os.Open(path)
	}
	if err != nil {
		j.fail(path, err)
		return
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil && len(entries) == 0 {
		j.fail(path, err)
		return
	}
	var wg sync.WaitGroup
	for _, e := range entries {
		if j.stop.Load() {
			break
		}
		p := filepath.Join(path, e.Name())
		if e.IsDir() { // 真正的文件夹；目录联接、符号链接不算，下面当成普通项删掉链接本身
			select {
			case j.sem <- struct{}{}:
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() { <-j.sem }()
					j.removeDir(p)
				}()
			default:
				j.removeDir(p)
			}
			continue
		}
		var size int64
		if info, err := e.Info(); err == nil {
			size = diskSize(info)
		}
		if j.remove(p) {
			j.files.Add(1)
			j.bytes.Add(size)
		}
	}
	wg.Wait()
	if j.stop.Load() {
		return
	}
	j.remove(path)
}

// remove 删除一个文件、一个空文件夹或者一个链接。
func (j *deleteJob) remove(path string) bool {
	err := deleteEntry(path)
	if err != nil {
		// 文件夹里有删不掉的东西时，文件夹本身自然也删不掉，那些东西已经记过了
		if !isDirNotEmpty(err) {
			j.fail(path, err)
		}
		return false
	}
	return true
}

func (j *deleteJob) fail(path string, err error) {
	j.failed.Add(1)
	j.mu.Lock()
	if len(j.errs) < 50 {
		j.errs = append(j.errs, path+"："+deleteErrorText(err))
	}
	j.mu.Unlock()
}
