// Package rest 构建平面 REST（方案 04 §3.6）。
package rest

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"

	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/importer"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/llmcreate"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/ontochat"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/pipeline"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/repo"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/seed"
)

type Server struct {
	Store    *repo.Store
	Sidecar  *importer.Sidecar
	LLM      *llmcreate.Creator
	OntoChat *ontochat.Engine

	ontoChatDB *ontochat.Store   // 惰性初始化（rest_ontochat.go）
	pipelineDB *pipeline.Store   // 惰性初始化（rest_pipeline.go）
}

func New(st *repo.Store, sc *importer.Sidecar, llm *llmcreate.Creator) *Server {
	return &Server{Store: st, Sidecar: sc, LLM: llm, OntoChat: &ontochat.Engine{LLM: llm}}
}

// Mount 注册到主平台兼容的 1.22 pattern mux。
func (s *Server) Mount(m *http.ServeMux) {
	m.HandleFunc("GET /api/ontologies", s.list)
	m.HandleFunc("POST /api/ontologies", s.create)
	m.HandleFunc("GET /api/ontologies/{id}", s.get)
	m.HandleFunc("PUT /api/ontologies/{id}", s.update)
	m.HandleFunc("DELETE /api/ontologies/{id}", s.remove)
	m.HandleFunc("GET /api/ontologies/{id}/spec", s.getSpec)
	m.HandleFunc("PUT /api/ontologies/{id}/spec", s.saveSpec)
	m.HandleFunc("POST /api/ontologies/{id}/validate", s.validate)
	m.HandleFunc("GET /api/ontologies/{id}/artifacts", s.artifacts)
	m.HandleFunc("POST /api/ontologies/import", s.importOntology)
	m.HandleFunc("GET /api/ontologies/{id}/export", s.exportOntology)
	m.HandleFunc("GET /api/ontologies/{id}/guide", s.guide)
	m.HandleFunc("POST /api/ontologies/seed-sample", s.seedSample)
	m.HandleFunc("POST /api/ontologies/ai-draft", s.aiDraft)
	m.HandleFunc("GET /api/ontologies/seed-learning", s.listLearning)
	m.HandleFunc("POST /api/ontologies/seed-learning", s.seedLearning)
	m.HandleFunc("GET /api/ontologies/{id}/versions", s.listVersions)
	m.HandleFunc("GET /api/ontologies/{id}/versions/{version}/original", s.versionOriginal)
	m.HandleFunc("GET /api/ontologies/{id}/diff", s.diffVersions)
	m.HandleFunc("POST /api/ontologies/{id}/ingest-csv", s.ingestCSV)
	m.HandleFunc("GET /api/ontologies/{id}/ingest-mapping", s.getIngestMapping)
	m.HandleFunc("PUT /api/ontologies/{id}/ingest-mapping", s.putIngestMapping)

	// 工具链配置（REQ-75/76，04 §4.6）
	m.HandleFunc("GET /api/pipelines/catalog", s.listToolCatalog)
	m.HandleFunc("GET /api/pipelines", s.listPipelines)
	m.HandleFunc("POST /api/pipelines", s.createPipeline)
	m.HandleFunc("GET /api/pipelines/{id}", s.getPipeline)
	m.HandleFunc("PUT /api/pipelines/{id}", s.updatePipeline)
	m.HandleFunc("POST /api/pipelines/{id}/clone", s.clonePipeline)
	m.HandleFunc("DELETE /api/pipelines/{id}", s.deletePipeline)
	m.HandleFunc("POST /api/pipelines/{id}/check", s.checkPipeline)
	m.HandleFunc("POST /api/ontologies/{id}/fork", s.fork)
	// OntoChat 多轮引导（REQ-103 模式 A）
	m.HandleFunc("GET /api/ontochat/sessions", s.listOntoChatSessions)
	m.HandleFunc("POST /api/ontochat/sessions", s.createOntoChatSession)
	m.HandleFunc("GET /api/ontochat/sessions/{id}", s.getOntoChatSession)
	m.HandleFunc("DELETE /api/ontochat/sessions/{id}", s.deleteOntoChatSession)
	m.HandleFunc("POST /api/ontochat/sessions/{id}/turn", s.ontoChatTurn)
	m.HandleFunc("POST /api/ontochat/sessions/{id}/save", s.ontoChatSave)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, repo.ErrNotFound) {
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(v)
}

// ---- 本体元数据 ----

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListOntologies()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name, Description string }
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name 必填"})
		return
	}
	id := "onto_" + newID()
	o, err := s.Store.CreateOntology(id, req.Name, req.Description)
	if err != nil {
		writeErr(w, err)
		return
	}
	// 新建即给一个空 spec 形态，编辑器可直接编辑
	empty := pkgspec.Spec{Name: req.Name, Description: req.Description}
	bts, _ := json.Marshal(empty)
	_ = s.Store.PutArtifact(id, "spec_json", string(bts), true)
	_ = s.Store.SaveVersion(id, 1, string(bts), "", "")
	writeJSON(w, http.StatusCreated, o)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	o, err := s.Store.GetOntology(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct{ Name, Description string }
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Store.UpdateOntology(id, req.Name, req.Description); err != nil {
		writeErr(w, err)
		return
	}
	o, _ := s.Store.GetOntology(id)
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteOntology(r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("id")})
}

