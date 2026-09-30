package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/companion"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/ontology"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// REQ-216 增量轮（2026-09-30 复查）API 单测：②a bind 校验 / ②b 本体删除伴生拦截 /
// ②c agent 删除伴生数据清理。

func newCompanion216Fixture(t *testing.T, buildMux *http.ServeMux) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	box, err := secrets.LoadKeyFile(filepath.Join(t.TempDir(), ".secret"))
	if err != nil {
		t.Fatal(err)
	}
	var buildSrv *httptest.Server
	if buildMux != nil {
		buildSrv = httptest.NewServer(buildMux)
		t.Cleanup(buildSrv.Close)
	} else {
		buildSrv = httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(buildSrv.Close)
	}
	buildURL := buildSrv.URL
	onto := &ontology.Service{BuildURL: buildURL, RuntimeURL: buildURL, DialTimeout: 2 * time.Second}
	s := &Server{
		Store: st, Box: box, Ontology: onto,
		Companion: companion.NewService(st, box, companion.NewPlanEngines(buildURL, buildURL)),
	}
	return s
}

func doReq216(s *Server, handler http.HandlerFunc, method, target string, body any, pathVals map[string]string) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, target, rd)
	for k, v := range pathVals {
		r.SetPathValue(k, v)
	}
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

// ②a bind 校验：本体不存在 → 400（构建平面 404），不落绑定。
func TestBindValidatesOntologyExists(t *testing.T) {
	s := newCompanion216Fixture(t, nil) // 构建平面恒 404
	if _, err := s.Store.CreateAgent(&store.Agent{ID: "agt-a", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	w := doReq216(s, s.bindCompanionAgent, "POST", "/api/companion/agents/agt-a/bind",
		map[string]any{"ontology_id": "onto_missing"}, map[string]string{"id": "agt-a"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("绑定不存在本体应 400，got %d: %s", w.Code, w.Body.String())
	}
	a, _ := s.Store.GetAgent("agt-a")
	if a.CompanionOntologyID != "" {
		t.Fatalf("校验失败不应落绑定: %s", a.CompanionOntologyID)
	}
}

// ②b 本体删除拦截：有绑定者 → 409 附清单；解绑后透传删除成功。
func TestDeleteOntologyGuard(t *testing.T) {
	var deleted string
	buildMux := http.NewServeMux()
	buildMux.HandleFunc("DELETE /api/ontologies/onto_x", func(w http.ResponseWriter, r *http.Request) {
		deleted = "onto_x"
		_, _ = w.Write([]byte(`{"deleted":"onto_x"}`))
	})
	s := newCompanion216Fixture(t, buildMux)
	if _, err := s.Store.CreateAgent(&store.Agent{ID: "agt-a", Name: "运维甲", CompanionOntologyID: "onto_x"}); err != nil {
		t.Fatal(err)
	}
	// 有绑定者 → 409
	w := doReq216(s, s.deleteOntologyGuard, "DELETE", "/api/ontologies/onto_x", nil, map[string]string{"id": "onto_x"})
	if w.Code != http.StatusConflict {
		t.Fatalf("有绑定者应 409，got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Binders []string `json:"binders"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Binders) != 1 || body.Binders[0] != "运维甲" {
		t.Fatalf("409 应附绑定者清单: %v", body.Binders)
	}
	if deleted != "" {
		t.Fatal("409 路径不应透传删除")
	}
	// 解绑 → 透传删除成功
	if err := s.Store.SetAgentCompanionBinding("agt-a", ""); err != nil {
		t.Fatal(err)
	}
	w = doReq216(s, s.deleteOntologyGuard, "DELETE", "/api/ontologies/onto_x", nil, map[string]string{"id": "onto_x"})
	if w.Code != http.StatusOK {
		t.Fatalf("无绑定者应透传删除 200，got %d: %s", w.Code, w.Body.String())
	}
	if deleted != "onto_x" {
		t.Fatal("无绑定者应透传到构建平面")
	}
}

// ②c agent 删除：候选与游标随删清理（图数据不动——归属本体资产）。
func TestDeleteAgentCleansCompanionData(t *testing.T) {
	s := newCompanion216Fixture(t, nil)
	if _, err := s.Store.CreateAgent(&store.Agent{ID: "agt-gone", Name: "将删", CompanionOntologyID: "onto_y"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.CreateCompanionCandidates([]*store.CompanionCandidate{
		{AgentID: "agt-gone", ConversationID: "c1", Kind: "concept", Name: "X", Status: "pending"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.AdvanceCompanionCursor("c1", "agt-gone", "m1"); err != nil {
		t.Fatal(err)
	}
	w := doReq216(s, s.deleteAgent, "DELETE", "/api/agents/agt-gone", nil, map[string]string{"id": "agt-gone"})
	if w.Code != http.StatusOK {
		t.Fatalf("删除 agent 应 200，got %d: %s", w.Code, w.Body.String())
	}
	if list, _ := s.Store.ListCompanionCandidates("", "agt-gone", ""); len(list) != 0 {
		t.Fatalf("agent 删除后候选应清理，剩 %d", len(list))
	}
	if cur, _ := s.Store.GetCompanionCursor("c1", "agt-gone"); cur.LastMessageID != "" {
		t.Fatalf("agent 删除后游标应清理: %+v", cur)
	}
}
