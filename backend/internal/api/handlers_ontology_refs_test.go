package api

// REQ-233/M60 本体资产治理面——单测：
// ①「被引用」三源聚合（running/stopped 方案过滤、KB 约束词表、伴生绑定、运行平面不可达降级 warnings）；
// ②删除保护扩展（running 方案引用 409 附引用者清单；stopped 引用放行透传构建平面）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/companion"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/ontology"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func newOntoRefsFixture(t *testing.T, runtimeHandler http.Handler, buildHandler http.Handler) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rtURL, buildURL := "http://127.0.0.1:1", "http://127.0.0.1:1" // 默认不可达
	if runtimeHandler != nil {
		rt := httptest.NewServer(runtimeHandler)
		t.Cleanup(rt.Close)
		rtURL = rt.URL
	}
	if buildHandler != nil {
		bd := httptest.NewServer(buildHandler)
		t.Cleanup(bd.Close)
		buildURL = bd.URL
	}
	return &Server{
		Store:     st,
		Ontology:  &ontology.Service{RuntimeURL: rtURL, BuildURL: buildURL},
		Companion: companion.NewService(st, nil, nil),
	}
}

// stubRuntime 返回固定方案清单 handler（:8090 list 最小投影形态；直接以 http.Handler
// 传 fixture——httptest.Server 在 go1.25 无导出 Handler 字段，包装无益）。
func stubRuntime(t *testing.T, profiles string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(profiles))
	})
}

func TestOntologyReferencesAggregation(t *testing.T) {
	rtH := stubRuntime(t, `[{"id":"rt_1","name":"oxigraph-1","engine":"oxigraph","ontology_ids":["onto_a","onto_b"],"status":"running"},{"id":"rt_2","name":"stopped-plan","engine":"oxigraph","ontology_ids":["onto_a"],"status":"stopped"},{"id":"rt_3","name":"other","engine":"oxigraph","ontology_ids":["onto_c"],"status":"running"}]`)
	s := newOntoRefsFixture(t, rtH, nil)

	if _, err := s.Store.CreateKnowledgeBase(&store.KnowledgeBase{ID: "kb_1", Name: "词表库", Mode: "graphrag", KGOntologyID: "onto_a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateKnowledgeBase(&store.KnowledgeBase{ID: "kb_2", Name: "无关库", Mode: "rag"}); err != nil {
		t.Fatal(err)
	}
	a := &store.Agent{ID: "agt_1", Name: "验证员", CompanionOntologyID: "onto_a"}
	if _, err := s.Store.CreateAgent(a); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/ontologies/onto_a/references", nil)
	req.SetPathValue("id", "onto_a")
	w := httptest.NewRecorder()
	s.ontologyReferences(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var out struct {
		RuntimePlans    []map[string]any `json:"runtime_plans"`
		KBVocabs        []map[string]any `json:"kb_vocabs"`
		CompanionAgents []map[string]any `json:"companion_agents"`
		Warnings        []string         `json:"warnings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.RuntimePlans) != 2 {
		t.Fatalf("runtime_plans 应过滤出 2 条（running+stopped），得到 %d", len(out.RuntimePlans))
	}
	if out.RuntimePlans[0]["status"] != "running" || out.RuntimePlans[1]["status"] != "stopped" {
		t.Fatalf("方案引用应保留状态字段，得到 %v", out.RuntimePlans)
	}
	if len(out.KBVocabs) != 1 || out.KBVocabs[0]["id"] != "kb_1" {
		t.Fatalf("kb_vocabs 应只含 kg_ontology_id 命中的 1 条，得到 %v", out.KBVocabs)
	}
	if len(out.CompanionAgents) != 1 || out.CompanionAgents[0]["name"] != "验证员" {
		t.Fatalf("companion_agents 应含绑定者，得到 %v", out.CompanionAgents)
	}
	if len(out.Warnings) != 0 {
		t.Fatalf("运行平面可达时不应有 warnings，得到 %v", out.Warnings)
	}
}

func TestOntologyReferencesRuntimeUnreachable(t *testing.T) {
	s := newOntoRefsFixture(t, nil, nil) // RuntimeURL 不可达
	if _, err := s.Store.CreateAgent(&store.Agent{ID: "agt_1", Name: "绑定者", CompanionOntologyID: "onto_a"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/ontologies/onto_a/references", nil)
	req.SetPathValue("id", "onto_a")
	w := httptest.NewRecorder()
	s.ontologyReferences(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("运行平面不可达应降级 200，得到 %d", w.Code)
	}
	var out struct {
		RuntimePlans []map[string]any `json:"runtime_plans"`
		Warnings     []string         `json:"warnings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.RuntimePlans) != 0 || len(out.Warnings) == 0 {
		t.Fatalf("不可达应 plans 置空 + warnings 如实标注，得到 plans=%d warnings=%v", len(out.RuntimePlans), out.Warnings)
	}
}

func TestDeleteOntologyGuardRunningBlocked(t *testing.T) {
	rtH := stubRuntime(t, `[{"id":"rt_1","name":"oxigraph-1","engine":"oxigraph","ontology_ids":["onto_a"],"status":"running"}]`)
	s := newOntoRefsFixture(t, rtH, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/ontologies/onto_a", nil)
	req.SetPathValue("id", "onto_a")
	w := httptest.NewRecorder()
	s.deleteOntologyGuard(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("running 引用应 409，得到 %d body=%s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !json.Valid([]byte(body)) {
		t.Fatalf("409 响应应为 JSON，得到 %q", body)
	}
}

func TestDeleteOntologyGuardStoppedAllowed(t *testing.T) {
	rtH := stubRuntime(t, `[{"id":"rt_2","name":"stopped-plan","engine":"oxigraph","ontology_ids":["onto_a"],"status":"stopped"}]`)
	bd := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"deleted":"onto_a"}`))
	})
	s := newOntoRefsFixture(t, rtH, bd)

	req := httptest.NewRequest(http.MethodDelete, "/api/ontologies/onto_a", nil)
	req.SetPathValue("id", "onto_a")
	w := httptest.NewRecorder()
	s.deleteOntologyGuard(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stopped 引用应警示放行（透传构建平面 200），得到 %d body=%s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != `{"deleted":"onto_a"}` {
		t.Fatalf("应原样透传构建平面响应体，得到 %q", body)
	}
}
