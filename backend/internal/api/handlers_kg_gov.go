// M16 阶段二（REQ-129）：KG 抽取治理与人工反馈——审核队列 / 实体消歧合并 / 质量面板 / 合并建议。
package api

import (
	"net/http"
	"strconv"
	"strings"

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

// kgMergeSuggestions GET /api/kg/{kbID}/merge-suggestions：系统合并建议（别名消歧粗规则，仅提示）。
func (s *Server) kgMergeSuggestions(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kbID")
	if _, err := s.Store.GetKnowledgeBase(kbID); err != nil {
		writeErr(w, err)
		return
	}
	list, err := s.Store.KGMergeSuggestions(kbID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": list})
}

