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
	if st.Bytes != 4707 {
		t.Errorf("total = %d, want 4707", st.Bytes)
	}
	if st.Files != 6 { // 0 字节的文件也算一个
		t.Errorf("files = %d, want 6", st.Files)
	}
	if st.Dirs != 4 {
		t.Errorf("dirs = %d, want 4", st.Dirs)
	}
	name, size, files, dir, ok := s.Info([]string{"a"})
	if !ok || !dir || name != "a" || size != 4500 || files != 3 {
		t.Errorf("Info(a) = %q %d %d %v %v", name, size, files, dir, ok)
	}
	if _, size, _, dir, ok := s.Info([]string{"a", "deep", "z.mp4"}); !ok || dir || size != 3000 {
		t.Errorf("Info(z.mp4) = %d %v %v", size, dir, ok)
	}
	if !s.Root.Done() {
		t.Error("root not marked done")
	}
}

func TestViewOrderAndPruning(t *testing.T) {
	spec := []string{"big.bin=100000", "mid/a=20000", "mid/b=10000"}
	for i := 0; i < 50; i++ {
		spec = append(spec, "small/f"+string(rune('a'+i%26))+string(rune('a'+i/26))+"=10")
	}
	root := makeTree(t, spec...)
	s := scanDone(t, root)

	v, at, list := s.View(nil, ViewOpts{W: 400, H: 300, MinArea: 30}, 1000)
	if len(at) != 0 {
		t.Fatalf("at = %v", at)
	}
	if v.S != 130500 {
		t.Fatalf("root size = %d", v.S)
	}
	if len(v.C) == 0 || v.C[0].N != "big.bin" || v.C[1].N != "mid" {
		t.Fatalf("children not sorted by size: %+v", v.C)
	}
	// small 里每个文件只有 10 字节，在 400×300 的图上远小于 30 平方像素，应该合并成「较小的项目」
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
	if got := s.Status().Bytes; got != 490 {
		t.Fatalf("total = %d", got)
	}

	if err := os.RemoveAll(filepath.Join(root, "gone")); err != nil {
		t.Fatal(err)
	}
	if !s.Remove([]string{"gone"}) {
		t.Fatal("Remove(gone) = false")
	}
	if st := s.Status(); st.Bytes != 140 || st.Files != 2 || st.Dirs != 1 {
		t.Errorf("after remove: %d bytes, %d files, %d dirs", st.Bytes, st.Files, st.Dirs)
	}
	if !s.Remove([]string{"file.txt"}) || s.Status().Bytes != 100 {
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
	if st := s.Status(); st.Bytes != 1000 || st.Files != 2 {
		t.Errorf("after rescan: %d bytes, %d files", st.Bytes, st.Files)
	}
	if _, size, _, _, _ := s.Info([]string{"keep"}); size != 1000 {
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
