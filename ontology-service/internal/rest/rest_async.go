package rest

// REQ-207/M43（52 号 E5④）：生成异步化——ai-draft 202+轮询（沿 REQ-146 先例）。
// 治 A-6「生成链路同步阻塞 120s」：POST ai-draft-async 立即返回 job_id（202），
// 后台 goroutine 执行 Draft；GET ai-draft-jobs/{id} 轮询结果。旧同步端点保留兼容。
// 进程内 map（单机教学尺度；重启丢任务=前端重新发起，可接受诚实边界）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type draftJob struct {
	ID        string       `json:"id"`
	Status    string       `json:"status"` // running | done | failed
	Result    *draftResult `json:"result,omitempty"`
	Error     string       `json:"error,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
}

type draftResult struct {
	Spec   map[string]any `json:"spec"`
	Rounds int            `json:"rounds"`
}

var (
	draftJobsMu sync.Mutex
	draftJobs   = map[string]*draftJob{}
)

// aiDraftAsync POST /api/ontologies/ai-draft-async → 202 {job_id}。
func (s *Server) aiDraftAsync(w http.ResponseWriter, r *http.Request) {
	if s.LLM == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "LLM 辅助创建未配置（PLATFORM_URL）"})
		return
	}
	var req struct {
		Description         string   `json:"description"`
		ExtraHint           string   `json:"extra_hint"`
		CapabilityQuestions []string `json:"capability_questions"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Description) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "description 必填"})
		return
	}
	job := &draftJob{ID: fmt.Sprintf("job_%d", time.Now().UnixNano()), Status: "running", CreatedAt: time.Now()}
	draftJobsMu.Lock()
	draftJobs[job.ID] = job
	draftJobsMu.Unlock()
	go func() {
		res, err := s.LLM.Draft(req.Description, req.ExtraHint)
		draftJobsMu.Lock()
		defer draftJobsMu.Unlock()
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
			return
		}
		job.Status = "done"
		// spec 结构体 → map（与同步端点 JSON 形态一致）
		b, _ := json.Marshal(res.Spec)
		var specMap map[string]any
		_ = json.Unmarshal(b, &specMap)
		job.Result = &draftResult{Spec: specMap, Rounds: res.Rounds}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": job.ID})
}

// aiDraftJob GET /api/ai-draft-jobs/{id} 轮询。
func (s *Server) aiDraftJob(w http.ResponseWriter, r *http.Request) {
	draftJobsMu.Lock()
	job, ok := draftJobs[r.PathValue("id")]
	draftJobsMu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job 不存在（或服务重启丢失——请重新发起）"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}
