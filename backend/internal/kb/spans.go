// B1 引用溯源强化（M36/37 号正式方案；36 号 B1 详设）：检索命中带 chunk 内偏移，
// 与 KG claims 溯源统一「可信可验证」产品面——不新增存储，命中区间在检索期确定性计算。
package kb

import (
	"sort"
	"strings"
	"unicode"
)

// Span chunk/excerpt 内匹配区间（rune 偏移，Start 含 / End 不含；前端 <mark> 高亮）。
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// MatchSpans 在 content 中定位 query 词项的命中区间并合并相邻（≤3 段，按出现序）。
// 词项 = 拉丁字母/数字连续串（大小写不敏感）+ CJK 2-gram（单字 CJK 查询整词命中）。
// 纯函数（单测覆盖）：确定性、零依赖，供向量臂/词法臂命中与 claim 出处共用。
func MatchSpans(content, query string) []Span {
	if content == "" || query == "" {
		return nil
	}
	cr := []rune(content)
	lower := strings.ToLower(string(cr))
	tokens := queryTokens(query)
	if len(tokens) == 0 {
		return nil
	}
	type iv struct{ s, e int }
	intervals := []iv{}
	for _, tok := range tokens {
		t := strings.ToLower(tok)
		from := 0
		for from <= len(lower)-len(t) {
			i := strings.Index(lower[from:], t)
			if i < 0 {
				break
			}
			start := from + i
			intervals = append(intervals, iv{start, start + len(t)})
			from = start + len(t)
		}
	}
	if len(intervals) == 0 {
		return nil
	}
	if len([]rune(lower)) != len(cr) { // ToLower 改变 rune 数的极端字符：偏移不可信，诚实放弃高亮
		return nil
	}
	// 按 byte 偏移建 rune 偏移换算表
	runeAt := make([]int, len(lower)+1)
	ri := 0
	for b := 0; b <= len(lower); b++ {
		runeAt[b] = ri
		if b < len(lower) {
			if lower[b]&0xC0 != 0x80 { // 非 UTF-8 续字节 = 新 rune 起点
				ri++
			}
		}
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].s != intervals[j].s {
			return intervals[i].s < intervals[j].s
		}
		return intervals[i].e < intervals[j].e
	}) // 按起点排序后合并（重叠或间隔 ≤1 rune 视为同段）
	merged := []iv{intervals[0]}
	for _, v := range intervals[1:] {
		last := &merged[len(merged)-1]
		if v.s <= last.e+1 {
			if v.e > last.e {
				last.e = v.e
			}
			continue
		}
		merged = append(merged, v)
	}
	spans := make([]Span, 0, 3)
	for _, m := range merged {
		if len(spans) >= 3 {
			break
		}
		spans = append(spans, Span{Start: runeAt[m.s], End: runeAt[m.e]})
	}
	return spans
}

// ClipSpans 把区间裁剪到 [0, maxRunes)（excerpt 截断后偏移对齐）。
func ClipSpans(spans []Span, maxRunes int) []Span {
	out := []Span{}
	for _, s := range spans {
		if s.Start >= maxRunes {
			continue
		}
		if s.End > maxRunes {
			s.End = maxRunes
		}
		out = append(out, s)
	}
	return out
}

// queryTokens 查询分词：拉丁词 + CJK 2-gram（CJK 连串长度 1 时取整字）。
func queryTokens(query string) []string {
	var out []string
	var latin strings.Builder
	var cjk []rune
	flushLatin := func() {
		if latin.Len() > 0 {
			out = append(out, latin.String())
			latin.Reset()
		}
	}
	flushCJK := func() {
		switch {
		case len(cjk) >= 2:
			for i := 0; i+1 < len(cjk); i++ {
				out = append(out, string(cjk[i:i+2]))
			}
		case len(cjk) == 1:
			out = append(out, string(cjk))
		}
		cjk = cjk[:0]
	}
	for _, r := range query {
		switch {
		case unicode.IsLetter(r) && r < 0x2E80, unicode.IsDigit(r): // 拉丁/数字
			flushCJK()
			latin.WriteRune(r)
		case r >= 0x2E80: // CJK 及扩展
			flushLatin()
			cjk = append(cjk, r)
		default:
			flushLatin()
			flushCJK()
		}
	}
	flushLatin()
	flushCJK()
	// 去重（短查询 2-gram 常重复）
	seen := map[string]bool{}
	uniq := out[:0]
	for _, t := range out {
		if !seen[t] {
			seen[t] = true
			uniq = append(uniq, t)
		}
	}
	return uniq
}

// ClaimExcerpt claim 出处摘录：以 claim 文本在 chunk 中的首处命中为中心取 ≤240 rune 窗口，
// 返回（摘录文本, 区间——相对摘录的 rune 偏移）。未命中时返回 chunk 头部摘录 + nil 区间。
// 纯函数（B1：句级溯源高亮不新增存储，检索期计算）。
func ClaimExcerpt(chunkContent, claimText string, window int) (string, []Span) {
	if window <= 0 {
		window = 240
	}
	cr := []rune(chunkContent)
	if len(cr) == 0 {
		return "", nil
	}
	spans := MatchSpans(chunkContent, claimText)
	if len(spans) == 0 {
		// claim 未原样命中（抽取改写过）：取头部窗口，无高亮区间（诚实降级）
		if len(cr) > window {
			return string(cr[:window]), nil
		}
		return string(cr), nil
	}
	center := (spans[0].Start + spans[0].End) / 2
	start := center - window/2
	if start < 0 {
		start = 0
	}
	end := start + window
	if end > len(cr) {
		end = len(cr)
		start = end - window
		if start < 0 {
			start = 0
		}
	}
	out := []Span{}
	for _, s := range spans {
		if s.Start >= end || s.End <= start {
			continue
		}
		lo, hi := s.Start-start, s.End-start
		if lo < 0 {
			lo = 0
		}
		if hi > end-start {
			hi = end - start
		}
		out = append(out, Span{Start: lo, End: hi})
	}
	return string(cr[start:end]), out
}
