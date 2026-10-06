package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Dir 是扫描树里的一个文件夹。大小和计数用原子变量，扫描线程边扫边往上累加，
// 界面随时来读都能拿到当前的数字；子文件夹、文件列表由 Scan.mu 保护。
type Dir struct {
	Name   string
	parent *Dir

	size  atomic.Int64 // 下面所有文件的总字节数
	files atomic.Int64 // 下面所有文件的个数
	dirs  atomic.Int64 // 下面所有子文件夹的个数（不含自己）

	// 自己的列目录 + 还没扫完的子文件夹数，归零就说明整棵子树扫完了
	pending atomic.Int32

	// 以下字段由 Scan.mu 保护
	subs    []*Dir
	list    []File // 按大小从大到小排好
	denied  bool   // 没有权限打开
	removed bool   // 已经移到回收站或被重新扫描替换，迟到的累加不再往上传
}

// File 是一个文件。0 字节的文件不单独记录，只计数。
type File struct {
	Name string
	Size int64
}

func (d *Dir) Size() int64  { return d.size.Load() }
func (d *Dir) Files() int64 { return d.files.Load() }
func (d *Dir) Done() bool   { return d.pending.Load() <= 0 }

// Scan 是对一个磁盘或文件夹的一次扫描，包括扫描结果和正在进行的扫描任务。
type Scan struct {
	Root     *Dir
	RootPath string

	mu sync.RWMutex

	nFiles, nDirs, nDenied atomic.Int64
	nPrivacy               atomic.Int64           // 打不开的文件夹里，有多少是被系统的隐私保护拦下的（macOS）
	current                atomic.Pointer[string] // 最近在读的文件夹，给界面显示

	hooks scanHooks // 各个平台对扫描的补充，Windows 上为 nil

	linkMu sync.Mutex
	links  map[fileKey]*Dir // 有多个硬链接的文件：第一次是在哪个文件夹里算的大小

	jobMu sync.Mutex
	job   *job

	started  atomic.Int64 // UnixNano
	finished atomic.Int64 // UnixNano，0 表示还没结束
	stopped  atomic.Bool  // 被用户停止
}

// 扫描并发数。固态盘上并发多快得多；实测 USB 机械移动硬盘上 32 个也不比少的慢
// （硬盘自己会给排队的请求排序），所以统一用 32。
var scanWorkers = 32

// forceWorkers 不为 0 时所有扫描都用这个并发数（命令行 -workers）
var forceWorkers int

func workersFor() int {
	if forceWorkers > 0 {
		return forceWorkers
	}
	return scanWorkers
}

// scanHooks 是各个平台对扫描的补充：比如 macOS 上不进入别的磁盘的挂载点、
// 按实际占用的磁盘块计算大小、硬链接只算一次。
type scanHooks interface {
	// displayName 是扫描起点的显示名，返回空字符串表示用文件夹名
	displayName(root string) string
	// device 是 path 所在的设备号（扫描起点用）
	device(path string) uint64
	// enterDir 决定一个子文件夹 show（要不要出现在树里）、enter（要不要读它里面的东西）。
	// parentDev 是上一级所在的设备号，dev 返回它自己的
	enterDir(path string, parentDev uint64, e fs.DirEntry) (show, enter bool, dev uint64)
	// fileSize 是一个文件算多少字节；owner 是它所在的文件夹
	fileSize(s *Scan, info fs.FileInfo, owner *Dir) int64
	// privacyDenied 表示打不开文件夹是因为系统的隐私保护（而不是普通的权限设置）
	privacyDenied(err error) bool
}

// fileKey 是一个文件在磁盘上的身份（设备号 + inode），用来认出同一个文件的多个硬链接
type fileKey struct{ dev, ino uint64 }

// firstLink 表示这是第一次遇到这个有多个硬链接的文件，它的大小算在 owner 里。
// 之前算过它的文件夹被删掉或者重新扫描了的话，换成这一次算。
func (s *Scan) firstLink(dev, ino uint64, owner *Dir) bool {
	k := fileKey{dev, ino}
	s.linkMu.Lock()
	defer s.linkMu.Unlock()
	if s.links == nil {
		s.links = map[fileKey]*Dir{}
	}
	if prev, ok := s.links[k]; ok {
		s.mu.RLock()
		gone := prev.removed
		s.mu.RUnlock()
		if !gone {
			return false
		}
	}
	s.links[k] = owner
	return true
}

