package companion

import (
	"sort"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---------------------------------------------------------------------------
// REQ-194/M34 批次二⑥：确认桶聚类（确认疲劳治理）。
// ListCompanionCandidates 平面列表按实体 slug 归组：组内 pending 计数、代表候选=组内置信最高；
// 前端按实体视图分组呈现 + 按组批量 confirm/reject（循环既有单候选端点，结果如实计数）。
// ---------------------------------------------------------------------------

// CandidateGroup 按实体归组的一桶候选。
type CandidateGroup struct {
	Key            string                      `json:"key"`            // 实体 slug（归组键）
	Entity         string                      `json:"entity"`         // 实体标签（代表候选的 name）
	Count          int                         `json:"count"`          // 组内候选总数（当前 status 过滤后）
	PendingCount   int                         `json:"pending_count"`  // 组内 pending 数（跨状态参考）
	Representative *store.CompanionCandidate   `json:"representative"` // 代表候选（组内置信最高）
	Members        []*store.CompanionCandidate `json:"members"`        // 组内全部候选（当前过滤序）
}

// GroupCandidatesByEntity 候选列表按实体 slug 归组（纯函数）。
// 归组键=Name 的 slug（relation 候选的 Name 即主体可读态——按主体实体归组）；
// 组序=组内最新 created_at 倒序（与时间倒序视图同一时序感）；代表候选=组内 confidence 最高。
func GroupCandidatesByEntity(list []*store.CompanionCandidate) []*CandidateGroup {
	byKey := map[string]*CandidateGroup{}
	var order []string
	for _, c := range list {
		k := Slug(c.Name)
		g, ok := byKey[k]
		if !ok {
			g = &CandidateGroup{Key: k, Entity: c.Name}
			byKey[k] = g
			order = append(order, k)
		}
		g.Members = append(g.Members, c)
		g.Count++
		if c.Status == "pending" {
			g.PendingCount++
		}
		if g.Representative == nil || c.Confidence > g.Representative.Confidence {
			g.Representative = c
			g.Entity = c.Name
		}
	}
	out := make([]*CandidateGroup, 0, len(byKey))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].latestAt() > out[j].latestAt()
	})
	return out
}

// latestAt 组内最新候选时间（空安全）。
func (g *CandidateGroup) latestAt() string {
	if len(g.Members) == 0 {
		return ""
	}
	latest := g.Members[0].CreatedAt
	for _, m := range g.Members[1:] {
		if m.CreatedAt > latest {
			latest = m.CreatedAt
		}
	}
	return latest
}
