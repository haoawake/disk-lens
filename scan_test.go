package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeTree 按 "路径=字节数" 建一棵测试用的目录树，以 / 结尾的是空文件夹
func makeTree(t *testing.T, spec ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, s := range spec {
		if strings.HasSuffix(s, "/") {
			if err := os.MkdirAll(filepath.Join(root, s), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		name, size, _ := strings.Cut(s, "=")
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, c := range size {
			n = n*10 + int(c-'0')
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// sizeOf 是这些文件按当前平台的算法算出的大小之和（macOS 上按实际占用的磁盘块算，Windows、Linux 上就是文件长度）
func sizeOf(t *testing.T, root string, names ...string) int64 {
	t.Helper()
	var n int64
	for _, name := range names {
		fi, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		n += diskSize(fi)
	}
	return n
}

func scanDone(t *testing.T, root string) *Scan {
	t.Helper()
	s, err := NewScan(root)
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	return s
}

func TestScanSizes(t *testing.T) {
	root := makeTree(t,
		"a/x.bin=1000", "a/y.bin=500",
		"a/deep/z.mp4=3000",
		"b/c.txt=200",
		"empty/",
		"top.dat=7",
		"zero.txt=0",
	)
	s := scanDone(t, root)
	st := s.Status()
	if st.State != "done" {
		t.Fatalf("state = %s", st.State)
	}
	if want := sizeOf(t, root, "a/x.bin", "a/y.bin", "a/deep/z.mp4", "b/c.txt", "top.dat", "zero.txt"); st.Bytes != want {
		t.Errorf("total = %d, want %d", st.Bytes, want)
	}
	if st.Files != 6 { // 0 字节的文件也算一个
		t.Errorf("files = %d, want 6", st.Files)
	}
	if st.Dirs != 4 {
		t.Errorf("dirs = %d, want 4", st.Dirs)
	}
	name, size, files, dir, ok := s.Info([]string{"a"})
	if !ok || !dir || name != "a" || size != sizeOf(t, root, "a/x.bin", "a/y.bin", "a/deep/z.mp4") || files != 3 {
		t.Errorf("Info(a) = %q %d %d %v %v", name, size, files, dir, ok)
	}
	if _, size, _, dir, ok := s.Info([]string{"a", "deep", "z.mp4"}); !ok || dir || size != sizeOf(t, root, "a/deep/z.mp4") {
		t.Errorf("Info(z.mp4) = %d %v %v", size, dir, ok)
	}
	if !s.Root.Done() {
		t.Error("root not marked done")
	}
}

func TestViewOrderAndPruning(t *testing.T) {
	// 大小取得比磁盘块大得多，这样 macOS 上按磁盘块算时顺序也不变
	spec := []string{"big.bin=1000000", "mid/a=200000", "mid/b=100000"}
	var names []string
	for i := 0; i < 50; i++ {
		n := "small/f" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		spec = append(spec, n+"=10")
		names = append(names, n)
	}
	root := makeTree(t, spec...)
	s := scanDone(t, root)

	total := sizeOf(t, root, append(names, "big.bin", "mid/a", "mid/b")...)
	// 小文件每个画出来的面积大约是 它的大小 / 总大小 × 整个画面 × 0.8；门槛取它的两倍
	minArea := 2 * float64(sizeOf(t, root, names[0])) / float64(total) * 400 * 300
	v, at, list := s.View(nil, ViewOpts{W: 400, H: 300, MinArea: minArea}, 1000)
	if len(at) != 0 {
		t.Fatalf("at = %v", at)
	}
	if v.S != total {
		t.Fatalf("root size = %d, want %d", v.S, total)
	}
	if len(v.C) == 0 || v.C[0].N != "big.bin" || v.C[1].N != "mid" {
		t.Fatalf("children not sorted by size: %+v", v.C)
	}
	// small 里每个文件都小于门槛，应该合并成「较小的项目」
	for _, c := range v.C {
		if c.N == "small" && len(c.C) != 0 {
			t.Errorf("small should not be expanded into %d tiles", len(c.C))
		}
	}
	if len(list) != 3 {
		t.Errorf("list has %d items, want 3", len(list))
	}

	// 不存在的路径退回到最近的上级
	_, at, _ = s.View([]string{"mid", "nope"}, ViewOpts{W: 400, H: 300}, 10)
	if len(at) != 1 || at[0] != "mid" {
		t.Errorf("fallback at = %v", at)
	}
}

func TestRemoveAndRescan(t *testing.T) {
	root := makeTree(t, "keep/a=100", "gone/b=300", "gone/c/d=50", "file.txt=40")
	s := scanDone(t, root)
	keepA, file := sizeOf(t, root, "keep/a"), sizeOf(t, root, "file.txt")
	if got, want := s.Status().Bytes, keepA+file+sizeOf(t, root, "gone/b", "gone/c/d"); got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}

	if err := os.RemoveAll(filepath.Join(root, "gone")); err != nil {
		t.Fatal(err)
	}
	if !s.Remove([]string{"gone"}) {
		t.Fatal("Remove(gone) = false")
	}
	if st := s.Status(); st.Bytes != keepA+file || st.Files != 2 || st.Dirs != 1 {
		t.Errorf("after remove: %d bytes, %d files, %d dirs", st.Bytes, st.Files, st.Dirs)
	}
	if !s.Remove([]string{"file.txt"}) || s.Status().Bytes != keepA {
		t.Errorf("remove file: total = %d", s.Status().Bytes)
	}

	// 在外面加了东西，重新扫描这个文件夹
	if err := os.WriteFile(filepath.Join(root, "keep", "new"), make([]byte, 900), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Rescan([]string{"keep"}); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	keep := sizeOf(t, root, "keep/a", "keep/new")
	if st := s.Status(); st.Bytes != keep || st.Files != 2 {
		t.Errorf("after rescan: %d bytes, %d files", st.Bytes, st.Files)
	}
	if _, size, _, _, _ := s.Info([]string{"keep"}); size != keep {
		t.Errorf("keep = %d", size)
	}
}

func TestFormat(t *testing.T) {
	cases := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.00 KB", 1536: "1.50 KB", 15 << 20: "15.0 MB", 300 << 30: "300 GB"}
	for n, want := range cases {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
	if got := formatCount(1636235); got != "1,636,235" {
		t.Errorf("formatCount = %q", got)
	}
	if got := formatCount(999); got != "999" {
		t.Errorf("formatCount = %q", got)
	}
}