// NewScan 开始扫描 root（必须是绝对路径），立即返回，扫描在后台进行。
func NewScan(root string) (*Scan, error) {
	root = filepath.Clean(root)
	fi, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, errors.New("不是文件夹")
	}
	name := filepath.Base(root)
	if vol := filepath.VolumeName(root); vol != "" && len(root) <= len(vol)+1 {
		name = vol // "C:\" 显示成 "C:"
	}
	hooks := newScanHooks(root)
	if hooks != nil {
		if n := hooks.displayName(root); n != "" {
			name = n
		}
	}
	s := &Scan{Root: &Dir{Name: name}, RootPath: root, hooks: hooks}
	s.startJob(s.Root, root, workersFor())
	return s, nil
}

// ---------------------------------------------------------------- 任务

// job 是一次后台遍历：从 top 开始把整棵子树读进来。
type job struct {
	s      *Scan
	top    *Dir
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu    sync.Mutex
	cond  *sync.Cond
	queue []task // 先进先出：各个顶层文件夹一起长大，实时画面更有参考价值
	head  int
	idle  int
	n     int
}

type task struct {
	d    *Dir
	path string
	dev  uint64 // 所在的设备号（只有 macOS 用到）
}

func (s *Scan) startJob(top *Dir, path string, workers int) *job {
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{s: s, top: top, ctx: ctx, cancel: cancel, done: make(chan struct{}), n: workers}
	j.cond = sync.NewCond(&j.mu)
	top.pending.Store(1)
	var dev uint64
	if s.hooks != nil {
		dev = s.hooks.device(path)
	}
	j.queue = append(j.queue, task{top, path, dev})
	s.jobMu.Lock()
	s.job = j
	s.jobMu.Unlock()
	s.started.Store(time.Now().UnixNano())
	s.finished.Store(0)
	s.stopped.Store(false)
	for range workers {
		go j.work()
	}
	go func() {
		select {
		case <-j.done:
		case <-ctx.Done():
		}
		j.mu.Lock()
		j.cond.Broadcast() // 叫醒还在等活的线程，让它们退出
		j.mu.Unlock()
		s.finished.CompareAndSwap(0, time.Now().UnixNano()) // 被停止的情况
	}()
	return j
}

// Stop 停止正在进行的扫描，已经扫到的结果保留。
func (s *Scan) Stop() {
	s.jobMu.Lock()
	j := s.job
	s.jobMu.Unlock()
	if j == nil {
		return
	}
	select {
	case <-j.done:
		return
	default:
	}
	s.stopped.Store(true)
	j.cancel()
}

// Busy 表示还有扫描任务在跑。
func (s *Scan) Busy() bool {
	s.jobMu.Lock()
	j := s.job
	s.jobMu.Unlock()
	if j == nil {
		return false
	}
	select {
	case <-j.done:
		return false
	case <-j.ctx.Done():
		return false
	default:
		return true
	}
}

// Wait 等当前任务结束（扫完或被停止），测试和命令行用。
func (s *Scan) Wait() {
	s.jobMu.Lock()
	j := s.job
	s.jobMu.Unlock()
	if j != nil {
		select {
		case <-j.done:
		case <-j.ctx.Done():
		}
	}
}

func (j *job) work() {
	for {
		t, ok := j.next()
		if !ok {
			return
		}
		j.read(t)
	}
}

func (j *job) next() (task, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for j.head == len(j.queue) {
		if j.ctx.Err() != nil || j.finishedLocked() {
			return task{}, false
		}
		j.idle++
		j.cond.Wait()
		j.idle--
	}
	if j.ctx.Err() != nil {
		return task{}, false
	}
	t := j.queue[j.head]
	j.queue[j.head] = task{}
	j.head++
	// 队头用掉一大半时整理一次，免得底层数组只增不减
	if j.head > 4096 && j.head*2 > len(j.queue) {
		j.queue = append(j.queue[:0], j.queue[j.head:]...)
		j.head = 0
	}
	return t, true
}

func (j *job) finishedLocked() bool {
	select {
	case <-j.done:
		return true
	default:
		return false
	}
}

func (j *job) push(ts []task) {
	if len(ts) == 0 {
		return
	}
	j.mu.Lock()
	j.queue = append(j.queue, ts...)
	if j.idle > 0 {
		if len(ts) == 1 {
			j.cond.Signal()
		} else {
			j.cond.Broadcast()
		}
	}
	j.mu.Unlock()
}

