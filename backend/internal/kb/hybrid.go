// KB-10①（M35/37 号方案）：混合检索——SQLite FTS5 BM25 词法臂 + 向量臂 RRF 融合（k=60）。
// 命中带 strategy 标注（vector|lexical|hybrid，只增不改）；词法臂不可用（FTS5 缺失/查询过短）
// 自动退化为纯向量，行为与历史一致。
package kb

import (
	"context"
	"log"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// rrfK RRF 常数（45 号 KB-10 口径 k=60）。
const rrfK = 60

// lexicalMinRunes trigram 分词最小查询长度（<3 字符无法构成 trigram，跳过词法臂）。
const lexicalMinRunes = 3

// hybridEnabled 混合检索开关（KB_HYBRID_RETRIEVAL=off 关闭；默认开）。
func hybridEnabled() bool { return os.Getenv("KB_HYBRID_RETRIEVAL") != "off" }

// lexicalArmed 词法臂是否可用（开关开 + FTS5 就绪 + 查询可构成 trigram）。
func (s *Service) lexicalArmed(query string) bool {
	if !hybridEnabled() || utf8.RuneCountInString(strings.TrimSpace(query)) < lexicalMinRunes {
		return false
	}
	return s.Store.ChunkFTSReady()
}

// fuseHybrid RRF 融合：vecHits（已按相似度降序）与 lexHits（已按 bm25 降序）按名次贡献
// 1/(k+rank) 累加；strategy=双臂 hybrid / 仅向量 vector / 仅词法 lexical。返回降序前 topK 条。
// 纯函数（可单测）：不触库，lexHits 已由调用方按库过滤。
func fuseHybrid(vecHits []Hit, lexHits []store.FTSHit, topK int) []Hit {
	if len(lexHits) == 0 {
		if topK > 0 && len(vecHits) > topK {
			vecHits = vecHits[:topK]
		}
		return vecHits
	}
	if len(vecHits) == 0 {
		out := make([]Hit, 0, len(lexHits))
		for _, h := range lexHits {
			out = append(out, Hit{ChunkID: h.ChunkID, DocID: h.DocID, Seq: h.Seq, Content: h.Content, Score: h.Score})
		}
		if topK > 0 && len(out) > topK {
			out = out[:topK]
		}
		return out
	}
	type acc struct {
		hit      Hit
		score    float64
		inVec    bool
		inLex    bool
	}
	byID := map[string]*acc{}
	order := []string{}
	add := func(chunkID string, rank int, h Hit, vec bool) {
		a := byID[chunkID]
		if a == nil {
			a = &acc{hit: h}
			byID[chunkID] = a
			order = append(order, chunkID)
		}
		a.score += 1.0 / float64(rrfK+rank)
		if vec {
			a.inVec = true
		} else {
			a.inLex = true
		}
	}
	for i, h := range vecHits {
		add(h.ChunkID, i+1, h, true)
	}
	for i, h := range lexHits {
		hh := Hit{ChunkID: h.ChunkID, DocID: h.DocID, Seq: h.Seq, Content: h.Content, Score: h.Score}
		add(h.ChunkID, i+1, hh, false)
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := byID[order[i]], byID[order[j]]
		if a.score != b.score {
			return a.score > b.score
		}
		return order[i] < order[j] // 并列取 id 字典序，稳定可复现
	})
	if topK > 0 && len(order) > topK {
		order = order[:topK]
	}
	out := make([]Hit, 0, len(order))
	for _, id := range order {
		a := byID[id]
		switch {
		case a.inVec && a.inLex:
			a.hit.Strategy = "hybrid"
		case a.inLex:
			a.hit.Strategy = "lexical"
		default:
			a.hit.Strategy = "vector"
		}
		out = append(out, a.hit)
	}
	return out
}

// hybridSearch 向量臂 + 词法臂混合检索入口（rag 检索路径共用；词法臂失败/不可用逐级降级纯向量）。
func (s *Service) hybridSearch(ctx context.Context, kbID, query string, vec []float32, topK int, minScore float64) ([]Hit, error) {
	vecHits, err := s.Vector.Search(ctx, kbID, vec, topK, minScore)
	if err != nil {
		return nil, err
	}
	if !s.lexicalArmed(query) {
		return vecHits, nil
	}
	lexHits, lerr := s.Store.SearchChunksFTS(kbID, store.FTSQuote(query), topK*2)
	if lerr != nil {
		log.Printf("[kb] 词法臂失败，退化纯向量 (kb=%s): %v", kbID, lerr)
		return vecHits, nil
	}
	return fuseHybrid(vecHits, lexHits, topK), nil
}
