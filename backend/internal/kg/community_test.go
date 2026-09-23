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
	n, err := sum.BuildKGCommunities(context.Background(), kb)
	if err != nil {
		t.Fatal(err)
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
