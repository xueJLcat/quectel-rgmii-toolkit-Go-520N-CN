package main

import (
	"testing"
)

// ---------- 扫描结果 empty 判定纯函数 ----------
// 注:QSCAN 三模式在本固件不返回任何小区,已从接口移除,
// 原依赖 QSCAN 扫描解析器的用例随之删除;
// 邻区扫描的 handler 级 empty 语义用例见 page_network_neighbour_test.go。

// TestCellScanEmpty 覆盖 empty 判定纯函数:零小区为 true、有小区为 false;
// 字段缺失或类型断言失败按 0 个小区处理。
// 直接构造解析器产物形态的结果 map 验证判定逻辑,不依赖任何解析器。
func TestCellScanEmpty(t *testing.T) {
	empty := map[string]any{"nr5g_cells_parsed": []map[string]string{}, "lte_cells_parsed": []map[string]string{}}
	if !cellScanEmpty(empty) {
		t.Fatalf("cellScanEmpty(zero cells) = false, want true")
	}
	withCells := map[string]any{
		"nr5g_cells_parsed": []map[string]string{{"type": "NR5G", "freq": "428910", "pci": "277"}},
		"lte_cells_parsed":  []map[string]string{},
	}
	if cellScanEmpty(withCells) {
		t.Fatalf("cellScanEmpty(with cells) = true, want false")
	}
	if !cellScanEmpty(map[string]any{}) {
		t.Fatalf("cellScanEmpty(missing fields) = false, want true")
	}
	if !cellScanEmpty(map[string]any{"nr5g_cells_parsed": "bogus", "lte_cells_parsed": 42}) {
		t.Fatalf("cellScanEmpty(bad field types) = false, want true")
	}
}
