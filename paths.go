package main

// 扫描树里的位置用相对扫描起点的一串名字表示（比如 ["Users", "me", "Downloads"]）。

func clonePath(p []string) []string { return append([]string(nil), p...) }

func samePath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasPrefix(p, prefix []string) bool {
	return len(p) >= len(prefix) && samePath(p[:len(prefix)], prefix)
}
