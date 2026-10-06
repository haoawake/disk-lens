package main

import (
	"strings"
	"testing"
)

func TestMacProtection(t *testing.T) {
	home := "/Users/me"
	cases := []struct {
		path        string
		block, warn bool
	}{
		{"/", true, false},
		{"/Volumes/U盘", true, false},
		{"/Volumes/U盘/电影/a.mkv", false, false},
		{"/System", true, false},
		{"/System/Library/CoreServices", true, false},
		{"/System/Volumes/Data", true, false},
		{"/System/Volumes/Data/Users/me/Downloads/a.dmg", false, false},
		{"/System/Volumes/Data/Applications/Xcode.app", false, true},
		{"/usr", true, false},
		{"/usr/bin/git", true, false},
		{"/usr/local", true, false},
		{"/usr/local/Cellar/ffmpeg", false, true},
		{"/opt/homebrew", false, true},
		{"/bin", true, false},
		{"/sbin/mount", true, false},
		{"/private/var/db/uuidtext", true, false},
		{"/private/var/vm/sleepimage", true, false},
		{"/Library/Apple/System", true, false},
		{"/Applications", true, false},
		{"/applications", true, false}, // APFS 不区分大小写
		{"/Applications/Xcode.app", false, true},
		{"/Applications/Xcode.app/Contents/Developer", false, true},
		{"/Applications/Adobe", false, true},
		{"/Library", true, false},
		{"/Library/Caches", true, false},
		{"/Library/Caches/com.apple.x", false, false},
		{"/Library/Application Support/Foo", false, true},
		{"/Library/Application Support/com.apple.TCC", true, false},
		{"/Users", true, false},
		{"/Users/other", true, false},
		{"/Users/Shared", true, false},
		{"/Users/Shared/大文件.zip", false, false},
		{home, true, false},
		{home + "/Downloads", true, false},
		{home + "/Downloads/安装包.dmg", false, false},
		{home + "/Movies/旅行.mov", false, false},
		{home + "/Library", true, false},
		{home + "/Library/Caches", true, false},
		{home + "/Library/Caches/com.google.Chrome", false, false},
		{home + "/Library/Logs/DiagnosticReports", false, false},
		{home + "/Library/Application Support/MobileSync/Backup", false, true},
		{home + "/Library/Containers/com.tencent.xinWeChat", false, true},
		{home + "/Library/Mobile Documents/com~apple~CloudDocs/论文.pages", false, true},
		{home + "/Library/CloudStorage/OneDrive-个人", false, true},
		{home + "/.Trash", true, false},
		{home + "/.Trash/旧东西", false, false},
		{home + "/Pictures/Photos Library.photoslibrary", false, true},
		{home + "/Pictures/Photos Library.photoslibrary/originals", false, true},
		{home + "/go/pkg/mod", false, false},
		{"/private/var/folders/xy/abc/T/junk", false, true},
		{"/private/tmp/junk", false, false},
	}
	for _, c := range cases {
		block, warn := macProtection(c.path, home)
		if (block != "") != c.block || (warn != "") != c.warn {
			t.Errorf("macProtection(%s) = block %q, warn %q", c.path, block, warn)
		}
	}
	// 应用里面的文件要点名是哪个应用
	if _, warn := macProtection("/Applications/Xcode.app/Contents/Developer", home); !strings.Contains(warn, "「Xcode」") {
		t.Errorf("warning should name the app: %q", warn)
	}
}

func TestMountPolicy(t *testing.T) {
	const sys, data, vm, usb, img = 1, 2, 3, 4, 5
	v := sysVolumes{
		rootDev: sys, dataDev: data,
		mounts:    map[string]uint64{"/System/Volumes/VM": vm},
		firmlinks: []string{"Users", "Applications", "private", "Volumes", "usr/local"},
	}
	p := newMountPolicy("/", v)
	cases := []struct {
		dir       string
		parent    uint64
		dev       uint64
		wantEnter bool
	}{
		{"/Users", sys, data, true},                                           // 固件链接：系统宗卷 → 数据宗卷
		{"/usr/local", sys, data, true},                                       // 同上
		{"/Users/me", data, data, true},                                       // 同一个设备
		{"/Volumes/U盘", data, usb, false},                                     // 移动硬盘
		{"/Library/Developer/CoreSimulator/Volumes/iOS_18", data, img, false}, // 挂上的磁盘映像
		{"/dev", sys, 9, false},
		{"/System/Volumes/VM", sys, vm, true},             // 同一块磁盘上的系统宗卷
		{"/System/Cryptexes/OS", sys, vm, false},          // 别的位置出现的同一个设备，不进
		{"/System/Volumes/Data", sys, data, true},         // 数据宗卷本身要进，里面没被链接的东西也要算
		{"/System/Volumes/Data/Users", data, data, false}, // 已经从 /Users 数过了
		{"/system/volumes/data/private", data, data, false},
		{"/System/Volumes/Data/usr/local", data, data, false},
		{"/System/Volumes/Data/.Spotlight-V100", data, data, true},
	}
	for _, c := range cases {
		if got := p.enter(c.dir, c.parent, c.dev); got != c.wantEnter {
			t.Errorf("enter(%s) = %v, want %v", c.dir, got, c.wantEnter)
		}
	}
	// 直接扫数据宗卷时，里面的东西都要进
	if !newMountPolicy("/System/Volumes/Data", v).enter("/System/Volumes/Data/Users", data, data) {
		t.Error("scanning the data volume itself should enter Users")
	}
	// 系统宗卷和数据宗卷报同一个设备号时，也不能从 /System/Volumes/Data 再数一遍
	same := sysVolumes{rootDev: sys, mounts: map[string]uint64{}, firmlinks: v.firmlinks}
	ps := newMountPolicy("/", same)
	for _, dir := range []string{"/System/Volumes/Data/Users", "/System/Volumes/Data/Applications"} {
		if ps.enter(dir, sys, sys) {
			t.Errorf("same device: enter(%s) = true, want false", dir)
		}
	}
	if !ps.enter("/Users", sys, sys) || !ps.enter("/System/Volumes/Data", sys, sys) {
		t.Error("same device: /Users and the data volume itself should be entered")
	}
	// 扫个人文件夹时不受影响
	if newMountPolicy("/Users/me", v).enter("/Users/me/Volumes", data, usb) {
		t.Error("mount point inside home should be skipped")
	}
}

func TestParseFirmlinks(t *testing.T) {
	in := "/AppleInternal\tAppleInternal\n/Applications\tApplications\n\n/usr/local\tusr/local\n"
	got := parseFirmlinks(strings.NewReader(in))
	if strings.Join(got, ",") != "AppleInternal,Applications,usr/local" {
		t.Errorf("parseFirmlinks = %v", got)
	}
}
