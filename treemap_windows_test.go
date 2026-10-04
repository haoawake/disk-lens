//go:build windows

package main

import "testing"

func TestSquarifyFillsArea(t *testing.T) {
	items := []sqItem{{size: 60}, {size: 25}, {size: 10}, {size: 4}, {size: 1}}
	squarify(items, 0, 0, 300, 200)
	var area float64
	for _, it := range items {
		if it.w <= 0 || it.h <= 0 {
			t.Fatalf("empty tile: %+v", it)
		}
		if it.x < -1e-6 || it.y < -1e-6 || it.x+it.w > 300+1e-6 || it.y+it.h > 200+1e-6 {
			t.Fatalf("tile out of bounds: %+v", it)
		}
		area += it.w * it.h
	}
	if area < 300*200-1e-3 || area > 300*200+1e-3 {
		t.Errorf("tiles cover %.2f, want %d", area, 300*200)
	}
	// 面积要和大小成正比
	if r := items[0].w * items[0].h / (300 * 200); r < 0.59 || r > 0.61 {
		t.Errorf("first tile share = %.3f, want 0.6", r)
	}
}
