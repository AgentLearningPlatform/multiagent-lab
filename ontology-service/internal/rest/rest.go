// Package rest 构建平面 REST（方案 04 §3.6）。
package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"

	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/importer"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/llmcreate"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/repo"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/seed"
)

type Server struct {
	Store   *repo.Store
	Sidecar *importer.Sidecar
	LLM     *llmcreate.Creator
}

func New(st *repo.Store, sc *importer.Sidecar, llm *llmcreate.Creator) *Server {
	return &Server{Store: st, Sidecar: sc, LLM: llm}
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
	v, _ := s.Store.BumpVersion(id)
	if origFormat != "" {
		_ = s.Store.SaveVersion(id, v, string(bts), origFormat, content)
	} else {
		_ = s.Store.SaveVersion(id, v, string(bts), "", "")
	}
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
	var req struct{ Description, ExtraHint string }
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Description) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "description 必填"})
		return
	}
	res, err := s.LLM.Draft(req.Description, req.ExtraHint)
	if res == nil {
		writeErr(w, err)
		return
	}
	out := map[string]any{"spec": res.Spec, "rounds": res.Rounds}
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
	var v int
	if _, err := fmt.Sscanf(r.PathValue("version"), "%d", &v); err != nil || v <= 0 {
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
		ct = "application/xml; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	_, _ = w.Write([]byte(content))
}

func newID() string {
	return strings.ReplaceAll(time.Now().UTC().Format("060102150405.000000000"), ".", "")
}