// read 读一个文件夹：记下文件、建好子文件夹、把大小往上累加，子文件夹排进队列。
func (j *job) read(t task) {
	s, d := j.s, t.d
	s.current.Store(&t.path)

	var (
		list    []File
		subs    []*Dir
		bytes   int64
		nfiles  int64
		denied  bool
		pending []task
	)
	h := s.hooks
	f, err := os.Open(t.path)
	if err == nil {
		var entries []fs.DirEntry
		// 不用 os.ReadDir：它会按名字排序，这里用不着
		entries, err = f.ReadDir(-1)
		f.Close()
		for _, e := range entries {
			if e.IsDir() { // 符号链接、目录联接（junction）不算文件夹，不跟进去，避免重复统计和死循环
				p := filepath.Join(t.path, e.Name())
				enter := true
				var dev uint64
				if h != nil {
					var show bool
					if show, enter, dev = h.enterDir(p, t.dev, e); !show {
						continue
					}
				}
				sub := &Dir{Name: e.Name(), parent: d}
				subs = append(subs, sub)
				if enter {
					sub.pending.Store(1)
					pending = append(pending, task{sub, p, dev})
				}
				continue
			}
			info, ierr := e.Info()
			if ierr != nil {
				continue
			}
			nfiles++
			var size int64
			if h != nil {
				size = h.fileSize(s, info, d)
			} else {
				size = diskSize(info)
			}
			if size > 0 {
				bytes += size
				list = append(list, File{e.Name(), size})
			}
		}
	}
	if err != nil && len(subs) == 0 && len(list) == 0 {
		denied = true
		s.nDenied.Add(1)
		if h != nil && h.privacyDenied(err) {
			s.nPrivacy.Add(1)
		}
	}
	slices.SortFunc(list, func(a, b File) int {
		switch {
		case a.Size > b.Size:
			return -1
		case a.Size < b.Size:
			return 1
		}
		return 0
	})

	// 累加也放在锁里：这样「移到回收站」「重新扫描」减掉旧数字时，不会有迟到的累加混进来
	s.mu.Lock()
	gone := d.removed
	if !gone {
		d.subs, d.list, d.denied = subs, list, denied
		s.nFiles.Add(nfiles)
		s.nDirs.Add(int64(len(subs)))
		for p := d; p != nil; p = p.parent {
			p.size.Add(bytes)
			p.files.Add(nfiles)
			p.dirs.Add(int64(len(subs)))
		}
	}
	s.mu.Unlock()
	if gone {
		j.finish(d)
		return
	}

	d.pending.Add(int32(len(pending)))
	j.push(pending)
	j.finish(d)
}

// finish 把 d 自己的那一份标记为完成；子树全部完成时一路往上通知，直到这次任务的起点。
func (j *job) finish(d *Dir) {
	for d != nil {
		if d.pending.Add(-1) > 0 {
			return
		}
		if d == j.top {
			// 先记下结束时间再通知，等待的人醒来时状态已经是「完成」
			j.s.finished.Store(time.Now().UnixNano())
			close(j.done)
			return
		}
		d = d.parent
	}
}

// ---------------------------------------------------------------- 查找和修改

// Find 按相对根目录的路径找文件夹，找不到返回 nil。调用方需要持有 s.mu 的读锁。
func (s *Scan) find(parts []string) *Dir {
	d := s.Root
	for _, p := range parts {
		var next *Dir
		for _, sub := range d.subs {
			if sub.Name == p {
				next = sub
				break
			}
		}
		if next == nil {
			return nil
		}
		d = next
	}
	return d
}

// RootName 是扫描起点的显示名（「C:」或者文件夹名）
func (s *Scan) RootName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Root.Name
}

// AbsPath 把相对根目录的路径拼成完整路径。
func (s *Scan) AbsPath(parts []string) string {
	return filepath.Join(append([]string{s.RootPath}, parts...)...)
}

// Lookup 找到 parts 指向的文件夹或文件。文件返回它所在的文件夹和文件序号。
func (s *Scan) lookup(parts []string) (dir *Dir, parent *Dir, fileIdx int) {
	if len(parts) == 0 {
		return s.Root, nil, -1
	}
	parent = s.find(parts[:len(parts)-1])
	if parent == nil {
		return nil, nil, -1
	}
	name := parts[len(parts)-1]
	for _, sub := range parent.subs {
		if sub.Name == name {
			return sub, parent, -1
		}
	}
	for i, f := range parent.list {
		if f.Name == name {
			return nil, parent, i
		}
	}
	return nil, nil, -1
}

