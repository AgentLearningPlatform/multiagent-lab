package api

import (
	"net/http"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// usageStats 使用统计（GET /api/stats/usage?group_by=model|agent|project&from=YYYY-MM-DD&to=YYYY-MM-DD）。
// from/to 可选、闭区间；契约：{"rows":[{key,label,calls,prompt_tokens,completion_tokens,total_tokens}]}。
func (s *Server) usageStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	groupBy := q.Get("group_by")
	switch groupBy {
	case "model", "agent", "project":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group_by must be model|agent|project"})
		return
	}

	from, ok := parseUsageDate(q.Get("from"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from/to 必须为 YYYY-MM-DD"})
		return
	}
	to, ok := parseUsageDate(q.Get("to"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from/to 必须为 YYYY-MM-DD"})
		return
	}
	// 闭区间日期字符串（YYYY-MM-DD）字典序即时间序，可直接比较
	if from != "" && to != "" && from > to {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from 不能晚于 to"})
		return
	}

	rows, err := s.Store.UsageStats(groupBy, store.UsageRange{From: from, To: to})
	if err != nil {
		writeErr(w, err)
		return
	}
	if rows == nil {
		rows = []store.UsageRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}

// parseUsageDate 校验并规范化 YYYY-MM-DD 日期参数；空串表示未提供。
func parseUsageDate(v string) (string, bool) {
	if v == "" {
		return "", true
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return "", false
	}
	return t.Format("2006-01-02"), true
}
