package kb

import (
	"regexp"
	"strings"
	"time"
)

// KB-10②（M35/37 号方案）切分升级：md 标题层级感知 + 父子分块（子块检索、父块召回上下文）。
//   - md（#~###）切节：每节 = 父块（含标题行）；节内超 chunkSize 再按固定窗切子块；
//   - 非结构化文本：固定窗切子块，连续窗口聚合为 ~parentSize 的父块（small-to-big）；
//   - 短文本：单块自父（Parent 空 = 无独立父块，excerpt 用自身）。
// 父块内容冗余存子块行（knowledge_chunk.parent_content，学习尺度存储代价可忽略），检索/索引只见子块。

const (
	chunkSize    = 500  // 子块上限（rune 计，历史默认不变）
	chunkOverlap = 50   // 子块重叠
	parentSize   = 1200 // 父块聚合目标规模
)

// embedTimeout embedding API 超时（索引/检索均同步，超时给足）。
const embedTimeout = 60 * time.Second

var headingRe = regexp.MustCompile(`(?m)^#{1,3}\s+\S`)

// Piece 切分产物：Content=子块（检索单元），Parent=所属父块内容（空=自身即独立块）。
type Piece struct {
	Content string
	Parent  string
}

// SplitText 固定窗口 + 重叠切分（rune 级，中文友好）——历史签名保留（KG 管线与评测基准在用）。
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

// SplitPieces KB-10② 结构感知切分入口（导入/重建索引用；SplitText 为其子块切分原语）。
func SplitPieces(text string) []Piece {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len([]rune(text)) <= chunkSize {
		return []Piece{{Content: text}}
	}
	if sections := splitMDSections(text); len(sections) > 0 {
		var out []Piece
		for _, sec := range sections {
			children := SplitText(sec)
			if len(children) <= 1 { // 节本身 ≤ chunkSize：单块自父（标题信息已在内容里）
				out = append(out, Piece{Content: sec})
				continue
			}
			parent := sec
			for _, c := range children {
				out = append(out, Piece{Content: c, Parent: parent})
			}
		}
		return out
	}
	// 非结构化：固定窗子块 + 连续聚合父块
	children := SplitText(text)
	var out []Piece
	group := []string{}
	glue := 0
	flush := func() {
		if len(group) == 0 {
			return
		}
		parent := strings.TrimSpace(strings.Join(group, "\n"))
		for _, c := range group {
			out = append(out, Piece{Content: c, Parent: parent})
		}
		group = group[:0]
		glue = 0
	}
	for _, c := range children {
		group = append(group, c)
		glue += len([]rune(c))
		if glue >= parentSize {
			flush()
		}
	}
	flush()
	return out
}

// splitMDSections 按 1~3 级标题切节（节含标题行；首节=首个标题前的内容，空则略）；
// 无标题返回 nil（调用方走非结构化路径）。
func splitMDSections(text string) []string {
	if !headingRe.MatchString(text) {
		return nil
	}
	lines := strings.Split(text, "\n")
	var sections []string
	cur := []string{}
	for _, ln := range lines {
		if headingRe.MatchString(ln) && len(cur) > 0 {
			if sec := strings.TrimSpace(strings.Join(cur, "\n")); sec != "" {
				sections = append(sections, sec)
			}
			cur = []string{}
		}
		cur = append(cur, ln)
	}
	if sec := strings.TrimSpace(strings.Join(cur, "\n")); sec != "" {
		sections = append(sections, sec)
	}
	return sections
}
