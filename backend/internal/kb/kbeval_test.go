//go:build kbeval

// KB-14（M35/37 号方案）：检索评测基准跑分器（build tag 手动跑，沿 M34④ 先例）。
//
//	跑法：cd backend && go test -tags kbeval ./internal/kb/ -run TestKBEval -v
//
// 三臂口径：①keyword=词法臂（chunk_fts FTS5 BM25，Recall@5，离线确定性）；
// ②multihop=图臂（KGNeighborsMultiHop 可达边；种子图直写——测检索管线而非抽取质量，
// 抽取质量基准随 KB-6 交付后补）；③community=全局臂（社区摘要 2-gram 评分命中）。
// 向量臂/混合臂需真实 embedding 连接，未配置时如实跳过（诚实边界：不伪造向量分数）。
package kb_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb/evaldata"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kg"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func TestKBEval(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "eval.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.DB.Close()
	const kbID, docID = "kbeval", "d1"
	if _, err := st.CreateKnowledgeBase(&store.KnowledgeBase{ID: kbID, Name: "评测基准库", Mode: "graphrag"}); err != nil {
		t.Fatalf("create kb: %v", err)
	}
	// 语料切分直写（绕开 embedding；词法臂经 InsertKnowledgeChunks 钩子自动入 FTS）
	pieces := kb.SplitText(evaldata.Corpus)
	if len(pieces) < 2 {
		t.Fatalf("语料切分应 ≥2 chunks, got %d", len(pieces))
	}
	chunks := make([]*store.KnowledgeChunk, 0, len(pieces))
	for i, p := range pieces {
		chunks = append(chunks, &store.KnowledgeChunk{ID: "c" + string(rune('a'+i)), KBID: kbID, DocID: docID, Seq: i, Content: p})
	}
	if err := st.InsertKnowledgeChunks(chunks); err != nil {
		t.Fatalf("insert chunks: %v", err)
	}
	// 种子图直写（对应 corpus.md 依赖链与缺陷记录）
	ents := []*store.KGEntity{
		{ID: "e1", KBID: kbID, DocID: docID, Name: "Eino框架", Type: "concept"},
		{ID: "e2", KBID: kbID, DocID: docID, Name: "eino-ext组件", Type: "concept"},
		{ID: "e3", KBID: kbID, DocID: docID, Name: "Qdrant服务", Type: "concept"},
		{ID: "e4", KBID: kbID, DocID: docID, Name: "HNSW索引", Type: "concept"},
		{ID: "e5", KBID: kbID, DocID: docID, Name: "BUG-1024", Type: "defect"},
	}
	rels := []*store.KGRelationship{
		{ID: "r1", KBID: kbID, DocID: docID, Source: "Eino框架", Target: "eino-ext组件", Type: "封装"},
		{ID: "r2", KBID: kbID, DocID: docID, Source: "eino-ext组件", Target: "Qdrant服务", Type: "连接"},
		{ID: "r3", KBID: kbID, DocID: docID, Source: "Qdrant服务", Target: "HNSW索引", Type: "内置"},
		{ID: "r4", KBID: kbID, DocID: docID, Source: "BUG-1024", Target: "Qdrant服务", Type: "涉及"},
	}
	if err := st.ReplaceKGForDoc(kbID, docID, ents, rels, nil); err != nil {
		t.Fatalf("seed kg: %v", err)
	}
	// 社区（全局臂；无 LLM → 骨架摘要）
	sum := &kg.Summarizer{Store: st}
	if _, _, err := sum.BuildKGCommunities(t.Context(), kbID); err != nil {
		t.Fatalf("build communities: %v", err)
	}
	comms, err := st.ListKGCommunities(kbID)
	if err != nil {
		t.Fatalf("list communities: %v", err)
	}

	questions, err := evaldata.Questions()
	if err != nil {
		t.Fatalf("parse questions: %v", err)
	}
	const topK = 5
	var kwTotal, kwHit, mhTotal, mhHit, cmTotal, cmHit int
	t.Logf("======== KB-14 评测基准（语料 %d chunks / 图 %d 边 / 社区 %d 个）========", len(chunks), len(rels), len(comms))
	for _, q := range questions {
		switch q.Type {
		case "keyword":
			kwTotal++
			hits, err := st.SearchChunksFTS(kbID, store.FTSMatchQuery(q.Q), topK)
			if err != nil {
				t.Fatalf("[%s] fts: %v", q.ID, err)
			}
			hay := ""
			for _, h := range hits {
				hay += h.Content + "\n"
			}
			hit, miss := 0, []string{}
			for _, g := range q.Gold {
				if strings.Contains(hay, g) {
					hit++
				} else {
					miss = append(miss, g)
				}
			}
			kwHit += hit
			t.Logf("[词法] %-6s R@%d=%.2f  miss=%v", q.ID, topK, float64(hit)/float64(len(q.Gold)), miss)
		case "multihop":
			mhTotal++
			edgeHit, miss := 0, []string{}
			reached, err := st.KGNeighborsMultiHop(kbID, []string{q.Seed}, q.Hops)
			if err != nil {
				t.Fatalf("[%s] multihop: %v", q.ID, err)
			}
			have := map[string]bool{}
			for _, r := range reached {
				have[r.Source+"|"+r.Type+"|"+r.Target] = true
			}
			for _, e := range q.GoldEdges {
				if have[e[0]+"|"+e[1]+"|"+e[2]] {
					edgeHit++
				} else {
					miss = append(miss, e[0]+"-"+e[1]+"->"+e[2])
				}
			}
			mhHit += edgeHit
			t.Logf("[图跳] %-6s hops=%d R=%.2f  miss=%v", q.ID, q.Hops, float64(edgeHit)/float64(len(q.GoldEdges)), miss)
		case "community":
			cmTotal++
			hits := kg.GlobalSearch(kbID, q.Q, comms)
			if len(hits) > 0 {
				cmHit++
				t.Logf("[全局] %-6s 命中 %d 社区（top=%s score=%d）", q.ID, len(hits), hits[0].Label, hits[0].Score)
			} else {
				t.Logf("[全局] %-6s 未命中", q.ID)
			}
		}
	}
	// 向量臂/混合臂诚实边界：需真实 embedding 连接，离线不伪造分数。
	t.Logf("[向量] 跳过——需 KB_EVAL_EMBED 环境与已配置 embedding 连接（混合检索质量随真机冒烟量化）")
	t.Logf("======== 汇总：词法 R@%d=%.2f（%d/%d）  图跳=%.2f（%d/%d）  全局命中=%.2f（%d/%d）========",
		topK, div(kwHit, kwTotal), kwHit, kwTotal,
		div(mhHit, goldSum(questions)), mhHit, goldSum(questions),
		div(cmHit, cmTotal), cmHit, cmTotal)
	// 验收门（种子库确定性语料：应全中）
	if kwHit != kwGoldTotal(questions) {
		t.Errorf("词法臂未满分：kwHit=%d want %d（语料为确定性种子，未中即回归）", kwHit, kwGoldTotal(questions))
	}
	if mhHit != goldSum(questions) {
		t.Errorf("图臂未满分：mhHit=%d want %d（种子图 gold 边应全部可达）", mhHit, goldSum(questions))
	}
	if cmHit != cmTotal {
		t.Errorf("全局臂未全命中：cmHit=%d want %d", cmHit, cmTotal)
	}
}

func div(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func kwGoldTotal(qs []evaldata.Question) int {
	n := 0
	for _, q := range qs {
		if q.Type == "keyword" {
			n += len(q.Gold)
		}
	}
	return n
}

func goldSum(qs []evaldata.Question) int {
	n := 0
	for _, q := range qs {
		if q.Type == "multihop" {
			n += len(q.GoldEdges)
		}
	}
	return n
}
