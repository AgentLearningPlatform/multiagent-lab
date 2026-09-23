// M16 阶段一（REQ-127/128）数据链路单测：临时库灌入手工 KG，
// 验证统计卡 / 实体搜索 / claims 溯源 / 实体聚焦检索（hops 1~2 + 关系类型过滤 + 明细）。
// 聚焦路径不依赖 embedder/LLM，零网络。
package kb

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func seedKG(t *testing.T, st *store.Store, kbID string) {
	t.Helper()
	ents := []*store.KGEntity{
		{ID: "e1", KBID: kbID, DocID: "d1", Name: "编排引擎", Type: "concept", Description: "调度智能体的核心组件"},
		{ID: "e2", KBID: kbID, DocID: "d1", Name: "智能体", Type: "concept", Description: "具备推理能力的执行单元"},
		{ID: "e3", KBID: kbID, DocID: "d1", Name: "大模型", Type: "concept", Description: "推理能力来源"},
		{ID: "e4", KBID: kbID, DocID: "d2", Name: "Token", Type: "property", Description: "计费单位"},
	}
	rels := []*store.KGRelationship{
		{ID: "r1", KBID: kbID, DocID: "d1", Source: "编排引擎", Target: "智能体", Type: "调度"},
		{ID: "r2", KBID: kbID, DocID: "d1", Source: "智能体", Target: "大模型", Type: "依赖"},
		{ID: "r3", KBID: kbID, DocID: "d2", Source: "大模型", Target: "Token", Type: "消耗"},
	}
	claims := []*store.KGClaim{
		{ID: "c1", KBID: kbID, DocID: "d1", ChunkID: "ch1", Subject: "编排引擎", Text: "编排引擎的职责是调度智能体"},
		{ID: "c2", KBID: kbID, DocID: "d1", ChunkID: "ch2", Subject: "智能体", Text: "智能体的推理依赖大模型"},
		{ID: "c3", KBID: kbID, DocID: "d2", ChunkID: "ch3", Subject: "大模型", Text: "大模型的调用会消耗 Token"},
	}
	if err := st.ReplaceKGForDoc(kbID, "", ents, rels, claims); err != nil {
		t.Fatal(err)
	}
}

func TestKGStatsSearchClaims(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const kb = "kb1"
	seedKG(t, st, kb)

	gstats, err := st.KGStatsForKB(kb)
	if err != nil {
		t.Fatal(err)
	}
	if gstats.Entities != 4 || gstats.Relationships != 3 || gstats.Claims != 3 {
		t.Fatalf("stats counts = %+v", gstats)
	}
	if gstats.DocsTotal != 0 || gstats.DocsWithKG != 0 {
		// kb_doc 无行时 docs_total=0（本用例未插文档表），覆盖口径不 panic 即可
		t.Logf("docs_total=%d docs_with_kg=%d", gstats.DocsTotal, gstats.DocsWithKG)
	}
	ents, err := st.KGSearchEntities(kb, "编排", 10)
	if err != nil || len(ents) != 1 || ents[0].Name != "编排引擎" {
		t.Fatalf("search ents=%v err=%v", ents, err)
	}
	traces, err := st.KGClaimsWithChunks(kb, []string{"编排引擎"}, 10)
	if err != nil || len(traces) != 1 || traces[0].Text != "编排引擎的职责是调度智能体" {
		t.Fatalf("claims traces=%+v err=%v", traces, err)
	}
}

func TestGraphragQueryDetailEntityFocus(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const kb = "kb1"
	seedKG(t, st, kb)
	svc := &Service{Store: st} // 聚焦路径仅用 Store；Vector/embedder 不参与

	// 1 跳：编排引擎 → 智能体
	d1, err := svc.GraphragQueryDetail(context.Background(), &store.KnowledgeBase{ID: kb, Name: "测试库", TopK: 4},
		"", GraphragOpts{Entity: "编排引擎", Hops: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(d1.Entities) != 2 || len(d1.Relationships) != 1 {
		t.Fatalf("hop1 entities=%d rels=%d, want 2/1", len(d1.Entities), len(d1.Relationships))
	}

	// 2 跳：继续到大模型（BFS 两轮 = r1 + r2；Token 在第三轮不可达）
	d2, err := svc.GraphragQueryDetail(context.Background(), &store.KnowledgeBase{ID: kb, Name: "测试库", TopK: 4},
		"", GraphragOpts{Entity: "编排引擎", Hops: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(d2.Entities) != 3 || len(d2.Relationships) != 2 {
		t.Fatalf("hop2 entities=%d rels=%d, want 3/2", len(d2.Entities), len(d2.Relationships))
	}
	if len(d2.Claims) == 0 {
		t.Fatal("hop2 claims empty")
	}

	// 关系类型过滤：仅"调度" → 1 跳 1 边
	d3, err := svc.GraphragQueryDetail(context.Background(), &store.KnowledgeBase{ID: kb, Name: "测试库", TopK: 4},
		"", GraphragOpts{Entity: "编排引擎", Hops: 2, RelTypes: []string{"调度"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(d3.Relationships) != 1 || d3.Relationships[0].Type != "调度" {
		t.Fatalf("filtered rels=%+v", d3.Relationships)
	}

	// 不存在的实体 → errKGEmpty
	if _, err := svc.GraphragQueryDetail(context.Background(), &store.KnowledgeBase{ID: kb},
		"", GraphragOpts{Entity: "不存在"}); err == nil {
		t.Fatal("want errKGEmpty for missing entity")
	}
}