// ---- spec_json 编辑与校验 ----

func (s *Server) getSpec(w http.ResponseWriter, r *http.Request) {
	raw, _, err := s.Store.GetArtifact(r.PathValue("id"), "spec_json")
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) saveSpec(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var sp pkgspec.Spec
	if err := decodeJSON(r, &sp); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	if errs := sp.Validate(); len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "校验未通过，不允许保存坏本体", "validation_errors": errs})
		return
	}
	bts, err := json.Marshal(sp)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Store.PutArtifact(id, "spec_json", string(bts), true); err != nil {
		writeErr(w, err)
		return
	}
	v, _ := s.Store.BumpVersion(id)
	_ = s.Store.SaveVersion(id, v, string(bts), "", "")
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "version": v})
}

func (s *Server) validate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	raw, _, err := s.Store.GetArtifact(id, "spec_json")
	if err != nil {
		writeErr(w, err)
		return
	}
	var sp pkgspec.Spec
	if err := json.Unmarshal([]byte(raw), &sp); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "validation_errors": []map[string]string{{"path": "$", "message": err.Error()}}})
		return
	}
	errs := sp.Validate()
	if errs == nil {
		errs = []pkgspec.ValidationError{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": len(errs) == 0, "validation_errors": errs})
}

func (s *Server) artifacts(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListArtifacts(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// ---- 导入 / 导出 ----

func (s *Server) importOntology(w http.ResponseWriter, r *http.Request) {
	var filename, content, name string
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart 解析失败: " + err.Error()})
			return
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 file 字段"})
			return
		}
		defer f.Close()
		bts, err := io.ReadAll(io.LimitReader(f, 64<<20))
		if err != nil {
			writeErr(w, err)
			return
		}
		filename, content = hdr.Filename, string(bts)
		name = strings.TrimSuffix(r.FormValue("name"), "")
	} else {
		var req struct{ Filename, Content, Name string }
		if err := decodeJSON(r, &req); err != nil || req.Content == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "需要 multipart file 或 JSON {filename, content}"})
			return
		}
		filename, content, name = req.Filename, req.Content, req.Name
	}
	if filename == "" {
		filename = "paste.ttl"
	}
	sp, rep, err := importer.Import(s.Sidecar, filename, content)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 本体记录
	id := "onto_" + newID()
	if name == "" {
		name = sp.Name
	}
	if name == "" {
		name = strings.TrimSuffix(filename, extensionOf(filename))
	}
	if _, err := s.Store.CreateOntology(id, name, sp.Description); err != nil {
		writeErr(w, err)
		return
	}
	// ①原样存档（original 不可变）②归一化 spec_json
	origFormat := originalFormatOf(rep.Format)
	if origFormat != "" {
		_ = s.Store.PutArtifact(id, origFormat, content, false)
	}
	bts, _ := json.Marshal(sp)
	if err := s.Store.PutArtifact(id, "spec_json", string(bts), true); err != nil {
		writeErr(w, err)
		return
	}
	// 导入即首次入库：CreateOntology 初始 version=1，此处不再 Bump（REQ-93 版本语义）
	_ = s.Store.SaveVersion(id, 1, string(bts), origFormat, content)
	o, _ := s.Store.GetOntology(id)
	writeJSON(w, http.StatusCreated, map[string]any{"ontology": o, "report": rep})
}

func extensionOf(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i:]
	}
	return ""
}

func originalFormatOf(format string) string {
	switch format {
	case importer.FormatTurtle:
		return "turtle"
	case importer.FormatOWLRDF:
		return "owl_rdfxml"
	case importer.FormatCSV:
		return "csv"
	case importer.FormatGraphML:
		return "graphml"
	}
	return "" // spec_json 无 original
}

