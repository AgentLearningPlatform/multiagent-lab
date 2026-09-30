// KB-7 实体消歧自动化（M36/37 号正式方案；35 号 KB-7 详设）：
// embedding 相似合并建议（实体名+描述向量化，topK+阈值）替代纯 O(n²) 名称包含规则；
// 「仅建议、人工确认」边界不破（REQ-129 原则，合并动作仍走既有 /merge 端点）；
// 双嵌入防误并：同名不同类型的对附类型警示（iText2KG 语义：「Python:语言」≠「Python:蛇」）。
// embedding 未配置/失败 → 如实返回 error（API 层降级为规则建议并标 degraded）。
package kb

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// MergeSuggestion 合并建议视图（API 层规则建议 + 本文件向量建议合并后的统一形态）。
type MergeSuggestion struct {
	Keep   string `json:"keep"`
	Merge  string `json:"merge"`
	Reason string `json:"reason"`
	// Strategy rule（名称包含规则）| vector（embedding 相似）；空 = 历史口径（纯规则）
	Strategy string `json:"strategy,omitempty"`
	// Similarity 向量余弦相似度（仅 vector）
	Similarity float64 `json:"similarity,omitempty"`
	// TypeWarning 双嵌入防误并提示（类型不同时附，慎并）
	TypeWarning string `json:"type_warning,omitempty"`
}

// mergeSimThreshold 向量建议相似度阈值（env KG_MERGE_SIM_THRESHOLD，默认 0.85）。
func mergeSimThreshold() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("KG_MERGE_SIM_THRESHOLD"), 64); err == nil && v > 0 && v < 1 {
		return v
	}
	return 0.85
}

// KGVectorMergeSuggestions 向量相似合并建议：实体「名 + 描述」embedding 两两余弦，
// ≥阈值即为建议（keep=短名/先建名，merge=另一名）；topK 限条数。
func (s *Service) KGVectorMergeSuggestions(ctx context.Context, kbID string, topK int) ([]MergeSuggestion, error) {
	if topK <= 0 {
		topK = 10
	}
	ents, _, err := s.Store.KGByKB(kbID)
	if err != nil {
		return nil, err
	}
	if len(ents) < 2 {
		return []MergeSuggestion{}, nil
	}
	texts := make([]string, 0, len(ents))
	for _, e := range ents {
		t := e.Name
		if e.Description != "" {
			t += "：" + e.Description
		}
		texts = append(texts, t)
	}
	vecs, err := s.embedder().EmbedTexts(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embedding 不可用（实体消歧向量建议降级）: %w", err)
	}
	th := mergeSimThreshold()
	type pair struct {
		a, b *store.KGEntity
		sim  float64
	}
	pairs := []pair{}
	for i := 0; i < len(ents); i++ {
		for j := i + 1; j < len(ents); j++ {
			if ents[i].Name == ents[j].Name {
				continue
			}
			sim := cosine(vecs[i], vecs[j])
			if sim >= th {
				pairs = append(pairs, pair{ents[i], ents[j], sim})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].sim > pairs[j].sim })
	out := make([]MergeSuggestion, 0, topK)
	for _, p := range pairs {
		if len(out) >= topK {
			break
		}
		keep, merge := p.a.Name, p.b.Name
		if len([]rune(merge)) < len([]rune(keep)) { // 短名优先为 keep（与规则口径一致）
			keep, merge = merge, keep
		}
		sg := MergeSuggestion{
			Keep: keep, Merge: merge, Strategy: "vector", Similarity: p.sim,
			Reason: fmt.Sprintf("「%s」与「%s」语义相似（%s 相似度 %.3f ≥ %.2f）", keep, merge, "名+描述", p.sim, th),
		}
		if p.a.Type != p.b.Type {
			sg.TypeWarning = fmt.Sprintf("两者类型不同（%s vs %s），可能为同名异义，慎并", p.a.Type, p.b.Type)
		}
		out = append(out, sg)
	}
	if len(out) > 0 {
		log.Printf("[kb] kb=%s 向量消歧建议 %d 对（阈值 %.2f）", kbID, len(out), th)
	}
	return out, nil
}
