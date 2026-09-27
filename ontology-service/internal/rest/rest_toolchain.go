package rest

// REQ-155 AI-native 本体工具链统一调用面（M-O15 阶段一，docs/23 §6.1）。
// 五工具一信封：{tool, ok, duration_ms, result}——validate / lint / diff / version / query。
// 复用边界：结构校验与质量引擎复用 pkg/ontology/spec + internal/qualitygate；
// diff 复用本包 diffConcepts/diffRelations/diffInstances（与 REQ-95 diffVersions 同源）；
// query 复用 internal/toolchain（SELECT-only 门禁与 facade 同口径）。

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/repo"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/toolchain"
	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
)

// specDiff 版本/草稿对照结果（diffVersions 与 toolchain diff 共用形状）。
type specDiff struct {
	Concepts  diffSet      `json:"concepts"`
	Relations diffSet      `json:"relations"`
	Instances diffSet      `json:"instances"`
	Impact    []diffImpact `json:"impact"`
}

func buildSpecDiff(from, to *pkgspec.Spec) specDiff {
	concepts := diffConcepts(from.Concepts, to.Concepts)
	relations := diffRelations(from.Relations, to.Relations)
	instances := diffInstances(from.Instances, to.Instances)

	refs := conceptReferences(*to)
	impact := []diffImpact{}
	seen := map[string]bool{}
	addImpact := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		impact = append(impact, diffImpact{Name: name, ReferencedBy: refs[name]})
	}
	for _, c := range concepts.Removed {
		if cc, ok := c.(pkgspec.Concept); ok {
			addImpact(cc.Name)
		}
	}
	for _, c := range concepts.Changed {
		addImpact(c.Name)
	}
	sort.SliceStable(impact, func(i, j int) bool {
		if impact[i].ReferencedBy != impact[j].ReferencedBy {
			return impact[i].ReferencedBy > impact[j].ReferencedBy
		}
		return impact[i].Name < impact[j].Name
	})
	return specDiff{Concepts: concepts, Relations: relations, Instances: instances, Impact: impact}
}

type toolchainReq struct {
	OntologyID string           `json:"ontology_id"`
	Spec       *json.RawMessage `json:"spec"` // 内联 spec 草稿（validate/lint/diff 的 to 侧）
	Strict     bool             `json:"strict"`

	From    int `json:"from"`    // diff 版本模式：起始版本
	To      int `json:"to"`      // diff 版本模式：目标版本
	Version int `json:"version"` // version 工具：取指定版本快照（0=仅列版本）

	Endpoint  string `json:"endpoint"`
	Query     string `json:"query"`
	Limit     int    `json:"limit"`
	TimeoutMs int    `json:"timeout_ms"`
}

// toolchain POST /api/ontology/toolchain/{tool} 统一入口。
func (s *Server) toolchain(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	tool := r.PathValue("tool")
	var req toolchainReq
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}

	var result any
	switch tool {
	case "validate", "lint":
		sp, err := s.loadToolchainSpec(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if tool == "validate" {
			result = toolchain.Validate(sp, req.Strict)
		} else {
			result = toolchain.Lint(sp)
		}
	case "diff":
		payload, ok := s.toolchainDiff(w, req)
		if !ok {
			return
		}
		result = payload
	case "version":
		payload, ok := s.toolchainVersion(w, req)
		if !ok {
			return
		}
		result = payload
	case "query":
		table, err := toolchain.Query(r.Context(), req.Endpoint, req.Query, req.Limit, req.TimeoutMs)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		result = table
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "未知工具: " + tool + "（可用 validate/lint/diff/version/query）"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tool":        tool,
		"ok":          true,
		"duration_ms": time.Since(started).Milliseconds(),
		"result":      result,
	})
}

