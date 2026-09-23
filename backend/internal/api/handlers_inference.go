package api

// handlers_inference.go M13/D-O13（方案 §8/§6.16）：推理后端探测清单端点。

import (
	"net/http"
)

// listInferenceBackends GET /api/inference-backends：全部后端状态（探测结果 10min TTL 缓存）。
func (s *Server) listInferenceBackends(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"backends": s.Chat.Inference.List(r.Context(), false)})
}

// reprobeInferenceBackends POST /api/inference-backends/reprobe：忽略缓存重新探测。
func (s *Server) reprobeInferenceBackends(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"backends": s.Chat.Inference.List(r.Context(), true)})
}
