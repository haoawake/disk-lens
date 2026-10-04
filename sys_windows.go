//go:build windows

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procSHFileOperationW = shell32.NewProc("SHFileOperationW")
)

// mainWindow 是程序窗口的句柄，弹出的对话框挂在它下面，不会跑到窗口后面去
var mainWindow atomic.Uintptr

// diskSize 是文件实际占用本机磁盘的大小。OneDrive 等「仅在线」的占位文件
// 只是个影子，不占空间，算 0。
func diskSize(info fs.FileInfo) int64 {
	if a, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		const cloudOnly = windows.FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS | windows.FILE_ATTRIBUTE_OFFLINE
		if a.FileAttributes&cloudOnly != 0 {
			return 0
		}
	}
	return info.Size()
}

// ---------------------------------------------------------------- 磁盘列表

// Drive 是起始页上的一个磁盘
type Drive struct {
	Path   string // "C:\"
	Name   string // "C:"
	Label  string // 卷标
	FS     string
	Kind   string // fixed / removable / network / cdrom
	Total  int64
	Free   int64
	Ready  bool
	System bool
}

func listDrives() []Drive {
	buf := make([]uint16, 256)
	n, err := windows.GetLogicalDriveStrings(uint32(len(buf)), &buf[0])
	if err != nil || n == 0 {
		return nil
	}
	sysDrive := os.Getenv("SystemDrive")
	var roots []string
	for i := 0; i < int(n); { // 缓冲区里是一串以 NUL 分隔的 "C:\"
		j := i
		for j < int(n) && buf[j] != 0 {
			j++
		}
		if j > i {
			roots = append(roots, windows.UTF16ToString(buf[i:j]))
		}
		i = j + 1
	}

	type result struct {
		i int
		d Drive
	}
	results := make(chan result, len(roots))
	drives := make([]Drive, len(roots))
	for i, root := range roots {
		drives[i] = Drive{Path: root, Name: strings.TrimSuffix(root, `\`), Kind: "fixed"}
		go func(i int, d Drive) {
			defer func() { results <- result{i, d} }()
			p := windows.StringToUTF16Ptr(d.Path)
			switch windows.GetDriveType(p) {
			case windows.DRIVE_REMOVABLE:
				d.Kind = "removable"
			case windows.DRIVE_REMOTE:
				d.Kind = "network"
			case windows.DRIVE_CDROM:
				d.Kind = "cdrom"
			}
			label := make([]uint16, 261)
			fsName := make([]uint16, 64)
			if windows.GetVolumeInformation(p, &label[0], uint32(len(label)), nil, nil, nil, &fsName[0], uint32(len(fsName))) != nil {
				return // 没插光盘的光驱、断开的网络盘
			}
			d.Label = windows.UTF16ToString(label)
			d.FS = windows.UTF16ToString(fsName)
			var avail, total, free uint64
			if windows.GetDiskFreeSpaceEx(p, &avail, &total, &free) == nil {
				d.Total, d.Free = int64(total), int64(free)
				d.Ready = true
			}
			d.System = strings.EqualFold(d.Name, sysDrive)
		}(i, drives[i])
	}
	// 断开的网络盘可能卡很久，最多等 3 秒，没回来的就显示成「未就绪」
	timeout := time.After(3 * time.Second)
	for range roots {
		select {
		case r := <-results:
			drives[r.i] = r.d
		case <-timeout:
			return drives
		}
	}
	return drives
}

// ---------------------------------------------------------------- 打开、定位、回收站

func shellExecute(verb, file, args string) error {
	var a *uint16
	if args != "" {
		a = windows.StringToUTF16Ptr(args)
	}
	return windows.ShellExecute(windows.Handle(mainWindow.Load()), windows.StringToUTF16Ptr(verb),
		windows.StringToUTF16Ptr(file), a, nil, windows.SW_SHOWNORMAL)
}

// shellOpen 用默认程序打开文件、文件夹或网址，相当于在资源管理器里双击
func shellOpen(target string) error { return shellExecute("open", target, "") }

// revealFile 打开资源管理器并选中这个文件或文件夹
func revealFile(path string) error {
	cmd := exec.Command("explorer.exe")
	// explorer 的参数格式特殊，必须是 /select,"路径"，不能让 Go 替它加引号
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + path + `"`}
	return cmd.Start()
}

// moveToTrash 把文件或文件夹移到回收站。回收站放不下时 Windows 会先问一句，不会悄悄彻底删除。
func moveToTrash(path string) error {
	from, err := windows.UTF16FromString(path)
	if err != nil {
		return err
	}
	from = append(from, 0) // 要以两个 \0 结尾
	op := struct {
		hwnd                  uintptr
		wFunc                 uint32
		pFrom                 *uint16
		pTo                   *uint16
		fFlags                uint16
		fAnyOperationsAborted int32
		hNameMappings         uintptr
		lpszProgressTitle     *uint16
	}{
		hwnd:  mainWindow.Load(),
		wFunc: 3, // FO_DELETE
		pFrom: &from[0],
		// FOF_ALLOWUNDO | FOF_NOCONFIRMATION | FOF_WANTNUKEWARNING：
		// 进回收站；我们自己的网页已经确认过了，不再问；但要彻底删除时一定提醒
		fFlags: 0x40 | 0x10 | 0x4000,
	}
	// 在界面线程上直接调用：Windows 会弹出自己的进度窗口，期间照样处理本窗口的消息
	ret, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if op.fAnyOperationsAborted != 0 {
		return errCancelled
	}
	if ret != 0 {
		if _, err := os.Lstat(path); err != nil {
			return nil // 返回码不一定可信，以实际是否还在为准
		}
		return fmt.Errorf("Windows 没能删除它（错误码 0x%X），可能有文件正在被使用", ret)
	}
	return nil
}

var errCancelled = errors.New("已取消")

// ---------------------------------------------------------------- 选择文件夹

// comObj 是一个 COM 接口指针，第一个字段指向虚函数表
type comObj struct{ vtbl *[32]uintptr }

func (o *comObj) call(method int, args ...uintptr) uint32 {
	r, _, _ := syscall.SyscallN(o.vtbl[method], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return uint32(r)
}

func (o *comObj) release() { o.call(2) }

// pickFolder 弹出系统的「选择文件夹」对话框，用户取消返回空字符串。
func pickFolder(title string) (string, error) {
	var (
		path string
		err  error
	)
	// 界面线程已经初始化过 COM（单线程套间），对话框必须在这个线程上弹，否则会和窗口互相等待
	func() {
		clsidFileOpenDialog := windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
		iidIFileOpenDialog := windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
		var dlg *comObj
		hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, 1, /* CLSCTX_INPROC_SERVER */
			uintptr(unsafe.Pointer(&iidIFileOpenDialog)), uintptr(unsafe.Pointer(&dlg)))
		if hr != 0 || dlg == nil {
			err = errors.New("没法打开选择文件夹的窗口")
			return
		}
		defer dlg.release()
		// IFileDialog 的方法序号：3 Show，9 SetOptions，10 GetOptions，17 SetTitle，20 GetResult
		var opts uint32
		dlg.call(10, uintptr(unsafe.Pointer(&opts)))
		const fosPickFolders, fosForceFileSystem, fosPathMustExist = 0x20, 0x40, 0x800
		dlg.call(9, uintptr(opts|fosPickFolders|fosForceFileSystem|fosPathMustExist))
		dlg.call(17, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(title))))
		if hr := dlg.call(3, mainWindow.Load()); hr != 0 {
			return // 0x800704C7 = 用户点了取消
		}
		var item *comObj
		if dlg.call(20, uintptr(unsafe.Pointer(&item))) != 0 || item == nil {
			return
		}
		defer item.release()
		// IShellItem::GetDisplayName(SIGDN_FILESYSPATH)
		var p *uint16
		if item.call(5, 0x80058000, uintptr(unsafe.Pointer(&p))) != 0 || p == nil {
			return
		}
		path = windows.UTF16PtrToString(p)
		windows.CoTaskMemFree(unsafe.Pointer(p))
	}()
	return path, err
}

// ---------------------------------------------------------------- 管理员

func isAdmin() bool { return windows.GetCurrentProcessToken().IsElevated() }

// enablePrivileges 以管理员身份运行时打开「备份」「还原」「取得所有权」特权。
// 有了它们，没有权限的文件夹（比如 System Volume Information）也能列出来统计大小，
// 永久删除时也能绕过文件本身的权限设置——和管理员在资源管理器里删东西一样。
func enablePrivileges() {
	var tok windows.Token
	if windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &tok) != nil {
		return
	}
	defer tok.Close()
	for _, name := range []string{"SeBackupPrivilege", "SeRestorePrivilege", "SeTakeOwnershipPrivilege"} {
		var luid windows.LUID
		if windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr(name), &luid) != nil {
			continue
		}
		tp := windows.Tokenprivileges{PrivilegeCount: 1}
		tp.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
		_ = windows.AdjustTokenPrivileges(tok, false, &tp, 0, nil, nil)
	}
}

// relaunchAsAdmin 以管理员身份重新启动自己，成功后当前进程应该退出。
func relaunchAsAdmin(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var quoted []string
	for _, a := range args {
		quoted = append(quoted, syscall.EscapeArg(a))
	}
	err = shellExecute("runas", exe, strings.Join(quoted, " "))
	if errors.Is(err, windows.ERROR_CANCELLED) {
		return errCancelled
	}
	return err
}

func showError(msg string) {
	_, _ = windows.MessageBox(windows.HWND(mainWindow.Load()), windows.StringToUTF16Ptr(msg),
		windows.StringToUTF16Ptr(appName), windows.MB_ICONERROR|windows.MB_OK)
}

func diskSpace(path string) (total, free int64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var avail, t, f uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &t, &f); err != nil {
		return 0, 0, err
	}
	return int64(t), int64(f), nil
}

// showProperties 打开资源管理器的「属性」窗口
func showProperties(path string) {
	const shopFilePath = 0x2
	pSHObjectProperties.Call(mainWindow.Load(), shopFilePath, uintptr(unsafe.Pointer(u16(path))), 0)
}