// exportOntology 导出：?format=turtle（spec 经 sidecar 导出并校验）；其他形态直接回原文。
func (s *Server) exportOntology(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "turtle"
	}
	o, err := s.Store.GetOntology(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	switch format {
	case "turtle":
		raw, _, err := s.Store.GetArtifact(id, "spec_json")
		if err != nil {
			writeErr(w, err)
			return
		}
		var sp pkgspec.Spec
		if err := json.Unmarshal([]byte(raw), &sp); err != nil {
			writeErr(w, err)
			return
		}
		ttl, err := importer.ExportTTL(s.Sidecar, id, &sp)
		if err != nil {
			writeErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/turtle; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s_v%d.ttl", o.ID, o.Version))
		_, _ = w.Write([]byte(ttl))
	default:
		raw, _, err := s.Store.GetArtifact(id, format)
		if err != nil {
			writeErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s_v%d.%s", o.ID, o.Version, format))
		_, _ = w.Write([]byte(raw))
	}
}

// ---- guide（主平台 M8 注入用，§5 集成契约）----

func (s *Server) guide(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	o, err := s.Store.GetOntology(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	raw, _, err := s.Store.GetArtifact(id, "spec_json")
	if err != nil {
		writeErr(w, err)
		return
	}
	var sp pkgspec.Spec
	_ = json.Unmarshal([]byte(raw), &sp)

	var b strings.Builder
	fmt.Fprintf(&b, "可用本体【%s】(ontology_id=%s, v%d)：%s\n", o.Name, o.ID, o.Version, o.Description)
	b.WriteString("概念：")
	for i, c := range sp.Concepts {
		if i > 0 {
			b.WriteString("、")
		}
		b.WriteString(c.Name)
	}
	b.WriteString("\n关系：")
	for i, rel := range sp.Relations {
		if i > 0 {
			b.WriteString("、")
		}
		fmt.Fprintf(&b, "%s(%s→%s)", rel.Name, rel.From, rel.To)
	}
	fmt.Fprintf(&b, "\n实例共 %d 个。查询工具使用：get_concept/get_instance 按名称精确查，list_instances 列出某概念全部实例，neighbors 查实例关系邻居。所有工具入参 ontology_id 固定为 %s。", len(sp.Instances), o.ID)
	writeJSON(w, http.StatusOK, map[string]string{"ontology_id": o.ID, "guide": b.String()})
}

// ---- seed / AI 草稿 ----

func (s *Server) seedSample(w http.ResponseWriter, r *http.Request) {
	sp := seed.K8sOpsWithRelations()
	if _, err := s.Store.GetOntology(sp.ID); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"id": sp.ID, "seeded": false, "note": "示例已存在"})
		return
	}
	if _, err := s.Store.CreateOntology(sp.ID, sp.Name, sp.Description); err != nil {
		writeErr(w, err)
		return
	}
	bts, _ := json.Marshal(sp)
	if err := s.Store.PutArtifact(sp.ID, "spec_json", string(bts), true); err != nil {
		writeErr(w, err)
		return
	}
	o, _ := s.Store.GetOntology(sp.ID)
	writeJSON(w, http.StatusCreated, o)
}

func (s *Server) aiDraft(w http.ResponseWriter, r *http.Request) {
	if s.LLM == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "LLM 辅助创建未配置（PLATFORM_URL）"})
		return
	}
	var req struct {
		Description         string   `json:"description"`
		ExtraHint           string   `json:"extra_hint"`
		CapabilityQuestions []string `json:"capability_questions"` // 可选：AI 创建的能力问题引导（§4.8.3）
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Description) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "description 必填"})
		return
	}
	if len(req.CapabilityQuestions) > 0 {
		var b strings.Builder
		b.WriteString("请重点让本体具备回答以下能力问题的潜力（据此补充概念/关系/属性建模）：")
		for i, q := range req.CapabilityQuestions {
			fmt.Fprintf(&b, "\n%d. %s", i+1, strings.TrimSpace(q))
		}
		if req.ExtraHint == "" {
			req.ExtraHint = b.String()
		} else {
			req.ExtraHint += "\n\n" + b.String()
		}
	}
	res, err := s.LLM.Draft(req.Description, req.ExtraHint)
	if res == nil {
		writeErr(w, err)
		return
	}
	out := map[string]any{"spec": res.Spec, "rounds": res.Rounds}
	if res.Usage != nil {
		out["usage"] = res.Usage
	}
	if err != nil {
		out["warning"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- utils ----

// listLearning GET /api/ontologies/seed-learning：列出内置学习示例。
func (s *Server) listLearning(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, seed.LearningExamples())
}

// seedLearning POST /api/ontologies/seed-learning {"key":"defects"}：灌装内置学习示例本体。
func (s *Server) seedLearning(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Key) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key 必填"})
		return
	}
	sp, err := seed.LoadLearningExample(strings.TrimSpace(req.Key))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if _, err := s.Store.GetOntology(sp.ID); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"id": sp.ID, "seeded": false, "note": "示例已存在"})
		return
	}
	if _, err := s.Store.CreateOntology(sp.ID, sp.Name, sp.Description); err != nil {
		writeErr(w, err)
		return
	}
	bts, _ := json.Marshal(sp)
	if err := s.Store.PutArtifact(sp.ID, "spec_json", string(bts), true); err != nil {
		writeErr(w, err)
		return
	}
	_ = s.Store.SaveVersion(sp.ID, 1, string(bts), "", "")
	o, _ := s.Store.GetOntology(sp.ID)
	writeJSON(w, http.StatusCreated, o)
}