// loadToolchainSpec 取被检 spec：内联 spec 优先（LLM 草稿不落库即可检），否则按 ontology_id 取当前 spec_json。
func (s *Server) loadToolchainSpec(req toolchainReq) (*pkgspec.Spec, error) {
	if req.Spec != nil {
		var sp pkgspec.Spec
		if err := json.Unmarshal(*req.Spec, &sp); err != nil {
			return nil, errors.New("内联 spec 解析失败: " + err.Error())
		}
		return &sp, nil
	}
	if req.OntologyID == "" {
		return nil, errors.New("需提供 ontology_id 或内联 spec")
	}
	raw, _, err := s.Store.GetArtifact(req.OntologyID, "spec_json")
	if err != nil {
		return nil, err
	}
	var sp pkgspec.Spec
	if err := json.Unmarshal([]byte(raw), &sp); err != nil {
		return nil, errors.New("当前 spec_json 解析失败: " + err.Error())
	}
	return &sp, nil
}

// toolchainDiff 两种模式：①from/to 版本号——取版本快照对照；②内联 spec + ontology_id——当前版本 vs 草稿（AI 修复环对照）。
func (s *Server) toolchainDiff(w http.ResponseWriter, req toolchainReq) (map[string]any, bool) {
	if req.Spec != nil && req.OntologyID != "" {
		fromRaw, _, err := s.Store.GetArtifact(req.OntologyID, "spec_json")
		if err != nil {
			writeErr(w, err)
			return nil, false
		}
		var from pkgspec.Spec
		if err := json.Unmarshal([]byte(fromRaw), &from); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "当前 spec_json 解析失败: " + err.Error()})
			return nil, false
		}
		var to pkgspec.Spec
		if err := json.Unmarshal(*req.Spec, &to); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "内联 spec 解析失败: " + err.Error()})
			return nil, false
		}
		d := buildSpecDiff(&from, &to)
		return map[string]any{"from_version": "current", "to_version": "draft", "concepts": d.Concepts,
			"relations": d.Relations, "instances": d.Instances, "impact": d.Impact}, true
	}

	if req.From <= 0 || req.To <= 0 || req.OntologyID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "diff 需 ontology_id + from/to 版本号，或 ontology_id + 内联 spec（草稿对照）"})
		return nil, false
	}
	if _, err := s.Store.GetOntology(req.OntologyID); err != nil {
		writeErr(w, err)
		return nil, false
	}
	vs, err := s.Store.ListVersions(req.OntologyID)
	if err != nil {
		writeErr(w, err)
		return nil, false
	}
	known := make(map[int]bool, len(vs))
	for _, m := range vs {
		known[m.Version] = true
	}
	load := func(v int) (*pkgspec.Spec, bool) {
		if !known[v] {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "版本不存在"})
			return nil, false
		}
		raw, err := s.Store.GetVersionSpec(req.OntologyID, v)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "该版本无 spec 快照"})
			} else {
				writeErr(w, err)
			}
			return nil, false
		}
		var sp pkgspec.Spec
		if err := json.Unmarshal([]byte(raw), &sp); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "版本快照解析失败: " + err.Error()})
			return nil, false
		}
		return &sp, true
	}
	from, ok := load(req.From)
	if !ok {
		return nil, false
	}
	to, ok := load(req.To)
	if !ok {
		return nil, false
	}
	d := buildSpecDiff(from, to)
	return map[string]any{"from_version": req.From, "to_version": req.To, "concepts": d.Concepts,
		"relations": d.Relations, "instances": d.Instances, "impact": d.Impact}, true
}

// toolchainVersion 版本清单；version>0 时附该版 spec 快照原文。
func (s *Server) toolchainVersion(w http.ResponseWriter, req toolchainReq) (map[string]any, bool) {
	if req.OntologyID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "需提供 ontology_id"})
		return nil, false
	}
	versions, err := s.Store.ListVersions(req.OntologyID)
	if err != nil {
		writeErr(w, err)
		return nil, false
	}
	if versions == nil {
		versions = []repo.VersionMeta{}
	}
	result := map[string]any{"versions": versions}
	if req.Version > 0 {
		raw, err := s.Store.GetVersionSpec(req.OntologyID, req.Version)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "该版本无 spec 快照"})
			} else {
				writeErr(w, err)
			}
			return nil, false
		}
		result["spec"] = json.RawMessage(raw)
	}
	return result, true
}
