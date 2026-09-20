package kb

import (
	"strings"
	"time"
)

// embedTimeout embedding API 超时（索引/检索均同步，超时给足）。
const embedTimeout = 60 * time.Second

// chunkSize/chunkOverlap 切分参数（§6.9：固定 500/50，可配后续扩展）。
const (
	chunkSize    = 500 // rune 计
	chunkOverlap = 50
)

// SplitText 固定窗口 + 重叠切分（rune 级，中文友好）。
// 返回按顺序的片段（seq 即下标）。
func SplitText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	rs := []rune(text)
	if len(rs) <= chunkSize {
		return []string{text}
	}
	step := chunkSize - chunkOverlap
	var out []string
	for start := 0; start < len(rs); start += step {
		end := start + chunkSize
		if end > len(rs) {
			end = len(rs)
		}
		piece := strings.TrimSpace(string(rs[start:end]))
		if piece != "" {
			out = append(out, piece)
		}
		if end >= len(rs) {
			break
		}
	}
	return out
}
