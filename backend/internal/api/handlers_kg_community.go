// M16 阶段二（REQ-130）：社区摘要与全局问答——重建（检测+摘要）/ 列表 / 全局检索端点。
package api

import (
	"net/http"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kg"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// kgCommunitiesRebuild POST /api/kg/{kbID}/communities/rebuild：
// 社区检测（KB-5 起 Louvain 主路径/lp 回退）+ 摘要（小库全量即时生成；大库按需生成+缓存 D5）全量重建。
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
	n, method, err := sum.BuildKGCommunities(r.Context(), kbID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "communities": n, "method": method})
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

// kgGlobalSearch POST /api/kb/{id}/global-search：全局性问答（KB-5② 升级：TopN 社区摘要
// map-reduce 进 LLM 上下文生成回答，answer 字段只增；摘要缺失按需生成+缓存 D5）。
// body {query}；社区未建时 200 + degraded:true + 引导信息；生成失败降级为摘要回显（degraded:true）。
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
	sum := &kg.Summarizer{Store: s.Store, Box: s.Box, ConnID: k.KGConnID}
	answer, hits, ok := sum.GlobalAnswer(r.Context(), k.ID, k.KGConnID, in.Query, comms)
	resp := map[string]any{"kb_id": k.ID, "degraded": !ok, "hits": hits}
	if ok {
		resp["answer"] = answer
	} else {
		resp["message"] = "未命中可用的社区摘要（可先重建社区，或换用图谱/向量检索）"
	}
	writeJSON(w, http.StatusOK, resp)
}
