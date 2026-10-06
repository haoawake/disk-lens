package main

// macOS 上扫描时进不进一个子文件夹。用的是 path 而不是 path/filepath，在哪个系统上都能测。
//
// 从 Catalina 起，「Macintosh HD」其实是同一个 APFS 容器里的好几个宗卷：
//   - 「/」是只读的系统宗卷；
//   - 用户的东西在数据宗卷上，它挂在 /System/Volumes/Data，同时通过「固件链接」（firmlink）
//     出现在 /Users、/Applications、/Library、/private 这些熟悉的位置；
//   - 另外还有 /System/Volumes/VM（虚拟内存）、Preboot（启动相关）、Update 等。
// 所以「挂载点一律不进」行不通（/Users 在另一个宗卷上），「什么都进」又会把数据宗卷数两遍。
// 规则是：子文件夹和上一级在同一个设备上就进；系统宗卷和数据宗卷之间（固件链接）可以来回进；
// /System/Volumes 下面那几个宗卷只从它们自己的挂载点进；其他换了设备的（移动硬盘、网络位置、
// 磁盘映像、/dev、autofs）一律不进。扫描范围包含 /System/Volumes/Data 时，跳过它里面那些
// 已经通过固件链接数过的文件夹，剩下的（比如 Spotlight 索引）照样统计。

import (
	"bufio"
	"io"
	"path"
	"strings"
)

type mountPolicy struct {
	rootDev, dataDev uint64            // 系统宗卷「/」和数据宗卷的设备号（0 表示没有）
	mounts           map[string]uint64 // /System/Volumes 下的宗卷：挂载点（小写）→ 设备号
	skip             map[string]bool   // 不进入的完整路径（小写）
}

// sysVolumes 描述这台 Mac 的宗卷布局，由 sys_darwin.go 读出来
type sysVolumes struct {
	rootDev, dataDev uint64
	mounts           map[string]uint64 // /System/Volumes/xxx → 设备号（不含 Data）
	firmlinks        []string          // 数据宗卷上被固件链接到别处的文件夹，相对数据宗卷，比如 "Users"
}

const dataVolume = "/System/Volumes/Data"

func newMountPolicy(root string, v sysVolumes) *mountPolicy {
	p := &mountPolicy{rootDev: v.rootDev, dataDev: v.dataDev, mounts: map[string]uint64{}, skip: map[string]bool{}}
	for m, dev := range v.mounts {
		p.mounts[strings.ToLower(path.Clean(m))] = dev
	}
	root = path.Clean(root)
	// 扫描范围包含数据宗卷的挂载点（扫「/」「/System」「/System/Volumes」）时，固件链接指向的
	// 那些文件夹已经从 /Users 等位置数过了。直接扫 /System/Volumes/Data 时就不用跳。
	// 不看 dataDev：有的 Mac 上（比如 GitHub 的 macOS 虚拟机）系统宗卷和数据宗卷报的设备号一样，
	// 那样数据宗卷会从 /System/Volumes/Data 进去再数一遍，「/」就多算出将近一倍
	if isUnderFold(dataVolume, root) && !strings.EqualFold(root, dataVolume) {
		for _, f := range v.firmlinks {
			p.skip[strings.ToLower(path.Join(dataVolume, f))] = true
		}
	}
	return p
}

// enter 判断要不要进入 dir（完整路径）；parentDev、dev 是上一级和它自己的设备号
func (p *mountPolicy) enter(dir string, parentDev, dev uint64) bool {
	lower := strings.ToLower(dir)
	if p.skip[lower] {
		return false
	}
	if dev == parentDev {
		return true
	}
	if p.rootDev != 0 && p.dataDev != 0 &&
		((parentDev == p.rootDev && dev == p.dataDev) || (parentDev == p.dataDev && dev == p.rootDev)) {
		return true // 固件链接
	}
	if d, ok := p.mounts[lower]; ok && d == dev {
		return true // /System/Volumes/VM 等，同一块磁盘上的系统宗卷
	}
	return false
}

// parseFirmlinks 读 /usr/share/firmlinks：每行「系统宗卷上的位置<Tab>数据宗卷上的相对路径」
func parseFirmlinks(r io.Reader) []string {
	var out []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		target := strings.TrimSpace(fields[len(fields)-1])
		if len(fields) < 2 || target == "" {
			continue
		}
		out = append(out, strings.Trim(target, "/"))
	}
	return out
}

// defaultFirmlinks 是读不到 /usr/share/firmlinks 时用的（macOS 12～15 上的内容）
var defaultFirmlinks = []string{
	"AppleInternal", "Applications", "Library", "System/Library/Caches", "System/Library/Assets",
	"System/Library/PreinstalledAssets", "System/Library/AssetsV2", "System/Library/PreinstalledAssetsV2",
	"System/Library/CoreServices/CoreTypes.bundle/Contents/Library", "System/Library/Speech",
	"Users", "Volumes", "cores", "opt", "private", "usr/local", "usr/libexec/cups", "usr/share/snmp",
}

// isUnderFold 表示 p 是 base 本身或者在 base 里面（不区分大小写，APFS 默认如此）
func isUnderFold(p, base string) bool {
	if base == "/" {
		return strings.HasPrefix(p, "/")
	}
	return strings.EqualFold(p, base) || (len(p) > len(base) && p[len(base)] == '/' && strings.EqualFold(p[:len(base)], base))
}
