// KB-11（M35/37 号方案，D2 采纳）：统一检索出口与能力路由。
// `kb.mode` 解除互斥改库级能力开关（kb_vector/kb_graph，存量迁移口径=原 mode 单能力启用），
// 使用方（对话 recallKB / search-preview / graphrag-search）只传查询与范围，路由在出口内完成；
// mode 保留为展示页签默认。retrieval SSE 契约只增不改（mode 增 "hybrid"、strategy 增 "graph"）。
package kb

import (
	"context"
	"log"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// SearchUnified 统一检索出口：按库能力开关路由（向量[含 KB-10① 混合] / 图谱）。
//   - 双臂开：向量臂命中（chunk 证据，含父块上下文）在前、图谱臂实体/claims 补充，mode="hybrid"；
//   - 仅向量：mode="rag"；仅图谱：mode="graphrag"（无命中降级向量的 degraded 语义保留——
//     回退属图谱臂自身降级口径，不受能力开关约束，degraded 如实标注）；
//   - 单臂失败另一臂可用时如实降级不报错（degraded=true）。
func (s *Service) SearchUnified(ctx context.Context, k *store.KnowledgeBase, query string, maxResults int, minScore float64, opts GraphragOpts) (*GraphragDetail, string, bool, error) {
	if maxResults <= 0 {
		maxResults = k.TopK
	}
	if maxResults <= 0 {
		maxResults = 4
	}
	vecOn, graphOn := k.KBVector, k.KBGraph
	if !vecOn && !graphOn { // 归一后不应出现（store 层已派生）；防御走向量
		vecOn = true
	}
	switch {
	case vecOn && graphOn:
		var (
			vh   []RetrievalHit
			gd   *GraphragDetail
			vErr error
			gErr error
		)
		vh, vErr = s.vectorArm(ctx, k, query, maxResults, minScore)
		gd, gErr = s.graphArm(ctx, k, query, maxResults, opts)
		if vErr != nil && gErr != nil {
			return nil, "hybrid", true, vErr // 双臂全挂：向量臂错误更具诊断性
		}
		out := mergeUnified(vh, vErr, gd, gErr, maxResults)
		empty := len(out.Hits) == 0
		if vErr != nil || gErr != nil || empty {
			log.Printf("[kb] hybrid 路由部分降级 (kb=%s): vecErr=%v graphErr=%v empty=%v", k.ID, vErr, gErr, empty)
		}
		return out, "hybrid", vErr != nil || gErr != nil || empty, nil
	case graphOn:
		detail, mode, degraded, err := s.graphOnly(ctx, k, query, maxResults, minScore, opts)
		return detail, mode, degraded, err
	default: // 仅向量
		hits, err := s.vectorArm(ctx, k, query, maxResults, minScore)
		if err != nil {
			return &GraphragDetail{Hits: []RetrievalHit{}}, "rag", true, err
		}
		return &GraphragDetail{Hits: hits}, "rag", false, nil
	}
}

// graphOnly 仅图谱臂（历史 graphrag 行为保留：无命中/失败 → 降级向量 degraded）。
func (s *Service) graphOnly(ctx context.Context, k *store.KnowledgeBase, query string, maxResults int, minScore float64, opts GraphragOpts) (*GraphragDetail, string, bool, error) {
	detail, err := s.GraphragQueryDetail(ctx, k, query, opts)
	if err == nil && detail != nil && len(detail.Hits) > 0 {
		markGraph(detail)
		return detail, "graphrag", false, nil
	}
	if err != nil {
		log.Printf("[kb] graphrag query degraded (kb=%s): %v → 回退向量检索", k.ID, err)
	} else {
		log.Printf("[kb] graphrag query 无命中 (kb=%s) → 回退向量检索", k.ID)
	}
	fb, ferr := s.vectorArm(ctx, k, query, maxResults, minScore)
	return &GraphragDetail{Hits: fb}, "rag", true, ferr
}

// graphArm 图谱臂（双臂路由用：无命中/失败作为空贡献，不单独降级）。
func (s *Service) graphArm(ctx context.Context, k *store.KnowledgeBase, query string, maxResults int, opts GraphragOpts) (*GraphragDetail, error) {
	if opts.MaxResults <= 0 {
		opts.MaxResults = maxResults
	}
	return s.GraphragQueryDetail(ctx, k, query, opts)
}

// mergeUnified 双臂合并：向量命中在前（chunk 证据）、图谱命中补足余位；图谱明细透传。
func mergeUnified(vh []RetrievalHit, vErr error, gd *GraphragDetail, gErr error, maxResults int) *GraphragDetail {
	out := &GraphragDetail{Hits: []RetrievalHit{}}
	if vErr == nil && vh != nil {
		out.Hits = append(out.Hits, vh...)
	}
	if gErr == nil && gd != nil {
		markGraph(gd)
		out.Hits = append(out.Hits, gd.Hits...)
		out.Entities = gd.Entities
		out.Relationships = gd.Relationships
		out.Claims = gd.Claims
	}
	if len(out.Hits) > maxResults {
		out.Hits = out.Hits[:maxResults]
	}
	return out
}

// markGraph 图谱臂命中统一 strategy=graph（strategy 只增枚举）。
func markGraph(d *GraphragDetail) {
	if d == nil {
		return
	}
	for i := range d.Hits {
		d.Hits[i].Strategy = "graph"
	}
}
