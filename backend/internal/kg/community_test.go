// M16 阶段二（REQ-130）社区检测与全局问答单测：label propagation 确定性、骨架摘要回退、
// 持久化替换、2-gram 全局检索评分。零 LLM/网络依赖。
package kg

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func TestDetectCommunities(t *testing.T) {
	// 两个簇：{编排引擎, 智能体, 大模型} 与 {Tom, Jerry}；孤立实体 Monolithic 自成一社区
	entities := []string{"编排引擎", "智能体", "大模型", "Tom", "Jerry", "Monolithic"}
	rels := [][2]string{
		{"编排引擎", "智能体"}, {"智能体", "大模型"},
		{"Tom", "Jerry"},
	}
	comms := DetectCommunities(entities, rels)
	if len(comms) != 3 {
		t.Fatalf("communities = %d, want 3（三大实体簇 + Tom/Jerry 簇 + 孤立实体）", len(comms))
	}
	// 最大簇应含三大实体（同 label）
	joined := strings.Join(comms[0].Members, ",")
	for _, want := range []string{"编排引擎", "智能体", "大模型"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("top community missing %s: %v", want, comms[0].Members)
		}
	}
	// 确定性：同图两次检测结果一致
	if again := DetectCommunities(entities, rels); len(again) != len(comms) || again[0].Label != comms[0].Label {
		t.Fatal("detection not deterministic")
	}
}

func TestBuildCommunitiesSkeletonAndSearch(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const kb = "kb1"
	ents := []*store.KGEntity{
		{ID: "e1", KBID: kb, DocID: "d1", Name: "编排引擎", Type: "concept"},
		{ID: "e2", KBID: kb, DocID: "d1", Name: "智能体", Type: "concept"},
		{ID: "e3", KBID: kb, DocID: "d1", Name: "大模型", Type: "concept"},
	}
	rels := []*store.KGRelationship{
		{ID: "r1", KBID: kb, DocID: "d1", Source: "编排引擎", Target: "智能体", Type: "调度"},
		{ID: "r2", KBID: kb, DocID: "d1", Source: "智能体", Target: "大模型", Type: "依赖"},
	}
	if err := st.ReplaceKGForDoc(kb, "", ents, rels, nil); err != nil {
		t.Fatal(err)
	}
	// 空库（无模型连接）→ LLM 失败回退骨架摘要
	sum := &Summarizer{Store: st}
	n, method, err := sum.BuildKGCommunities(context.Background(), kb)
	if err != nil {
		t.Fatal(err)
	}
	if method != "louvain" {
		t.Fatalf("detect method = %q, want louvain", method)
	}
	if n != 1 {
		t.Fatalf("communities = %d, want 1", n)
	}
	list, err := st.ListKGCommunities(kb)
	if err != nil || len(list) != 1 {
		t.Fatalf("list err=%v len=%d", err, len(list))
	}
	if list[0].Method != "skeleton" {
		t.Fatalf("method = %q, want skeleton (no LLM configured)", list[0].Method)
	}
	if !strings.Contains(list[0].Summary, "编排引擎") {
		t.Fatalf("skeleton summary = %q", list[0].Summary)
	}

	// 全局检索：命中「编排」相关查询；不相关查询无命中
	hits := GlobalSearch(kb, "编排引擎如何调度", list)
	if len(hits) == 0 {
		t.Fatalf("global search hits = %+v", hits)
	}
	if h := GlobalSearch(kb, "完全无关的查询词", list); len(h) != 0 {
		t.Fatalf("irrelevant query should miss, got %+v", h)
	}
}

func TestGlobalSearchGramTerms(t *testing.T) {
	terms := gramTerms("编排引擎 调度")
	// 空格分词 + 2-gram：编排/排引/引擎/调度 + 整词
	for _, want := range []string{"编排", "引擎", "调度"} {
		found := false
		for _, t := range terms {
			if strings.Contains(t, want) || want == t {
				found = true
			}
		}
		if !found {
			t.Fatalf("gram terms missing %s: %v", want, terms)
		}
	}
}

// KB-5：Louvain 社区检测——簇划分、单例归尾、确定性。
func TestDetectCommunitiesLouvain(t *testing.T) {
	entities := []string{"编排引擎", "智能体", "大模型", "Tom", "Jerry", "Monolithic"}
	rels := [][2]string{
		{"编排引擎", "智能体"}, {"智能体", "大模型"},
		{"Tom", "Jerry"},
	}
	comms := DetectCommunitiesLouvain(entities, rels)
	// 两个 ≥2 成员簇 + Monolithic 归 __tail__
	if len(comms) != 3 {
		t.Fatalf("communities = %d, want 3（两簇 + __tail__）", len(comms))
	}
	if comms[len(comms)-1].Label != "__tail__" {
		t.Fatalf("末位应为 __tail__: %v", comms[len(comms)-1].Label)
	}
	joined := strings.Join(comms[0].Members, ",")
	for _, want := range []string{"编排引擎", "智能体", "大模型"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("top community missing %s: %v", want, comms[0].Members)
		}
	}
	// 确定性：同图两次划分一致
	if again := DetectCommunitiesLouvain(entities, rels); len(again) != len(comms) || again[0].Label != comms[0].Label {
		t.Fatal("louvain not deterministic")
	}
	// 空图 → nil（调用方回退 lp）
	if got := DetectCommunitiesLouvain(nil, nil); got != nil {
		t.Fatalf("empty graph should return nil, got %v", got)
	}
}

// KB-5/D5：摘要按需生成+缓存——pending 社区经 GlobalAnswer 触发生成（无 LLM 回退骨架）并持久化；
// 生成失败（无 LLM）时 ok=false 诚实降级。
func TestGlobalAnswerLazySummaryCache(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const kb = "kb1"
	// 直接落两个 pending 社区（模拟大库 >12 场景）
	comms := []*store.KGCommunity{
		{KBID: kb, Label: "编排引擎", Summary: "", Method: "pending", Members: []string{"编排引擎", "智能体", "大模型"}},
		{KBID: kb, Label: "Tom", Summary: "", Method: "pending", Members: []string{"Tom", "Jerry"}},
	}
	if err := st.ReplaceKGCommunities(kb, comms); err != nil {
		t.Fatal(err)
	}
	sum := &Summarizer{Store: st}
	answer, hits, ok := sum.GlobalAnswer(context.Background(), kb, "", "编排引擎如何调度大模型", []*store.KGCommunity{}) // 空社区列表 → false
	if ok || answer != "" || len(hits) != 0 {
		t.Fatalf("空社区列表应 ok=false, got ok=%v", ok)
	}
	list, err := st.ListKGCommunities(kb)
	if err != nil {
		t.Fatal(err)
	}
	answer, hits, ok = sum.GlobalAnswer(context.Background(), kb, "", "编排引擎如何调度大模型", list)
	if ok {
		t.Fatalf("无 LLM 环境应生成失败 ok=false")
	}
	if len(hits) == 0 || !hits[0].Used {
		t.Fatalf("命中社区应带 used 标注: %+v", hits)
	}
	// 按需生成已持久化（骨架回退）
	list, err = st.ListKGCommunities(kb)
	if err != nil {
		t.Fatal(err)
	}
	byLabel := map[string]*store.KGCommunity{}
	for _, c := range list {
		byLabel[c.Label] = c
	}
	if byLabel["编排引擎"].Method != "skeleton" || byLabel["编排引擎"].Summary == "" {
		t.Fatalf("摘要应按需生成并缓存（skeleton）: %+v", byLabel["编排引擎"])
	}
}
