// M16 阶段一（REQ-127 图谱浏览与统计）：graphrag 库「图谱」视图的数据面。
// 统计卡 / 实体搜索 / 邻域展开（BFS 1~2 跳）+ claims chunk 溯源。
package api

import (
	"net/http"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// kgStats GET /api/kg/{kbID}/stats：统计卡（实体/关系/claims 计数、类型分布、文档覆盖）。
func (s *Server) kgStats(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kbID")
	if _, err := s.Store.GetKnowledgeBase(kbID); err != nil {
		writeErr(w, err)
		return
	}
	st, err := s.Store.KGStatsForKB(kbID)
	if err != nil {
		writeErr(w, err)
		return
	}
	st.DegradedDocs = st.DocsTotal - st.DocsWithKG
	writeJSON(w, http.StatusOK, st)
}

// kgEntitySearch GET /api/kg/{kbID}/entities?q=&limit=：实体名模糊搜索（图谱页搜索框）。
func (s *Server) kgEntitySearch(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kbID")
	if _, err := s.Store.GetKnowledgeBase(kbID); err != nil {
		writeErr(w, err)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := atoiDefault(r.URL.Query().Get("limit"), 20)
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"entities": []*store.KGEntity{}})
		return
	}
	ents, err := s.Store.KGSearchEntities(kbID, q, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	if ents == nil {
		ents = []*store.KGEntity{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entities": ents})
}

// kgNeighborhood GET /api/kg/{kbID}/neighborhood?entity=&hops=1|2&types=a,b：
// 以实体为种子的邻域子图（实体 + 关系 + claims 含 chunk 溯源 doc/seq）。
// 学习尺度：KGByKB 一次载入全图后在内存 BFS（教学规模实体 ≤ 数百，性能足够）。
func (s *Server) kgNeighborhood(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kbID")
	if _, err := s.Store.GetKnowledgeBase(kbID); err != nil {
		writeErr(w, err)
		return
	}
	seed := strings.TrimSpace(r.URL.Query().Get("entity"))
	if seed == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "entity 必填"})
		return
	}
	hops := atoiDefault(r.URL.Query().Get("hops"), 1)
	if hops > 2 {
		hops = 2
	}
	var allowTypes map[string]bool
	if tv := strings.TrimSpace(r.URL.Query().Get("types")); tv != "" {
		allowTypes = map[string]bool{}
		for _, t := range strings.Split(tv, ",") {
			if t = strings.TrimSpace(t); t != "" {
				allowTypes[t] = true
			}
		}
	}
	_, allRels, err := s.Store.KGByKB(kbID)
	if err != nil {
		writeErr(w, err)
		return
	}

	entitySet := map[string]bool{seed: true}
	inRels := []*store.KGRelationship{}
	seenRel := map[string]bool{}
	frontier := []string{seed}
	for hop := 0; hop < hops; hop++ {
		next := []string{}
		frontierSet := map[string]bool{}
		for _, n := range frontier {
			frontierSet[n] = true
		}
		for _, rel := range allRels {
			if allowTypes != nil && !allowTypes[rel.Type] {
				continue
			}
			inS, inT := frontierSet[rel.Source], frontierSet[rel.Target]
			if !inS && !inT {
				continue
			}
			if !seenRel[rel.ID] {
				seenRel[rel.ID] = true
				inRels = append(inRels, rel)
			}
			other := rel.Source
			if inS && !inT {
				other = rel.Target
			}
			if !entitySet[other] {
				entitySet[other] = true
				next = append(next, other)
			}
		}
		if len(next) == 0 {
			break
		}
		frontier = next
	}
	names := make([]string, 0, len(entitySet))
	for n := range entitySet {
		names = append(names, n)
	}
	entities, err := s.Store.KGEntitiesByNames(kbID, names)
	if err != nil {
		writeErr(w, err)
		return
	}
	entList := make([]*store.KGEntity, 0, len(entities))
	for _, n := range names {
		if e := entities[n]; e != nil {
			entList = append(entList, e)
		}
	}
	claims, err := s.Store.KGClaimsWithChunks(kbID, names, 100)
	if err != nil {
		writeErr(w, err)
		return
	}
if claims == nil {
		claims = []*store.KGClaimTrace{}
	}
	if inRels == nil {
		inRels = []*store.KGRelationship{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kb_id":         kbID,
		"seed":          seed,
		"hops":          hops,
		"entities":      entList,
		"relationships": inRels,
		"claims":        claims,
	})
}