// Info 返回 parts 指向的一项的名字、大小、文件数，以及是不是文件夹。
func (s *Scan) Info(parts []string) (name string, size, files int64, dir, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, parent, fi := s.lookup(parts)
	switch {
	case d != nil:
		return d.Name, d.size.Load(), d.files.Load(), true, true
	case parent != nil && fi >= 0:
		f := parent.list[fi]
		return f.Name, f.Size, 1, false, true
	}
	return "", 0, 0, false, false
}

// Remove 把已经从磁盘上删掉的文件或文件夹从树里摘掉，并从所有上级里减掉它的大小。
func (s *Scan) Remove(parts []string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, parent, fi := s.lookup(parts)
	switch {
	case d != nil && parent != nil:
		parent.subs = slices.DeleteFunc(parent.subs, func(x *Dir) bool { return x == d })
		markRemoved(d)
		s.subtract(parent, d.size.Load(), d.files.Load(), d.dirs.Load()+1)
		return true
	case parent != nil && fi >= 0:
		size := parent.list[fi].Size
		parent.list = slices.Delete(parent.list, fi, fi+1)
		s.subtract(parent, size, 1, 0)
		return true
	}
	return false
}

func (s *Scan) subtract(from *Dir, size, files, dirs int64) {
	s.nFiles.Add(-files)
	s.nDirs.Add(-dirs)
	for p := from; p != nil; p = p.parent {
		p.size.Add(-size)
		p.files.Add(-files)
		p.dirs.Add(-dirs)
	}
}

func markRemoved(d *Dir) {
	d.removed = true
	for _, sub := range d.subs {
		markRemoved(sub)
	}
}

// Rescan 重新扫描一个文件夹（比如在外面删了东西之后），用新结果替换旧的。
// 只能在没有其他任务时调用。
func (s *Scan) Rescan(parts []string) error {
	if s.Busy() {
		return errors.New("还在扫描，请等扫描结束")
	}
	s.mu.Lock()
	old := s.find(parts)
	if old == nil {
		s.mu.Unlock()
		return errors.New("找不到这个文件夹")
	}
	fresh := &Dir{Name: old.Name, parent: old.parent}
	if old.parent == nil {
		s.Root = fresh
	} else {
		for i, sub := range old.parent.subs {
			if sub == old {
				old.parent.subs[i] = fresh
			}
		}
		s.subtract(old.parent, old.size.Load(), old.files.Load(), old.dirs.Load())
	}
	if old.parent == nil {
		s.nFiles.Store(0)
		s.nDirs.Store(0)
	}
	markRemoved(old)
	s.mu.Unlock()
	s.nDenied.Store(0) // 重新扫描后只统计这次遇到的
	s.nPrivacy.Store(0)
	s.startJob(fresh, s.AbsPath(parts), workersFor())
	return nil
}

// ---------------------------------------------------------------- 进度

type Status struct {
	Root    string `json:"root"`
	State   string `json:"state"` // scanning / done / stopped
	Files   int64  `json:"files"`
	Dirs    int64  `json:"dirs"`
	Bytes   int64  `json:"bytes"`
	Denied  int64  `json:"denied"`
	Privacy int64  `json:"privacy"` // Denied 里被系统隐私保护拦下的（macOS 的「完全磁盘访问权限」）
	Elapsed int64  `json:"elapsed"` // 毫秒
	Current string `json:"current,omitempty"`
}

func (s *Scan) Status() Status {
	st := Status{
		Root:    s.RootPath,
		Files:   s.nFiles.Load(),
		Dirs:    s.nDirs.Load(),
		Denied:  s.nDenied.Load(),
		Privacy: s.nPrivacy.Load(),
	}
	s.mu.RLock()
	st.Bytes = s.Root.size.Load()
	s.mu.RUnlock()
	end := time.Now()
	if f := s.finished.Load(); f != 0 {
		end = time.Unix(0, f)
		st.State = "done"
		if s.stopped.Load() {
			st.State = "stopped"
		}
	} else {
		st.State = "scanning"
		if p := s.current.Load(); p != nil {
			st.Current = *p
		}
	}
	st.Elapsed = end.Sub(time.Unix(0, s.started.Load())).Milliseconds()
	return st
}
