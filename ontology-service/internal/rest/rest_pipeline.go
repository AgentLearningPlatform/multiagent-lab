// rest_pipeline.go 工具链配置端点（REQ-75/76，04 §4.6）。
// pipeline_profile = 路径级工具链配置（七阶段 × 候选工具）；guided 打卡走 checklist 双 key 空间的 tool: 侧。
// 候选清单数据驱动（pipeline/tools.json），增删工具不改核心代码（REQ-77 开放性）。
package rest

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/pipeline"
)

// pipelineDB 惰性初始化（表由 migrations/004_pipeline.sql 建）。
func (s *Server) pipelineStore() *pipeline.Store {
	if s.pipelineDB == nil {
		s.pipelineDB = pipeline.NewStore(s.Store.DB())
	}
	return s.pipelineDB
}

// listToolCatalog GET /api/pipelines/catalog → 七阶段候选工具清单（分组返回）
func (s *Server) listToolCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"stages": pipeline.StageIDs, "catalog": pipeline.Catalog()})
}

// listPipelines GET /api/pipelines → 配置列表
func (s *Server) listPipelines(w http.ResponseWriter, _ *http.Request) {
	ps, err := s.pipelineStore().List()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

// createPipeline POST /api/pipelines {name, ontology_id?} → 201（默认工具链模板）
func (s *Server) createPipeline(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		OntologyID string `json:"ontology_id"`
	}
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name 必填"})
		return
	}
	p, err := s.pipelineStore().Create("pipe_"+newID(), name, strings.TrimSpace(req.OntologyID), nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// getPipeline GET /api/pipelines/{id} → 详情（附 checklist 视图）
func (s *Server) getPipeline(w http.ResponseWriter, r *http.Request) {
	p, err := s.pipelineStore().Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"profile":   p,
		"checklist": pipeline.BuildChecklist(p.Stages),
	})
}

// updatePipeline PUT /api/pipelines/{id} {name?, ontology_id?, runtime_profile_id?, stages?} → 全量/局部更新
func (s *Server) updatePipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.pipelineStore().Get(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Name             *string                          `json:"name"`
		OntologyID       *string                          `json:"ontology_id"`
		RuntimeProfileID *string                          `json:"runtime_profile_id"`
		Stages           *map[string]pipeline.StageSelection `json:"stages"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	if req.Stages != nil {
		if err := s.pipelineStore().UpdateStages(id, *req.Stages); err != nil {
			writeErr(w, err)
			return
		}
	}
	name := p.Name
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		name = strings.TrimSpace(*req.Name)
	}
	if err := s.pipelineStore().UpdateMeta(id, name, req.OntologyID, req.RuntimeProfileID); err != nil {
		writeErr(w, err)
		return
	}
	np, err := s.pipelineStore().Get(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, np)
}

// clonePipeline POST /api/pipelines/{id}/clone {name?} → 201 新配置（复制阶段选择，checklist 清零）
func (s *Server) clonePipeline(w http.ResponseWriter, r *http.Request) {
	src, err := s.pipelineStore().Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = src.Name + "（副本）"
	}
	p, err := s.pipelineStore().Create("pipe_"+newID(), name, src.OntologyID, src.Stages)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// deletePipeline DELETE /api/pipelines/{id}
func (s *Server) deletePipeline(w http.ResponseWriter, r *http.Request) {
	if err := s.pipelineStore().Delete(r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// checkPipeline POST /api/pipelines/{id}/check {key, done} → guided 打卡（tool:<stage>:<tool_id>）
func (s *Server) checkPipeline(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key  string `json:"key"`
		Done *bool  `json:"done"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key 必填"})
		return
	}
	done := req.Done == nil || *req.Done
	if err := s.pipelineStore().Check(r.PathValue("id"), key, done); err != nil {
		writeErr(w, err)
		return
	}
	p, err := s.pipelineStore().Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "done": done, "checklist": p.Checklist})
}

// pipelineCatalogJSON catalog 原样透出（前端首屏渲染候选卡片）。
var _ = json.Marshal
