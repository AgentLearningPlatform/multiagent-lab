// M36/B1 引用溯源单测：MatchSpans（CJK 2-gram/拉丁词/区间合并/上限）、ClipSpans 截断、
// ClaimExcerpt 居中窗口与区间换算。纯函数零依赖。
package kb

import (
	"strings"
	"testing"
)

func TestMatchSpansCJK(t *testing.T) {
	content := "工作流引擎负责调度智能体执行，智能体依赖大模型推理。"
	spans := MatchSpans(content, "智能体依赖")
	if len(spans) == 0 {
		t.Fatalf("应命中「智能体」「能体依」「体依赖」等 2-gram 并合并")
	}
	cr := []rune(content)
	for _, s := range spans {
		if s.Start < 0 || s.End > len(cr) || s.Start >= s.End {
			t.Fatalf("区间越界: %+v", s)
		}
	}
	// 合并后区间应覆盖「智能体依赖」连续段
	joined := string(cr[spans[0].Start:spans[len(spans)-1].End])
	for _, want := range []string{"智能体", "依赖"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("合并区间应覆盖命中词 %q: %q", want, joined)
		}
	}
}

func TestMatchSpansLatinCaseInsensitive(t *testing.T) {
	content := "The WorkflowEngine schedules agents."
	spans := MatchSpans(content, "workflowengine")
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	cr := []rune(content)
	if got := string(cr[spans[0].Start:spans[0].End]); got != "WorkflowEngine" {
		t.Fatalf("span text = %q", got)
	}
}

func TestMatchSpansCapAndNone(t *testing.T) {
	if spans := MatchSpans("", "查询"); spans != nil {
		t.Fatalf("空 content 应 nil")
	}
	if spans := MatchSpans("内容", ""); spans != nil {
		t.Fatalf("空 query 应 nil")
	}
	if spans := MatchSpans("这段内容毫无命中词", "不存在词语"); spans != nil {
		t.Fatalf("无命中应 nil, got %v", spans)
	}
	// 散落命中 > 3 段 → 截到 3
	content := "甲乙XX丙丁XX戊己XX庚辛XX壬癸"
	if spans := MatchSpans(content, "甲乙丙丁戊己庚辛壬癸"); len(spans) > 3 {
		t.Fatalf("spans = %d, ≤3", len(spans))
	}
}

func TestClipSpans(t *testing.T) {
	spans := []Span{{Start: 5, End: 10}, {Start: 50, End: 60}}
	got := ClipSpans(spans, 20)
	if len(got) != 1 || got[0].End != 10 {
		t.Fatalf("ClipSpans = %v", got)
	}
	got2 := ClipSpans([]Span{{Start: 15, End: 25}}, 20)
	if len(got2) != 1 || got2[0].End != 20 {
		t.Fatalf("越界端裁剪 = %v", got2)
	}
}

func TestClaimExcerptCentered(t *testing.T) {
	var chunks []rune
	for i := 0; i < 100; i++ {
		chunks = append(chunks, '甲')
	}
	claim := []rune("关键事实陈述句")
	copy(chunks[80:80+len(claim)], claim)
	content := string(chunks)
	excerpt, spans := ClaimExcerpt(content, string(claim), 40)
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	er := []rune(excerpt)
	if got := string(er[spans[0].Start:spans[0].End]); got != string(claim) {
		t.Fatalf("高亮区间文本 = %q, want claim 原文", got)
	}
	if len(er) > 40 {
		t.Fatalf("excerpt = %d runes > 40", len(er))
	}
}

func TestClaimExcerptMissDegrades(t *testing.T) {
	// claim 被抽取改写、原文无 2-gram 交集 → 头部摘录 + 无区间（诚实降级）
	content := "甲乙丙丁戊己庚辛壬癸。"
	excerpt, spans := ClaimExcerpt(content, "子丑寅卯辰巳", 240)
	if spans != nil {
		t.Fatalf("未命中应无区间")
	}
	if excerpt == "" {
		t.Fatalf("应有头部摘录")
	}
}
