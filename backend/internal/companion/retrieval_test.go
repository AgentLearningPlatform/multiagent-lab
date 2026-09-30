package companion

import (
	"strings"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// REQ-170 P2「KG 检索源并入」零依赖单测：标签匹配 / 上下文渲染 / SPARQL 生成。

func TestRecallEntities(t *testing.T) {
	labels := []string{"阿司匹林", "前列腺素", "ab", "布洛芬", "阿司匹林"}
	got := recallEntities(labels, "阿司匹林和布洛芬能一起吃吗？它如何抑制前列腺素？")
	// 保序去重（按标签清单序）、<2 字符标签跳过、上限内全命中
	want := []string{"阿司匹林", "前列腺素", "布洛芬"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("recallEntities = %v, want %v", got, want)
	}
	if got := recallEntities(labels, "今天天气如何"); len(got) != 0 {
		t.Fatalf("无命中应返回空，got %v", got)
	}
	// 反向包含：短输入（≥4 字符）被长标签包含
	if got := recallEntities([]string{"阿司匹林肠溶片"}, "阿司匹林"); len(got) != 1 {
		t.Fatalf("反向包含应命中 1 个，got %v", got)
	}
}

func TestRecallEntitiesLimit(t *testing.T) {
	labels := []string{"概念一", "概念二", "概念三", "概念四", "概念五", "概念六", "概念七"}
	got := recallEntities(labels, "概念一 概念二 概念三 概念四 概念五 概念六 概念七")
	if len(got) != maxRecallEntities {
		t.Fatalf("应截断到 %d 个，got %d", maxRecallEntities, len(got))
	}
}

func TestRenderCompanionContext(t *testing.T) {
	hits := []entityHit{
		{Label: "阿司匹林", Definition: "非甾体抗炎药", Edges: []edgeRow{
			{Rel: "抑制", Other: "前列腺素", Dir: "out"},
			{Rel: "属于", Other: "解热镇痛药", Dir: "in"},
		}},
		{Label: "布洛芬"},
	}
	text := renderCompanionContext(hits)
	for _, want := range []string{"伴生图检索", "阿司匹林", "非甾体抗炎药", "「抑制」→ 前列腺素", "解热镇痛药 →「属于」", "布洛芬"} {
		if !strings.Contains(text, want) {
			t.Fatalf("渲染缺 %q：%s", want, text)
		}
	}
	if renderCompanionContext(nil) != "" {
		t.Fatal("空命中应渲染为空")
	}
}

func TestSelectEntityContextSPARQL(t *testing.T) {
	info := SelectEntityInfo("c1", "阿司匹林")
	if !strings.Contains(info, "GRAPH <http://eino-lab/graph/ont-c1>") || !strings.Contains(info, "OPTIONAL") {
		t.Fatalf("SelectEntityInfo 图限定/OPTIONAL 缺失：%s", info)
	}
	edges := SelectEntityEdges("c1", "阿司匹林")
	for _, want := range []string{"bot:subject", "bot:object", "UNION", "FILTER NOT EXISTS", `BIND("out" AS ?dir)`} {
		if !strings.Contains(edges, want) {
			t.Fatalf("SelectEntityEdges 缺 %q：%s", want, edges)
		}
	}
}

func TestParseLabelValues(t *testing.T) {
	raw := []byte(`{"results":{"bindings":[{"label":{"value":"阿司匹林"}},{"label":{"value":"布洛芬"}}]}}`)
	if got := parseLabelValues(raw); len(got) != 2 || got[0] != "阿司匹林" {
		t.Fatalf("parseLabelValues = %v", got)
	}
}

// RetrievalContext 空值防御：nil 会话/空输入/未绑定本体均返回空（nil Service 由编译期保证不可调）。
func TestRetrievalContextGuards(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := NewService(st, nil, nil)
	conv := &store.Conversation{ID: "c1", Scope: "agent"}
	// REQ-216：未绑定伴生本体（companion_ontology_id 空）→ 不召回（空返回）
	agt := &store.Agent{ID: "c1"}
	if text, ents, err := s.RetrievalContext(t.Context(), conv, agt, "阿司匹林"); err != nil || text != "" || ents != nil {
		t.Fatalf("未绑定本体应空返回，got %q %v %v", text, ents, err)
	}
}
