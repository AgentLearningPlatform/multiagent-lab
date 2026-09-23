package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
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

var _ = json.Marshal
