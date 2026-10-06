package main

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// printBenchTree 在 -bench 结束后按大小列出前几层里 1 GB 以上的文件夹，用来查哪里数多了或者数漏了。
// 设置环境变量 DISKLENS_BENCH_TREE=层数 才会打印。
func printBenchTree(s *Scan) {
	depth, _ := strconv.Atoi(os.Getenv("DISKLENS_BENCH_TREE"))
	if depth <= 0 {
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var walk func(d *Dir, prefix string, level int)
	walk = func(d *Dir, prefix string, level int) {
		subs := slices.Clone(d.subs)
		slices.SortFunc(subs, func(a, b *Dir) int { return cmp.Compare(b.Size(), a.Size()) })
		for _, c := range subs {
			if c.Size() < 1<<30 {
				break
			}
			fmt.Printf("%s%s%s  %s\n", strings.Repeat("  ", level), prefix, c.Name, humanSize(c.Size()))
			if level+1 < depth {
				walk(c, prefix+c.Name+"/", level+1)
			}
		}
	}
	walk(s.Root, strings.TrimSuffix(s.RootPath, "/")+"/", 0)
}
