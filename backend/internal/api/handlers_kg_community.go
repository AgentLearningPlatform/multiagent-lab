// M16 阶段二（REQ-130）：社区摘要与全局问答——重建（检测+摘要）/ 列表 / 全局检索端点。
package api

import (
	"net/http"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kg"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// kgCommunitiesRebuild POST /api/kg/{kbID}/communities/rebuild：
// 社区检测（label propagation）+ 每社区摘要（LLM 主路径/骨架回退）全量重建。
func (s *Server) kgCommunitiesRebuild(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kbID")
	k, err := s.Store.GetKnowledgeBase(kbID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if k.Mode != "graphrag" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "仅 graphrag 模式库支持社区摘要"})
		return
	}
	sum := &kg.Summarizer{Store: s.Store, Box: s.Box, ConnID: k.KGConnID} // 库级抽取连接（REQ-129① 同源）
	n, err := sum.BuildKGCommunities(r.Context(), kbID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "communities": n})
}

// kgCommunities GET /api/kg/{kbID}/communities：社区列表（摘要 + 成员实体）。
func (s *Server) kgCommunities(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kbID")
	if _, err := s.Store.GetKnowledgeBase(kbID); err != nil {
		writeErr(w, err)
		return
	}
	list, err := s.Store.ListKGCommunities(kbID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if list == nil {
		list = []*store.KGCommunity{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"communities": list})
}

// kgGlobalSearch POST /api/kb/{id}/global-search：全局性问题检索（社区摘要匹配，REQ-130）。
// body {query}；社区未建时 200 + degraded:true + 引导信息。
func (s *Server) kgGlobalSearch(w http.ResponseWriter, r *http.Request) {
	k, err := s.Store.GetKnowledgeBase(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Query string `json:"query"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(in.Query) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query 必填"})
		return
	}
	comms, err := s.Store.ListKGCommunities(k.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(comms) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"kb_id": k.ID, "degraded": true,
			"message":     "尚未构建社区摘要：请先在图谱视图点击「重建社区摘要」。",
			"communities": []any{},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kb_id": k.ID, "degraded": false, "hits": kg.GlobalSearch(k.ID, in.Query, comms),
	})
}
