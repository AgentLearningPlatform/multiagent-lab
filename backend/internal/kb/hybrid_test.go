package kb

import (
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// KB-10①：RRF 融合纯函数测试——排序、strategy 标注、topK 截断、空臂降级。
func TestFuseHybrid(t *testing.T) {
	vec := []Hit{
		{ChunkID: "a", DocID: "d1", Seq: 0, Content: "向量第一"},
		{ChunkID: "b", DocID: "d1", Seq: 1, Content: "向量第二"},
		{ChunkID: "c", DocID: "d2", Seq: 0, Content: "向量第三"},
	}
	lex := []store.FTSHit{
		{ChunkID: "b", DocID: "d1", Seq: 1, Content: "词法第一（与向量重叠）"},
		{ChunkID: "x", DocID: "d2", Seq: 1, Content: "仅词法命中"},
	}
	out := fuseHybrid(vec, lex, 10)
	if len(out) != 4 {
		t.Fatalf("融合后数量错误: %d", len(out))
	}
	// b 双臂命中应排第一（RRF 累加）；strategy=hybrid
	if out[0].ChunkID != "b" || out[0].Strategy != "hybrid" {
		t.Fatalf("双臂命中应融合置顶 hybrid: %+v", out[0])
	}
	strat := map[string]string{}
	for _, h := range out {
		strat[h.ChunkID] = h.Strategy
	}
	if strat["a"] != "vector" || strat["c"] != "vector" || strat["x"] != "lexical" {
		t.Fatalf("strategy 标注错误: %v", strat)
	}
	// topK 截断
	if got := fuseHybrid(vec, lex, 2); len(got) != 2 {
		t.Fatalf("topK 截断错误: %d", len(got))
	}
	// 空词法臂 → 纯向量透传（截断）
	outOnlyVec := fuseHybrid(vec, nil, 2)
	if len(outOnlyVec) != 2 || outOnlyVec[0].ChunkID != "a" {
		t.Fatalf("空词法臂透传错误: %+v", outOnlyVec)
	}
	// 空向量臂 → 纯词法透传
	outOnlyLex := fuseHybrid(nil, lex, 10)
	if len(outOnlyLex) != 2 || outOnlyLex[0].ChunkID != "b" {
		t.Fatalf("空向量臂透传错误: %+v", outOnlyLex)
	}
}
