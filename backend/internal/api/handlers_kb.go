package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/ontobuild"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---- Knowledge Base（M6，方案 §450）----

type kbServer struct{ s *Server }

func (s *Server) kb() kbServer { return kbServer{s} }

func (s *Server) listKB(w http.ResponseWriter, r *http.Request) {
	kbs, err := s.Store.ListKnowledgeBases()
	if err != nil {
		writeErr(w, err)
		return
	}
	if kbs == nil {
		kbs = []*store.KnowledgeBase{}
	}
	writeJSON(w, http.StatusOK, kbs)
}

func (s *Server) createKB(w http.ResponseWriter, r *http.Request) {
	var k store.KnowledgeBase
	if err := decodeJSON(r, &k); err != nil {
		writeErr(w, err)
		return
	}
	created, err := s.Store.CreateKnowledgeBase(&k)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) getKB(w http.ResponseWriter, r *http.Request) {
	k, err := s.Store.GetKnowledgeBase(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, k)
}

func (s *Server) updateKB(w http.ResponseWriter, r *http.Request) {
	var k store.KnowledgeBase
	if err := decodeJSON(r, &k); err != nil {
		writeErr(w, err)
		return
	}
	k.ID = r.PathValue("id")
	updated, err := s.Store.UpdateKnowledgeBase(&k)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteKB(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.KB.DeleteKB(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (s *Server) listKBDocs(w http.ResponseWriter, r *http.Request) {
	docs, err := s.Store.ListKnowledgeDocs(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if docs == nil {
		docs = []*store.KnowledgeDoc{}
	}
	writeJSON(w, http.StatusOK, docs)
}

// importKBDoc 粘贴文本导入并同步索引（§341 流程：切分→embed→upsert→状态回写）。
func (s *Server) importKBDoc(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(in.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = truncateTitle(in.Content)
	}
	doc, err := s.KB.Import(r.Context(), r.PathValue("id"), title, in.Content)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) deleteKBDoc(w http.ResponseWriter, r *http.Request) {
	kbID, docID := r.PathValue("id"), r.PathValue("did")
	if err := s.KB.DeleteDoc(r.Context(), kbID, docID); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": docID})
}

func (s *Server) reindexKBDoc(w http.ResponseWriter, r *http.Request) {
	doc, err := s.KB.Reindex(r.Context(), r.PathValue("id"), r.PathValue("did"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// previewKBSearch 检索试运行（§490：TopK/得分回显；M14 ④：按 KB mode 走 GraphRAG/向量，降级标注）。
func (s *Server) previewKBSearch(w http.ResponseWriter, r *http.Request) {
	k, err := s.Store.GetKnowledgeBase(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Query    string   `json:"query"`
		TopK     int      `json:"top_k"`
		MinScore *float64 `json:"min_score"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(in.Query) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	minScore := k.MinScore
	if in.MinScore != nil {
		minScore = *in.MinScore
	}
	hits, mode, degraded, err := s.KB.GraphragQueryWithFallback(r.Context(), k, in.Query, in.TopK, minScore)
	if err != nil {
		writeErr(w, err)
		return
	}
	if hits == nil {
		hits = []kb.RetrievalHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"kb_id": k.ID, "mode": mode, "hits": hits, "degraded": degraded})
}

// graphragSearchKB GraphRAG 子模块直查（M14 ③：POST /api/kbs/{id}/graphrag-search → worker /query）。
// worker 不可达时 200 + degraded:true（M14 ⑥ 非阻断降级语义，供 UI 友好提示）。
func (s *Server) graphragSearchKB(w http.ResponseWriter, r *http.Request) {
	k, err := s.Store.GetKnowledgeBase(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(in.Query) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	hits, err := s.KB.GraphragQuery(r.Context(), k, in.Query, in.MaxResults)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"kb_id": k.ID, "mode": "graphrag", "degraded": true,
			"error": "semantica worker 不可达，GraphRAG 检索降级: " + err.Error(),
			"hits":  []kb.RetrievalHit{},
		})
		return
	}
	if hits == nil {
		hits = []kb.RetrievalHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"kb_id": k.ID, "mode": "graphrag", "degraded": false, "hits": hits})
}

func truncateTitle(s string) string {
	rs := []rune(strings.TrimSpace(s))
	if len(rs) > 30 {
		rs = rs[:30]
	}
	return string(rs)
}

// ---- O13（D-O14/REQ-108）：由知识库构建本体——构建栏第六路径 4 端点 ----
// 路由用「方法 + 精确路径」注册在反代前缀（/api/ontologies*、/api/semantica*）之上，
// ServeMux 最长优先匹配会选中这里的精确模式，构建平面/worker 零侵入（04 §3.7）。

// selectableForOntologyBuild GET /api/kbs/selectable-for-ontology-build
// KB 选择器：mode 徽标 + chunk 数 / KG 实体关系数预览。
func (s *Server) selectableForOntologyBuild(w http.ResponseWriter, r *http.Request) {
	items, err := s.OntoBuild.SelectableKBs()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// buildFromKB POST /api/ontologies/build-from-kb
// {kb_id, strategy: chunk-llm|kg-direct|hybrid, cq_mode: auto|custom|skip, custom_cqs?, conn_id?}
// → {spec_json, validation_report, cqs, rounds, ...}（草稿预览用；入库走构建平面 POST /api/ontologies + PUT spec）。
func (s *Server) buildFromKB(w http.ResponseWriter, r *http.Request) {
	var in struct {
		KBID      string   `json:"kb_id"`
		Strategy  string   `json:"strategy"`
		CQMode    string   `json:"cq_mode"`
		CustomCQs []string `json:"custom_cqs"`
		ConnID    string   `json:"conn_id"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(in.KBID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kb_id is required"})
		return
	}
	if in.CQMode == "custom" && len(in.CustomCQs) == 0 { // 与 REQ-90 自定义模式一致：选了自定义必须给问题
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cq_mode=custom 需提供 custom_cqs（每行一个问题）"})
		return
	}
	res, err := s.OntoBuild.BuildFromKB(r.Context(), in.KBID, in.Strategy, in.CQMode, in.CustomCQs, in.ConnID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// chunksToKG POST /api/semantica/chunks-to-kg
// {kb_id} → 把该 KB 的 chunk 池推给 semantica worker /graphrag/ingest 建 KG
// （graphrag 模式导入时已自动；此处供 rag 模式 KB / 显式重建，M15 ④ 薄封装）。
func (s *Server) chunksToKG(w http.ResponseWriter, r *http.Request) {
	var in struct {
		KBID string `json:"kb_id"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(in.KBID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kb_id is required"})
		return
	}
	info, n, err := s.OntoBuild.ChunksToKG(r.Context(), in.KBID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kb_id": in.KBID, "chunks": n, "graphrag": info})
}

// kgToSpecJSON POST /api/ontologies/kg-to-spec-json
// {kb_id} → 回读 worker KG 做薄映射（entity→Concept / relation→Relation / HAS→具有），
// 不做语义抽取（04 §3.7 <300 行约定）；策略 B 的独立入口（策略 C 走 build-from-kb）。
func (s *Server) kgToSpecJSON(w http.ResponseWriter, r *http.Request) {
	var in struct {
		KBID string `json:"kb_id"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(in.KBID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kb_id is required"})
		return
	}
	k, err := s.Store.GetKnowledgeBase(in.KBID)
	if err != nil {
		writeErr(w, err)
		return
	}
	kg, err := s.OntoBuild.KB.GraphragKG(r.Context(), in.KBID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "KG 回读失败（semantica worker 不可达或该库无 KG）: " + err.Error()})
		return
	}
	if len(kg.Entities) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "该知识库尚无 KG（可先 POST /api/semantica/chunks-to-kg 构建）"})
		return
	}
	spec := ontobuild.MapKGToSpec(kg, k.Name)
	writeJSON(w, http.StatusOK, map[string]any{
		"kb_id": in.KBID, "method": kg.Method, "kg_entities": len(kg.Entities), "kg_relationships": len(kg.Relationships),
		"spec_json": spec, "validation_report": ontobuild.ValidateBuildSpec(spec),
	})
}

var _ = json.Marshal
