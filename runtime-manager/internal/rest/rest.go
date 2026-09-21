// Package rest 运行平面 REST（方案 04 §4.5）。
package rest

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/manager"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/store"
)

type Server struct {
	Store   *store.Store
	Manager *manager.Manager
}

func New(st *store.Store, mg *manager.Manager) *Server { return &Server{Store: st, Manager: mg} }

func (s *Server) Mount(m *http.ServeMux) {
	m.HandleFunc("GET /api/runtime-profiles", s.list)
	m.HandleFunc("POST /api/runtime-profiles", s.create)
	m.HandleFunc("GET /api/runtime-profiles/{id}", s.get)
	m.HandleFunc("PUT /api/runtime-profiles/{id}", s.update)
	m.HandleFunc("DELETE /api/runtime-profiles/{id}", s.remove)
	m.HandleFunc("POST /api/runtime-profiles/{id}/start", s.start)
	m.HandleFunc("POST /api/runtime-profiles/{id}/stop", s.stop)
	m.HandleFunc("POST /api/runtime-profiles/{id}/reload", s.reload)
	m.HandleFunc("GET /api/runtime-profiles/{id}/logs", s.logs)
	m.HandleFunc("GET /healthz", s.healthz)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, store.ErrNotFound) {
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.List()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string   `json:"name"`
		Engine      string   `json:"engine"`
		OntologyIDs []string `json:"ontology_ids"`
		Config      string   `json:"config"`
		Port        int      `json:"port"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name 必填"})
		return
	}
	if req.Engine == "" {
		req.Engine = "oxigraph" // P1 唯一引擎
	}
	if req.Engine != "oxigraph" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "P1 仅支持 oxigraph 引擎（fuseki/memory_graph 为 P2）"})
		return
	}
	if req.Config == "" {
		req.Config = "{}"
	}
	id := "rt_" + newID()
	p := &store.Profile{ID: id, Name: req.Name, Engine: req.Engine, OntologyIDs: req.OntologyIDs, Config: req.Config, Port: req.Port}
	if err := s.Store.Create(p); err != nil {
		writeErr(w, err)
		return
	}
	out, _ := s.Store.Get(id)
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Name        string   `json:"name"`
		OntologyIDs []string `json:"ontology_ids"`
		Config      string   `json:"config"`
		Port        int      `json:"port"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	// 配置变更需重启生效：running 方案先停
	p, err := s.Store.Get(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if p.Status == "running" || p.Status == "starting" {
		if err := s.Manager.Stop(id); err != nil {
			writeErr(w, err)
			return
		}
	}
	if err := s.Store.UpdateMeta(id, req.Name, orDefault(req.Config, p.Config), req.OntologyIDs, req.Port); err != nil {
		writeErr(w, err)
		return
	}
	out, _ := s.Store.Get(id)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.Store.Get(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if p.Status == "running" || p.Status == "starting" {
		_ = s.Manager.Stop(id)
	}
	if err := s.Store.Delete(id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Manager.Start(r.Context(), id); err != nil {
		// 失败细节已落库（status=error + last_error），返回当前快照
		p, _ := s.Store.Get(id)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "profile": p})
		return
	}
	out, _ := s.Store.Get(id)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	if err := s.Manager.Stop(r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	out, _ := s.Store.Get(r.PathValue("id"))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) reload(w http.ResponseWriter, r *http.Request) {
	if err := s.Manager.Reload(r.Context(), r.PathValue("id")); err != nil {
		p, _ := s.Store.Get(r.PathValue("id"))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "profile": p})
		return
	}
	out, _ := s.Store.Get(r.PathValue("id"))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail <= 0 {
		tail = 200
	}
	lines, err := s.Manager.Logs(r.PathValue("id"), tail)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.List()
	if err != nil {
		writeErr(w, err)
		return
	}
	summaries := []map[string]any{}
	for _, p := range list {
		summaries = append(summaries, map[string]any{"id": p.ID, "status": p.Status, "engine": p.Engine, "port": p.Port})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "profiles": summaries})
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func newID() string {
	return strings.ReplaceAll(time.Now().UTC().Format("060102150405.000000000"), ".", "")
}