// listVersions GET /api/ontologies/{id}/versions：版本历史列表（REQ-93）。
func (s *Server) listVersions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.Store.GetOntology(id); err != nil {
		writeErr(w, err)
		return
	}
	vs, err := s.Store.ListVersions(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if vs == nil {
		vs = []repo.VersionMeta{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ontology_id": id, "versions": vs})
}

// versionOriginal GET /api/ontologies/{id}/versions/{version}/original：按版本读取原始源文件（REQ-93 源码视图）。
func (s *Server) versionOriginal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	v, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || v <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "版本号必须是正整数"})
		return
	}
	content, format, err := s.Store.GetVersionOriginal(id, v)
	if err != nil {
		writeErr(w, err)
		return
	}
	ct := "text/plain; charset=utf-8"
	switch format {
	case "turtle":
		ct = "text/turtle; charset=utf-8"
	case "owl_rdfxml":
		ct = "application/rdf+xml; charset=utf-8"
	case "spec_json":
		ct = "application/json; charset=utf-8"
	case "csv":
		ct = "text/csv; charset=utf-8"
	case "graphml":
		ct = "application/graphml; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Ontology-Version", strconv.Itoa(v))
	_, _ = w.Write([]byte(content))
}

// ---- REQ-95 版本 diff ----

type diffFieldDelta struct {
	From any `json:"from,omitempty"`
	To   any `json:"to,omitempty"`
}

type diffChangedItem struct {
	Name   string                    `json:"name"`
	Fields map[string]diffFieldDelta `json:"fields"`
}

type diffSet struct {
	Added   []any             `json:"added"`
	Removed []any             `json:"removed"`
	Changed []diffChangedItem `json:"changed"`
}

type diffImpact struct {
	Name         string `json:"name"`
	ReferencedBy int    `json:"referenced_by"`
}

func newDiffSet() diffSet {
	return diffSet{Added: []any{}, Removed: []any{}, Changed: []diffChangedItem{}}
}

func strDelta(from, to string) diffFieldDelta {
	d := diffFieldDelta{}
	if from != "" {
		d.From = from
	}
	if to != "" {
		d.To = to
	}
	return d
}

func strSliceDelta(from, to []string) diffFieldDelta {
	d := diffFieldDelta{}
	if len(from) > 0 {
		d.From = from
	}
	if len(to) > 0 {
		d.To = to
	}
	return d
}

func attrsDelta(from, to map[string]any) diffFieldDelta {
	d := diffFieldDelta{}
	if len(from) > 0 {
		d.From = from
	}
	if len(to) > 0 {
		d.To = to
	}
	return d
}

func relsDelta(from, to []pkgspec.InstanceRel) diffFieldDelta {
	d := diffFieldDelta{}
	if len(from) > 0 {
		d.From = from
	}
	if len(to) > 0 {
		d.To = to
	}
	return d
}

// sortedStrings 返回副本排序结果（parents 视为无序集合比较）。
func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as, bs := sortedStrings(a), sortedStrings(b)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// jsonValueEqual 以确定性 JSON 序列化比较任意 JSON 值（map 键有序）。
func jsonValueEqual(a, b any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ab) == string(bb)
}

func attrsEqual(a, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return jsonValueEqual(a, b)
}

func sortedRels(in []pkgspec.InstanceRel) []pkgspec.InstanceRel {
	out := append([]pkgspec.InstanceRel(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rel != out[j].Rel {
			return out[i].Rel < out[j].Rel
		}
		return out[i].Target < out[j].Target
	})
	return out
}

func instanceRelsEqual(a, b []pkgspec.InstanceRel) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return jsonValueEqual(sortedRels(a), sortedRels(b))
}

func diffConcepts(from, to []pkgspec.Concept) diffSet {
	out := newDiffSet()
	fm := make(map[string]pkgspec.Concept, len(from))
	for _, c := range from {
		fm[c.Name] = c
	}
	tm := make(map[string]pkgspec.Concept, len(to))
	for _, c := range to {
		tm[c.Name] = c
	}
	for _, c := range to {
		if _, ok := fm[c.Name]; !ok {
			out.Added = append(out.Added, c)
		}
	}
	for _, c := range from {
		if _, ok := tm[c.Name]; !ok {
			out.Removed = append(out.Removed, c)
		}
	}
	for _, c := range to {
		o, ok := fm[c.Name]
		if !ok {
			continue
		}
		fields := map[string]diffFieldDelta{}
		if o.Label != c.Label {
			fields["label"] = strDelta(o.Label, c.Label)
		}
		if o.Definition != c.Definition {
			fields["definition"] = strDelta(o.Definition, c.Definition)
		}
		if !stringSlicesEqual(o.Parents, c.Parents) {
			fields["parents"] = strSliceDelta(o.Parents, c.Parents)
		}
		if len(fields) > 0 {
			out.Changed = append(out.Changed, diffChangedItem{Name: c.Name, Fields: fields})
		}
	}
	return out
}

