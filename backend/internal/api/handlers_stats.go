package api

import (
	"net/http"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// usageStats 使用统计（GET /api/stats/usage?group_by=model|agent|project）。
// 契约：{"rows":[{key,label,calls,prompt_tokens,completion_tokens,total_tokens}]}。
func (s *Server) usageStats(w http.ResponseWriter, r *http.Request) {
	groupBy := r.URL.Query().Get("group_by")
	switch groupBy {
	case "model", "agent", "project":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group_by must be model|agent|project"})
		return
	}
	rows, err := s.Store.UsageStats(groupBy)
	if err != nil {
		writeErr(w, err)
		return
	}
	if rows == nil {
		rows = []store.UsageRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}
