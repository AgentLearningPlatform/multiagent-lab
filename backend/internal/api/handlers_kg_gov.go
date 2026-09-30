// M16 阶段二（REQ-129）：KG 抽取治理与人工反馈——审核队列 / 实体消歧合并 / 质量面板 / 合并建议。
package api

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// kgReview POST /api/kg/{kbID}/review：关系/claims 人工审核（REQ-129③）。
// body {kind: "relationship"|"claim", id, status: "approved"|"rejected"}；rejected 不参与检索。
func (s *Server) kgReview(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kbID")
	if _, err := s.Store.GetKnowledgeBase(kbID); err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Kind   string `json:"kind"`
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(in.ID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 必填"})
		return
	}
	switch in.Kind {
	case "relationship":
		if err := s.Store.SetKGRelStatus(kbID, in.ID, in.Status); err != nil {
			writeErr(w, err)
			return
		}
	case "claim":
		if err := s.Store.SetKGClaimStatus(kbID, in.ID, in.Status); err != nil {
			writeErr(w, err)
			return
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind 必须是 relationship|claim"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "kind": in.Kind, "id": in.ID, "status": in.Status})
}

// kgMerge POST /api/kg/{kbID}/merge：实体消歧合并（REQ-129②，仅人工触发）。
// body {keep: "保留实体名", merge: ["被合并实体名", ...]}——关系/claims 迁移到 keep，merge 实体删除。
func (s *Server) kgMerge(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kbID")
	if _, err := s.Store.GetKnowledgeBase(kbID); err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Keep  string   `json:"keep"`
		Merge []string `json:"merge"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	movedRels, movedClaims, err := s.Store.MergeKGEntities(kbID, strings.TrimSpace(in.Keep), in.Merge)
	if err != nil {
		writeErr(w, err)
		return
	}
	// 合并本身即一条治理决策（REQ-101 审计留痕口径）
	_, _ = s.Store.InsertDecision(&store.OntoDecision{
		SubjectKind: "kg",
		SubjectID:   kbID,
		Title:       "实体消歧合并：" + strings.Join(in.Merge, "、") + " → " + in.Keep,
		Rationale:   "人工触发合并：迁移关系 " + strconv.Itoa(movedRels) + " 条、claims " + strconv.Itoa(movedClaims) + " 条",
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "keep": in.Keep, "merged": in.Merge, "moved_relationships": movedRels, "moved_claims": movedClaims,
	})
}

// kgQuality GET /api/kg/{kbID}/quality：质量面板（REQ-129④）——method 分布、孤儿实体、
// 高频关系类型 TopN、rejected 计数。
func (s *Server) kgQuality(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kbID")
	if _, err := s.Store.GetKnowledgeBase(kbID); err != nil {
		writeErr(w, err)
		return
	}
	q, err := s.Store.KGQuality(kbID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}

// kgMergeSuggestions GET /api/kg/{kbID}/merge-suggestions：合并建议（M36/KB-7 起为
// 规则〔名称包含〕+ 向量〔名+描述 embedding 相似〕双臂；向量臂 embedding 不可用时降级并标
// vector_degraded；双嵌入防误并：类型不同附 type_warning「慎并」，人工确认后才执行合并）。
func (s *Server) kgMergeSuggestions(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kbID")
	if _, err := s.Store.GetKnowledgeBase(kbID); err != nil {
		writeErr(w, err)
		return
	}
	out := []kb.MergeSuggestion{}
	rule, err := s.Store.KGMergeSuggestions(kbID)
	if err != nil {
		writeErr(w, err)
		return
	}
	seen := map[string]bool{}
	pairKey := func(a, b string) string {
		if a > b {
			a, b = b, a
		}
		return a + "|" + b
	}
	for _, sg := range rule {
		out = append(out, kb.MergeSuggestion{Keep: sg.Keep, Merge: sg.Merge, Reason: sg.Reason, Strategy: "rule"})
		seen[pairKey(sg.Keep, sg.Merge)] = true
	}
	vectorDegraded := false
	if vec, verr := s.KB.KGVectorMergeSuggestions(r.Context(), kbID, 10); verr != nil {
		vectorDegraded = true
		log.Printf("[api] kb=%s 向量消歧建议降级: %v", kbID, verr)
	} else {
		for _, sg := range vec {
			if seen[pairKey(sg.Keep, sg.Merge)] {
				continue
			}
			seen[pairKey(sg.Keep, sg.Merge)] = true
			out = append(out, sg)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": out, "vector_degraded": vectorDegraded})
}

// kgEntityAlias PUT /api/kg/{kbID}/entity-alias：实体别名人工标注（M36/KB-7②）。
// body {name, alias}；alias 分号分隔多别名，空串清除；重建/合并时别名自动保留归并。
func (s *Server) kgEntityAlias(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kbID")
	if _, err := s.Store.GetKnowledgeBase(kbID); err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Name  string `json:"name"`
		Alias string `json:"alias"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name 必填"})
		return
	}
	if err := s.Store.SetKGEntityAlias(kbID, strings.TrimSpace(in.Name), in.Alias); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": in.Name, "alias": in.Alias})
}