func diffRelations(from, to []pkgspec.Relation) diffSet {
	out := newDiffSet()
	fm := make(map[string]pkgspec.Relation, len(from))
	for _, r := range from {
		fm[r.Name] = r
	}
	tm := make(map[string]pkgspec.Relation, len(to))
	for _, r := range to {
		tm[r.Name] = r
	}
	for _, r := range to {
		if _, ok := fm[r.Name]; !ok {
			out.Added = append(out.Added, r)
		}
	}
	for _, r := range from {
		if _, ok := tm[r.Name]; !ok {
			out.Removed = append(out.Removed, r)
		}
	}
	for _, r := range to {
		o, ok := fm[r.Name]
		if !ok {
			continue
		}
		fields := map[string]diffFieldDelta{}
		if o.Label != r.Label {
			fields["label"] = strDelta(o.Label, r.Label)
		}
		if o.Definition != r.Definition {
			fields["definition"] = strDelta(o.Definition, r.Definition)
		}
		if o.From != r.From {
			fields["from"] = strDelta(o.From, r.From)
		}
		if o.To != r.To {
			fields["to"] = strDelta(o.To, r.To)
		}
		if len(fields) > 0 {
			out.Changed = append(out.Changed, diffChangedItem{Name: r.Name, Fields: fields})
		}
	}
	return out
}

func diffInstances(from, to []pkgspec.Instance) diffSet {
	out := newDiffSet()
	fm := make(map[string]pkgspec.Instance, len(from))
	for _, it := range from {
		fm[it.Name] = it
	}
	tm := make(map[string]pkgspec.Instance, len(to))
	for _, it := range to {
		tm[it.Name] = it
	}
	for _, it := range to {
		if _, ok := fm[it.Name]; !ok {
			out.Added = append(out.Added, it)
		}
	}
	for _, it := range from {
		if _, ok := tm[it.Name]; !ok {
			out.Removed = append(out.Removed, it)
		}
	}
	for _, it := range to {
		o, ok := fm[it.Name]
		if !ok {
			continue
		}
		fields := map[string]diffFieldDelta{}
		if o.Concept != it.Concept {
			fields["concept"] = strDelta(o.Concept, it.Concept)
		}
		if !attrsEqual(o.Attributes, it.Attributes) {
			fields["attributes"] = attrsDelta(o.Attributes, it.Attributes)
		}
		if !instanceRelsEqual(o.Relations, it.Relations) {
			fields["relations"] = relsDelta(o.Relations, it.Relations)
		}
		if len(fields) > 0 {
			out.Changed = append(out.Changed, diffChangedItem{Name: it.Name, Fields: fields})
		}
	}
	return out
}

// conceptReferences 统计 TO 版本中被引用的概念名（relations.from/to + instances.concept + 其他概念 parents）。
func conceptReferences(to pkgspec.Spec) map[string]int {
	refs := map[string]int{}
	for _, r := range to.Relations {
		refs[r.From]++
		refs[r.To]++
	}
	for _, it := range to.Instances {
		refs[it.Concept]++
	}
	for _, c := range to.Concepts {
		for _, p := range c.Parents {
			refs[p]++
		}
	}
	return refs
}

func parseVersionQuery(r *http.Request, key string) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

// diffVersions GET /api/ontologies/{id}/diff?from={v1}&to={v2}：版本 spec 三集结构 diff（REQ-95）。
func (s *Server) diffVersions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.Store.GetOntology(id); err != nil {
		writeErr(w, err)
		return
	}
	fromV, ok := parseVersionQuery(r, "from")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from 参数必须是正整数"})
		return
	}
	toV, ok := parseVersionQuery(r, "to")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "to 参数必须是正整数"})
		return
	}
	vs, err := s.Store.ListVersions(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	known := make(map[int]bool, len(vs))
	for _, m := range vs {
		known[m.Version] = true
	}
	load := func(v int) (*pkgspec.Spec, bool) {
		if !known[v] {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("版本 %d 不存在", v)})
			return nil, false
		}
		raw, err := s.Store.GetVersionSpec(id, v)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("版本 %d 无 spec 快照", v)})
			} else {
				writeErr(w, err)
			}
			return nil, false
		}
		var sp pkgspec.Spec
		if err := json.Unmarshal([]byte(raw), &sp); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("版本 %d 的 spec 快照解析失败: %s", v, err.Error())})
			return nil, false
		}
		return &sp, true
	}
	fromSpec, ok := load(fromV)
	if !ok {
		return
	}
	toSpec, ok := load(toV)
	if !ok {
		return
	}

	concepts := diffConcepts(fromSpec.Concepts, toSpec.Concepts)
	relations := diffRelations(fromSpec.Relations, toSpec.Relations)
	instances := diffInstances(fromSpec.Instances, toSpec.Instances)

	refs := conceptReferences(*toSpec)
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

	writeJSON(w, http.StatusOK, map[string]any{
		"from_version": fromV,
		"to_version":   toV,
		"concepts":     concepts,
		"relations":    relations,
		"instances":    instances,
		"impact":       impact,
	})
}

