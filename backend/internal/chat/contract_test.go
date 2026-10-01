package chat

import (
	"strings"
	"testing"
)

// REQ-224/M52（51 号 W1）：tool.result 落库截断纯函数。
func TestTruncateToolResult(t *testing.T) {
	// 未超限原样
	short := "hello 世界"
	out, _ := truncateToolResult(short)
	if out != short {
		t.Fatalf("短内容应原样: %q", out)
	}
	// 超限：32KB 阈值，头 24KB+尾 8KB+标记
	long := strings.Repeat("汉", 40*1024) // 每个 3 字节 ≈ 120KB
	out, total := truncateToolResult(long)
	if total != len(long) {
		t.Fatalf("total=%d", total)
	}
	if len(out) >= len(long) {
		t.Fatalf("应被截断")
	}
	if !strings.Contains(out, "truncated") {
		t.Fatalf("应含截断标记")
	}
	// 头尾保留且 rune 完整（无乱码半字节：首尾 rune 解码后与原文一致）
	if !strings.HasPrefix(out, "汉") || !strings.HasSuffix(out, "汉") {
		t.Fatalf("头尾 rune 应完整: %q...", out[:6])
	}
}
