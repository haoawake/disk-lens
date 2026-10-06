package main

// macOS 上哪些东西不能删、哪些要先提醒。和 Windows 版（delete_windows.go 的 protection）一个思路：
// 删了就开不了机、或者整个删掉肯定不对的位置直接拦下；软件、应用数据、云盘里的东西先提醒。
// 用的是 path 而不是 path/filepath，在哪个系统上都能测。APFS 默认不区分大小写，比较时也不区分。

import (
	"path"
	"strings"
)

// macProtection 判断 abs 能不能删。block 不为空时坚决不让删；warn 不为空时可以删但要提醒。
// home 是当前用户的个人文件夹（比如 /Users/me）。
func macProtection(abs, home string) (block, warn string) {
	p := path.Clean(abs)
	home = path.Clean(home)

	// 从数据宗卷的挂载点进来的（/System/Volumes/Data/Users/…），换算成平常看到的路径
	if strings.EqualFold(p, dataVolume) {
		return "这是 Mac 的数据宗卷，里面是你所有的文件和应用，不能整个删除。", ""
	}
	if isUnderFold(p, dataVolume) {
		p = p[len(dataVolume):]
	}

	is := func(base string) bool { return strings.EqualFold(p, base) }
	in := func(base string) bool { return isUnderFold(p, base) && !strings.EqualFold(p, base) }

	// 整个磁盘
	if p == "/" {
		return "不能删除整个磁盘。", ""
	}
	if parts := strings.Split(strings.Trim(p, "/"), "/"); len(parts) == 2 && strings.EqualFold(parts[0], "Volumes") {
		return "不能删除整个磁盘。想清空一个移动硬盘，可以在「磁盘工具」里把它抹掉。", ""
	}

	// macOS 系统本身：大部分受「系统完整性保护」，本来也删不掉
	switch {
	case is("/private/var/vm") || in("/private/var/vm"):
		return "这是 macOS 的虚拟内存和睡眠镜像文件，由系统自己管理，不能删除。", ""
	case is("/private/var/db") || in("/private/var/db"):
		return "这是 macOS 的系统数据库（登录、更新、安全设置等），删了系统会出问题。", ""
	case is("/Library/Application Support/com.apple.TCC") || in("/Library/Application Support/com.apple.TCC"):
		return "这是 macOS 记录隐私权限的地方，不能删除。", ""
	}
	if !(is("/usr/local") || in("/usr/local")) {
		for _, base := range []string{"/System", "/usr", "/bin", "/sbin", "/Library/Apple", "/dev", "/etc", "/private/etc", "/cores"} {
			if is(base) || in(base) {
				return "这是 macOS 的系统文件，删了 Mac 可能就开不了机了（大部分也受系统保护，删不掉）。", ""
			}
		}
	}

	// 只能删里面的东西，不能整个删掉的文件夹
	if is(path.Join(home, ".Trash")) {
		return "这是废纸篓本身。想清空废纸篓，请在程序坞里的废纸篓图标上点右键，选「清空废纸篓」。", ""
	}
	whole := []string{
		"/Applications", "/Library", "/Users", "/Users/Shared", "/private", "/private/var", "/private/tmp",
		"/tmp", "/var", "/opt", "/usr/local", "/Volumes", "/Library/Caches",
		home, path.Join(home, "Library"), path.Join(home, "Library/Caches"), path.Join(home, "Library/Mobile Documents"),
		path.Join(home, "Library/CloudStorage"), path.Join(home, "Applications"),
	}
	for _, name := range []string{"Desktop", "Documents", "Downloads", "Movies", "Music", "Pictures", "Public"} {
		whole = append(whole, path.Join(home, name))
	}
	for _, base := range whole {
		if is(base) {
			return "这是系统的重要文件夹，不能整个删除。可以进到里面删具体的内容。", ""
		}
	}
	if in("/Users") && strings.Count(strings.Trim(p, "/"), "/") == 1 && !isUnderFold(home, p) {
		return "这是另一个用户的个人文件夹，不能在这里删除。", ""
	}

	// 可以删，但要先说清楚
	if app, inside := appBundle(p); app != "" {
		name := strings.TrimSuffix(path.Base(app), path.Ext(app))
		if inside {
			return "", "这是应用「" + name + "」内部的文件，删掉它这个应用可能就打不开了。要卸载应用，请删除整个「" + path.Base(app) + "」。"
		}
		return "", "删除它就相当于卸载「" + name + "」。有些应用自带卸载程序（会一起清理后台服务、系统扩展），如果有，最好先用它卸载。"
	}
	if lib, inside := bundleWithExt(p, ".photoslibrary"); lib != "" {
		if inside {
			return "", "这是「照片」图库内部的文件，直接删除可能损坏整个图库。请在「照片」App 里删除不要的照片和视频。"
		}
		return "", "这是「照片」的图库，删除后里面所有的照片和视频都会丢失（除非开了 iCloud 照片）。"
	}
	switch {
	case in(path.Join(home, "Library/Mobile Documents")):
		return "", "这是 iCloud 云盘里的文件。删除后，你所有设备上的 iCloud 云盘里也会一起删掉。"
	case in(path.Join(home, "Library/CloudStorage")):
		return "", "这是云盘（比如 OneDrive、Dropbox、Google 云端硬盘）同步的文件夹，删除后云端的文件也会一起删掉。"
	case in(path.Join(home, "Library/Caches")) || in(path.Join(home, "Library/Logs")) || in("/Library/Caches") ||
		in("/Library/Logs") || in("/private/tmp") || in("/tmp") || in(path.Join(home, ".Trash")):
		// 缓存、日志、临时文件本来就是可以删的
		return "", ""
	case in(path.Join(home, "Library")):
		return "", "这是应用保存的设置和数据（Caches 里的缓存一般可以放心删）。删除后对应的应用可能需要重新登录或设置。"
	case in("/Applications"):
		return "", "这是安装在「应用程序」里的东西，直接删除可能让对应的应用打不开。"
	case in("/Library"):
		return "", "这是所有用户共用的应用数据、驱动和系统组件。除非你清楚它是干什么的，否则删除可能让应用或系统出问题。"
	case in("/usr/local") || in("/opt"):
		return "", "这是 Homebrew 等命令行工具安装的软件，删除后相关的命令就不能用了。卸载最好用 brew uninstall。"
	case in("/private/var/folders"):
		return "", "这是 macOS 的临时文件和缓存，一般会自动清理；正在运行的程序可能会因此出错。"
	case in("/private/var") || in("/var"):
		return "", "这是 macOS 的系统数据，删除可能让系统出问题。"
	}
	return "", ""
}

// appBundle 找 p 所在的（或者 p 本身就是的）.app 应用，inside 表示 p 在应用里面
func appBundle(p string) (app string, inside bool) {
	return bundleWithExt(p, ".app")
}

// bundleWithExt 找 p 路径上第一个以 ext 结尾的文件夹（比如 Xcode.app、照片图库.photoslibrary）
func bundleWithExt(p, ext string) (bundle string, inside bool) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, part := range parts {
		if len(part) > len(ext) && strings.EqualFold(part[len(part)-len(ext):], ext) {
			return "/" + strings.Join(parts[:i+1], "/"), i < len(parts)-1
		}
	}
	return "", false
}