// ---- REQ-96 数据灌装（CSV → 实例） ----

// csvList 兼容 JSON 数组与逗号分隔字符串两种列清单写法。
type csvList []string

func (l *csvList) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*l = splitCSVList(s)
		return nil
	}
	var arr []string
	if err := json.Unmarshal(b, &arr); err != nil {
		return err
	}
	*l = csvList(arr)
	return nil
}

func splitCSVList(s string) csvList {
	out := csvList{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ingestConfig 灌装配置（REQ-96）：P2a 同名映射 + P2b 映射向导增量。
// TypeRules 列名→转换类型（int/number/date/bool，缺省 string 原样）；
// MultiValueSep 关系列多值分隔符（空 = 整格单值，保持 P2a 语义）。
type ingestConfig struct {
	Concept          string            `json:"concept"`
	KeyColumn        string            `json:"key_column"`
	RelationColumns  csvList           `json:"relation_columns"`
	AttributeColumns csvList           `json:"attribute_columns"`
	SkipRows         int               `json:"skip_rows"`
	TypeRules        map[string]string `json:"type_rules,omitempty"`
	MultiValueSep    string            `json:"multi_value_sep,omitempty"`
}

// convertCell P2b 类型转换：合法返回转换值，非法返回 error（由调用方记 warning 并降级为原字符串）。
func convertCell(col, v string, rules map[string]string) (any, error) {
	switch rules[col] {
	case "int":
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("不是整数")
		}
		return n, nil
	case "number":
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("不是数值")
		}
		return f, nil
	case "bool":
		switch strings.ToLower(v) {
		case "true", "1", "yes", "y", "是":
			return true, nil
		case "false", "0", "no", "n", "否":
			return false, nil
		}
		return nil, fmt.Errorf("不是布尔值")
	case "date":
		for _, layout := range []string{"2006-01-02", "2006/01/02", "2006-01-02 15:04:05", "20060102"} {
			if t, err := time.Parse(layout, v); err == nil {
				return t.Format("2006-01-02"), nil
			}
		}
		return nil, fmt.Errorf("不是日期（支持 2006-01-02 / 2006/01/02 / 20060102）")
	}
	return v, nil // string / 未配置：原样
}

type ingestStats struct {
	RowsRead           int `json:"rows_read"`
	InstancesGenerated int `json:"instances_generated"`
	SkippedEmptyKey    int `json:"skipped_empty_key"`
}

// ingestDraft 预览条目：attributes/relations 恒为非空容器，保证响应形状稳定。
type ingestDraft struct {
	Name       string                `json:"name"`
	Concept    string                `json:"concept"`
	Attributes map[string]any        `json:"attributes"`
	Relations  []pkgspec.InstanceRel `json:"relations"`
}

