// M36/KB-6 抽取管线升级单测：窗口化与预算、实体归并去重、本体词表 prompt 注入与截断、
// 两步归并的指代一致性丢弃。零 LLM/网络依赖（纯函数面）。
package kg

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func chunk(doc string, seq int, content string) *store.KnowledgeChunk {
	return &store.KnowledgeChunk{ID: store.NewID(), KBID: "kb1", DocID: doc, Seq: seq, Content: content}
}

func TestBuildWindowsBudgetAndSize(t *testing.T) {
	chunks := make([]*store.KnowledgeChunk, 0, 10)
	for i := 0; i < 10; i++ {
		chunks = append(chunks, chunk("d1", i, strings.Repeat("内容甲乙丙丁。", 20))) // ~168 chars each
	}
	wins, truncated := buildWindows(chunks, 10)
	if truncated {
		t.Fatalf("预算 10 chunks 不应截断")
	}
	if len(wins) != 3 { // 10 chunks / 4 per window → 3 windows (4+4+2)
		t.Fatalf("windows = %d, want 3（每窗 ≤4 chunks）", len(wins))
	}
	for i, w := range wins {
		if w.idx != i {
			t.Fatalf("window idx = %d, want %d", w.idx, i)
		}
		if n := strings.Count(w.text, "[doc:"); n > windowMaxChunks {
			t.Fatalf("window %d chunks = %d > %d", i, n, windowMaxChunks)
		}
	}
	// 预算截断：预算 5 → 只吃 5 chunks，truncated=true
	wins2, truncated2 := buildWindows(chunks, 5)
	if !truncated2 || windowCountAll(wins2) != 5 {
		t.Fatalf("预算 5：truncated=%v windows=%d chunks, want true/5", truncated2, windowCountAll(wins2))
	}
	// budgetFor：KB 级覆盖与默认
	if got := budgetFor(nil); got != defaultKGMaxChunks {
		t.Fatalf("budgetFor(nil) = %d", got)
	}
	if got := budgetFor(&store.KnowledgeBase{KGMaxChunks: 7}); got != 7 {
		t.Fatalf("budgetFor(KB) = %d, want 7", got)
	}
}

func windowCountAll(wins []extractWindow) int {
	n := 0
	for _, w := range wins {
		n += strings.Count(w.text, "[doc:")
	}
	return n
}

func TestEntityAccMergeAndDedupe(t *testing.T) {
	acc := newEntityAcc()
	acc.add("工作流引擎", "concept", "调度智能体执行")
	acc.add("工作流 引擎", "", "")    // 空白压缩后同名 → 合并
	acc.add("Workflow Engine", "", "") // 大小写不敏感键不同 → 新实体
	acc.add("工作流引擎", "concept", "第二条描述忽略")
	names := acc.names()
	if len(names) != 2 {
		t.Fatalf("names = %v, want 2（同义归并 + 独立实体）", names)
	}
	if names[0] != "工作流引擎" { // 频次 3 > 1，频次序第一
		t.Fatalf("names[0] = %s, want 工作流引擎（频次序）", names[0])
	}
	e := acc.byKey[mergeKey("工作流引擎")]
	if e.Description != "调度智能体执行" {
		t.Fatalf("description = %q（首个非空应保留）", e.Description)
	}
	if e.Type != "concept" {
		t.Fatalf("type = %q（首个非空应保留）", e.Type)
	}
}

func TestVocabPromptTruncation(t *testing.T) {
	if vocabPrompt(nil) != "" {
		t.Fatalf("nil vocab 应返回空串")
	}
	v := &OntoVocab{Name: "运维本体"}
	for i := 0; i < 200; i++ {
		v.Concepts = append(v.Concepts, fmt.Sprintf("概念%03d", i))
		v.Relations = append(v.Relations, fmt.Sprintf("关系%03d", i))
	}
	p := vocabPrompt(v)
	if !strings.Contains(p, "运维本体") || !strings.Contains(p, "概念词表") {
		t.Fatalf("prompt 缺少本体名/词表段")
	}
	if !strings.Contains(p, "关系059") || strings.Contains(p, "关系060") {
		t.Fatalf("关系词表未截断至 60：含059=%v 含060=%v", strings.Contains(p, "关系059"), strings.Contains(p, "关系060"))
	}
	if !strings.Contains(p, "概念119") || strings.Contains(p, "概念120") {
		t.Fatalf("概念词表未截断至 120")
	}
}

func TestVocabPromptNoEmpty(t *testing.T) {
	v := &OntoVocab{Name: "空本体"}
	if vocabPrompt(v) != "" {
		t.Fatalf("空词表应返回空串（不注入无效约束段）")
	}
}