// ingestCSV POST /api/ontologies/{id}/ingest-csv?mode=preview|apply：CSV → 实例草稿/入库（REQ-96 P2a）。
func (s *Server) ingestCSV(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	if mode == "" {
		mode = "preview"
	}
	if mode != "preview" && mode != "apply" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode 只能是 preview 或 apply"})
		return
	}
	if _, err := s.Store.GetOntology(id); err != nil {
		writeErr(w, err)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "需要 multipart/form-data（csv 文件 + 配置字段）"})
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart 解析失败: " + err.Error()})
		return
	}

	// 配置：优先 config JSON part，否则逐字段 form value。
	var cfg ingestConfig
	cfgRaw := strings.TrimSpace(r.FormValue("config"))
	if cfgRaw == "" && r.MultipartForm != nil {
		if fhs := r.MultipartForm.File["config"]; len(fhs) > 0 {
			if cf, err := fhs[0].Open(); err == nil {
				if cb, err := io.ReadAll(io.LimitReader(cf, 1<<20)); err == nil {
					cfgRaw = strings.TrimSpace(string(cb))
				}
				_ = cf.Close()
			}
		}
	}
	if cfgRaw != "" {
		if err := json.Unmarshal([]byte(cfgRaw), &cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "config JSON 解析失败: " + err.Error()})
			return
		}
	} else {
		cfg.Concept = strings.TrimSpace(r.FormValue("concept"))
		cfg.KeyColumn = strings.TrimSpace(r.FormValue("key_column"))
		cfg.RelationColumns = splitCSVList(r.FormValue("relation_columns"))
		cfg.AttributeColumns = splitCSVList(r.FormValue("attribute_columns"))
		if sr := strings.TrimSpace(r.FormValue("skip_rows")); sr != "" {
			n, err := strconv.Atoi(sr)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "skip_rows 必须是整数"})
				return
			}
			cfg.SkipRows = n
		}
	}
	cfg.Concept = strings.TrimSpace(cfg.Concept)
	cfg.KeyColumn = strings.TrimSpace(cfg.KeyColumn)
	if cfg.Concept == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "concept 必填"})
		return
	}
	if cfg.KeyColumn == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key_column 必填"})
		return
	}
	if cfg.SkipRows < 0 {
		cfg.SkipRows = 0
	}

	// CSV 文件（字段名 csv，兼容 file）。
	f, _, err := r.FormFile("csv")
	if err != nil {
		f, _, err = r.FormFile("file")
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 csv 文件字段"})
		return
	}
	defer f.Close()
	bts, err := io.ReadAll(io.LimitReader(f, 64<<20))
	if err != nil {
		writeErr(w, err)
		return
	}
	cr := csv.NewReader(strings.NewReader(string(bts)))
	cr.FieldsPerRecord = -1 // 容忍参差不齐的行
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "CSV 解析失败: " + err.Error()})
		return
	}
	if cfg.SkipRows >= len(records) {
		records = nil
	} else {
		records = records[cfg.SkipRows:]
	}
	if len(records) < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "CSV 无表头/数据行"})
		return
	}
	header, data := records[0], records[1:]

	colIdx := map[string]int{}
	for i, h := range header {
		h = strings.TrimSpace(h)
		if i == 0 {
			h = strings.TrimPrefix(h, "\ufeff") // 去 BOM
		}
		if h != "" {
			if _, dup := colIdx[h]; !dup {
				colIdx[h] = i
			}
		}
	}
	keyIdx, ok := colIdx[cfg.KeyColumn]
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("主键列 %s 不存在", cfg.KeyColumn)})
		return
	}

	// 目标概念必须已存在（preview/apply 同样校验）。
	rawSpec, _, err := s.Store.GetArtifact(id, "spec_json")
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("概念 %s 不存在", cfg.Concept)})
			return
		}
		writeErr(w, err)
		return
	}
	var sp pkgspec.Spec
	if err := json.Unmarshal([]byte(rawSpec), &sp); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "spec 解析失败: " + err.Error()})
		return
	}
	conceptSet := map[string]bool{}
	for _, c := range sp.Concepts {
		conceptSet[c.Name] = true
	}
	if !conceptSet[cfg.Concept] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("概念 %s 不存在", cfg.Concept)})
		return
	}

	warnings := []string{}
	relCols := []string{}
	for _, c := range cfg.RelationColumns {
		if _, ok := colIdx[c]; !ok {
			warnings = append(warnings, fmt.Sprintf("关系列 %s 不存在，已忽略", c))
			continue
		}
		relCols = append(relCols, c)
	}
	attrCols := []string{}
	for _, c := range cfg.AttributeColumns {
		if _, ok := colIdx[c]; !ok {
			warnings = append(warnings, fmt.Sprintf("属性列 %s 不存在，已忽略", c))
			continue
		}
		attrCols = append(attrCols, c)
	}

	stats := ingestStats{}
	draft := []pkgspec.Instance{}
	seenKey := map[string]bool{}
	for ri, row := range data {
		rowNo := cfg.SkipRows + 2 + ri // 1-based（跳过行 + 表头）
		stats.RowsRead++
		cell := func(i int) string {
			if i >= 0 && i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		name := cell(keyIdx)
		if name == "" {
			stats.SkippedEmptyKey++
			warnings = append(warnings, fmt.Sprintf("第 %d 行主键为空，已跳过", rowNo))
			continue
		}
		if seenKey[name] {
			warnings = append(warnings, fmt.Sprintf("主键重复 %s（第 %d 行），已跳过", name, rowNo))
			continue
		}
		seenKey[name] = true
		inst := pkgspec.Instance{Name: name, Concept: cfg.Concept}
		for _, c := range attrCols {
			if v := cell(colIdx[c]); v != "" {
				cv, err := convertCell(c, v, cfg.TypeRules)
				if err != nil {
					cv = v // 降级：非法值按原字符串保留
					warnings = append(warnings, fmt.Sprintf("第 %d 行属性列 %s 值 %q %v，按原字符串保留", rowNo, c, v, err))
				}
				if inst.Attributes == nil {
					inst.Attributes = map[string]any{}
				}
				inst.Attributes[c] = cv
			}
		}
		for _, c := range relCols {
			v := cell(colIdx[c])
			if v == "" {
				warnings = append(warnings, fmt.Sprintf("第 %d 行关系列 %s 目标为空，已跳过", rowNo, c))
				continue
			}
			// P2b 多值分隔符：一格多个目标（如 "D1;D2"）拆成多条断言。
			targets := []string{v}
			if cfg.MultiValueSep != "" {
				targets = nil
				for _, p := range strings.Split(v, cfg.MultiValueSep) {
					if p = strings.TrimSpace(p); p != "" {
						targets = append(targets, p)
					}
				}
				if len(targets) == 0 {
					warnings = append(warnings, fmt.Sprintf("第 %d 行关系列 %s 拆分后无有效目标，已跳过", rowNo, c))
					continue
				}
			}
			for _, t := range targets {
				inst.Relations = append(inst.Relations, pkgspec.InstanceRel{Rel: c, Target: t})
			}
		}
		draft = append(draft, inst)
		stats.InstancesGenerated++
	}

	if mode == "preview" {
		limit := len(draft)
		if limit > 20 {
			limit = 20
		}
		preview := make([]ingestDraft, 0, limit)
		for _, it := range draft[:limit] {
			attrs := it.Attributes
			if attrs == nil {
				attrs = map[string]any{}
			}
			rels := it.Relations
			if rels == nil {
				rels = []pkgspec.InstanceRel{}
			}
			preview = append(preview, ingestDraft{Name: it.Name, Concept: it.Concept, Attributes: attrs, Relations: rels})
		}
		writeJSON(w, http.StatusOK, map[string]any{"stats": stats, "warnings": warnings, "draft": preview})
		return
	}

	// apply：合并进当前 spec → 校验 → 存为新版本。
	existing := map[string]bool{}
	for _, it := range sp.Instances {
		existing[it.Name] = true
	}
	for _, inst := range draft {
		if existing[inst.Name] {
			warnings = append(warnings, fmt.Sprintf("实例 %s 已存在，已跳过", inst.Name))
			continue
		}
		sp.Instances = append(sp.Instances, inst)
		existing[inst.Name] = true
	}
	if errs := sp.Validate(); len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "校验未通过，不允许保存坏本体", "validation_errors": errs})
		return
	}
	bts2, err := json.Marshal(sp)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Store.PutArtifact(id, "spec_json", string(bts2), true); err != nil {
		writeErr(w, err)
		return
	}
	v, _ := s.Store.BumpVersion(id)
	_ = s.Store.SaveVersion(id, v, string(bts2), "", "")
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "version": v, "stats": stats, "warnings": warnings})
}

// ---- REQ-96 P2b 映射配置存取 ----

// getIngestMapping GET /api/ontologies/{id}/ingest-mapping：读取已保存的映射配置（无则 404）。
// 存 artifact format=ingest_mapping_json：不 bump version、不进产物列表（ListArtifacts 过滤）、不随 fork 复制。
func (s *Server) getIngestMapping(w http.ResponseWriter, r *http.Request) {
	raw, _, err := s.Store.GetArtifact(r.PathValue("id"), "ingest_mapping_json")
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(raw))
}

// putIngestMapping PUT /api/ontologies/{id}/ingest-mapping：保存映射配置（结构即 ingestConfig 的 JSON）。
func (s *Server) putIngestMapping(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.Store.GetOntology(id); err != nil {
		writeErr(w, err)
		return
	}
	var cfg ingestConfig
	if err := decodeJSON(r, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	if strings.TrimSpace(cfg.Concept) == "" || strings.TrimSpace(cfg.KeyColumn) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "concept 与 key_column 必填"})
		return
	}
	bts, err := json.Marshal(cfg)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Store.PutArtifact(id, "ingest_mapping_json", string(bts), true); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true})
}

// ---- REQ-83 fork ----

// fork POST /api/ontologies/{id}/fork：复制 spec 与全部形态资产派生新本体（REQ-83）。
func (s *Server) fork(w http.ResponseWriter, r *http.Request) {
	srcID := r.PathValue("id")
	src, err := s.Store.GetOntology(srcID)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct{ Name, Description string }
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = src.Name + "（副本）"
	}
	desc := req.Description
	if strings.TrimSpace(desc) == "" {
		desc = src.Description
	}
	newOntoID := "onto_" + newID()
	if _, err := s.Store.CreateOntologyFork(newOntoID, name, desc, srcID); err != nil {
		writeErr(w, err)
		return
	}
	arts, err := s.Store.ListArtifactContents(srcID)
	if err != nil {
		writeErr(w, err)
		return
	}
	specJSON := ""
	for _, a := range arts {
		if a.Format == "spec_json" {
			specJSON = a.Content
			continue
		}
		if err := s.Store.PutArtifact(newOntoID, a.Format, a.Content, a.Normalized); err != nil {
			writeErr(w, err)
			return
		}
	}
	if specJSON == "" {
		// 回退：最新版本快照
		if vs, e := s.Store.ListVersions(srcID); e == nil && len(vs) > 0 {
			if raw, e2 := s.Store.GetVersionSpec(srcID, vs[len(vs)-1].Version); e2 == nil {
				specJSON = raw
			}
		}
	}
	if specJSON == "" {
		empty := pkgspec.Spec{Name: name, Description: desc}
		b, _ := json.Marshal(empty)
		specJSON = string(b)
	}
	if err := s.Store.PutArtifact(newOntoID, "spec_json", specJSON, true); err != nil {
		writeErr(w, err)
		return
	}
	_ = s.Store.SaveVersion(newOntoID, 1, specJSON, "", "")
	o, err := s.Store.GetOntology(newOntoID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

func newID() string {
	return strings.ReplaceAll(time.Now().UTC().Format("060102150405.000000000"), ".", "")
}
