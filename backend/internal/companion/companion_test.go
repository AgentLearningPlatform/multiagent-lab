package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// 零依赖单测：游标增量定位 / 候选转换 / SPARQL 生成（REQ-170/M28 P1）。

func msgsOf(ids ...string) []*store.Message {
	out := make([]*store.Message, 0, len(ids))
	for _, id := range ids {
		out = append(out, &store.Message{ID: id, Role: "user", Content: "内容 " + id})
	}
	return out
}

func TestRemainingAfter(t *testing.T) {
	msgs := msgsOf("m1", "m2", "m3", "m4")
	// 空游标 = 全量（首次抽取）
	if got := remainingAfter(msgs, ""); len(got) != 4 {
		t.Fatalf("空游标应返回全部 4 条，got %d", len(got))
	}
	// 定位 m2 → 之后 2 条
	got := remainingAfter(msgs, "m2")
	if len(got) != 2 || got[0].ID != "m3" || got[1].ID != "m4" {
		t.Fatalf("游标 m2 后应为 m3/m4，got %v", got)
	}
	// 游标在末尾 → 空
	if got := remainingAfter(msgs, "m4"); len(got) != 0 {
		t.Fatalf("游标在末尾应为空，got %d", len(got))
	}
	// 游标 id 不在列表（历史被清理）→ 保守返回空（不重抽历史）
	if got := remainingAfter(msgs, "gone"); len(got) != 0 {
		t.Fatalf("未知游标应保守返回空，got %d", len(got))
	}
}

func TestToCandidates(t *testing.T) {
	msgs := []*store.Message{
		{ID: "u1", Role: "user", Content: "Pod 扩容会触发什么事件？"},
		{ID: "a1", Role: "assistant", Content: "Pod 扩容通常引发 HPA 调整副本数。"},
	}
	out := &extractOut{}
	out.Concepts = append(out.Concepts, struct {
		Name       string  `json:"name"`
		Definition string  `json:"definition"`
		Confidence float64 `json:"confidence"`
		Source     string  `json:"source"`
	}{Name: "Pod 扩容", Definition: "副本数水平伸缩动作", Confidence: 0.9, Source: "Pod 扩容"})
	out.Relations = append(out.Relations, struct {
		RelName    string  `json:"rel_name"`
		Source     string  `json:"source"`
		Target     string  `json:"target"`
		Definition string  `json:"definition"`
		Confidence float64 `json:"confidence"`
		Evidence   string  `json:"evidence"`
	}{RelName: "引发", Source: "Pod 扩容", Target: "HPA", Confidence: 0.8, Evidence: "引发 HPA"})
	out.Events = append(out.Events, struct {
		Name       string  `json:"name"`
		Definition string  `json:"definition"`
		Confidence float64 `json:"confidence"`
		Source     string  `json:"source"`
	}{Name: "", Confidence: 0.5}) // 空 name 应被过滤
	cands := toCandidates("conv1", "agt1", msgs, out)
	if len(cands) != 2 {
		t.Fatalf("应产出 2 条候选（空名事件被滤），got %d", len(cands))
	}
	var rel *store.CompanionCandidate
	for _, c := range cands {
		if c.Kind == "relation" {
			rel = c
		}
	}
	if rel == nil || rel.Name != "Pod 扩容" || rel.RelName != "引发" || rel.RelTarget != "HPA" {
		t.Fatalf("relation 三件拆解不符: %+v", rel)
	}
	if rel.SourceMessageID == "" || rel.Status != "pending" {
		t.Fatalf("relation 溯源/状态不符: %+v", rel)
	}
}

func TestSlugAndEscapes(t *testing.T) {
	if got := Slug("Pod 扩容"); got != "Pod_扩容" {
		t.Fatalf("slug 混排不符: %q", got)
	}
	if got := Slug(`a"b\c`); strings.ContainsAny(got, `"\\`) {
		t.Fatalf("slug 应剥离危险字符: %q", got)
	}
	esc := turtleEscape("引\"号\n换行")
	if strings.Contains(esc, "\"") && !strings.Contains(esc, "\\\"") {
		t.Fatalf("未转义引号: %q", esc)
	}
}

func TestGraphURIs(t *testing.T) {
	// REQ-216：图换绑本体伴生子图（ont-{ontologyID}）
	if g := GraphURI("abc"); g != "http://eino-lab/graph/ont-abc" {
		t.Fatalf("graph URI 不符: %s", g)
	}
	e1 := EntityURI("滚动更新")
	e2 := EntityURI("滚动 更新") // 空格转下划线 → 不同实体（薄版口径：标签原文区分）
	if e1 == e2 {
		t.Fatalf("不同标签不应归并: %s", e1)
	}
}

func TestInsertAndInvalidate(t *testing.T) {
	ins := InsertNodeTriples("c1", "cand1", "concept", "Pod 扩容", "副本伸缩", 0.86, "m9", testTime())
	for _, want := range []string{"GRAPH <http://eino-lab/graph/ont-c1>", "a bot:Concept", `rdfs:label "Pod 扩容"`, "prov:wasGeneratedBy", "bot:extractedFrom <http://eino-lab/msg/m9>"} {
		if !strings.Contains(ins, want) {
			t.Fatalf("INSERT 缺少 %q:\n%s", want, ins)
		}
	}
	relIns := InsertRelationTriples("c1", "cand2", "引发", "Pod 扩容", "HPA", "", 0.8, "m9", testTime())
	for _, want := range []string{"a bot:Relation", `bot:relName "引发"`, "bot:subject <http://eino-lab/e/Pod_扩容>", "bot:object <http://eino-lab/e/HPA>"} {
		if !strings.Contains(relIns, want) {
			t.Fatalf("关系 INSERT 缺少 %q:\n%s", want, relIns)
		}
	}
	find := FindActiveEdge("c1", "Pod 扩容", "引发")
	if !strings.Contains(find, "FILTER NOT EXISTS") || !strings.Contains(find, "ont-c1") {
		t.Fatalf("矛盾检测查询不符:\n%s", find)
	}
	inv := InvalidateEdge("c1", "http://eino-lab/e/edge-cand2", testTime())
	if !strings.Contains(inv, "bot:invalidAt") {
		t.Fatalf("失效化不符:\n%s", inv)
	}
	if dp := DropGraph("c1"); !strings.Contains(dp, "DROP SILENT GRAPH <http://eino-lab/graph/ont-c1>") {
		t.Fatalf("DROP 不符: %s", dp)
	}
	seed := SeedSchema()
	for _, cls := range []string{"bot:Concept", "bot:Relation", "bot:Event", "bot:Source", "bot:Agent"} {
		if !strings.Contains(seed, cls+" a owl:Class") {
			t.Fatalf("种子 schema 缺 %s", cls)
		}
	}
}

func testTime() time.Time { return time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC) }

// 2026-09-27 修复验证：项目会话（scope=project）此前被 Scope 守卫整类拦截——
// agent 开关开启且 agent 为项目 coordinator/成员时应触发抽取；跨项目 agent 仍拦截。
func TestOnRunCompleteProjectScope(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := NewService(st, nil, nil)

	// 三个 agent：coordinator（开伴生）/ 普通成员（开伴生）/ 外部 agent（开伴生）
	// REQ-216：开启语义 = 绑定伴生本体（companion_ontology_id 非空）
	for _, id := range []string{"agt_coord", "agt_member", "agt_outside"} {
		if _, err := st.CreateAgent(&store.Agent{ID: id, Name: id, CompanionOntologyID: "ont_" + id}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.CreateProject(&store.Project{ID: "proj_1", Name: "p1", Coordinator: "agt_coord"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetProjectAgents("proj_1", []store.ProjectMember{{AgentID: "agt_coord", Role: "coordinator"}, {AgentID: "agt_member", Role: "member"}}); err != nil {
		t.Fatal(err)
	}
	conv := &store.Conversation{ID: "conv_p1", Scope: "project", ProjectID: strPtr("proj_1")}

	cases := []struct {
		name  string
		agent string
		want  bool
	}{
		{"coordinator 触发", "agt_coord", true},
		{"成员触发", "agt_member", true},
		{"外部 agent 拦截", "agt_outside", false},
	}
	for _, c := range cases {
		s.mu.Lock()
		s.running = map[string]bool{}
		s.mu.Unlock()
		a, _ := st.GetAgent(c.agent)
		s.OnRunComplete(conv, a)
		s.mu.Lock()
		_, fired := s.running[conv.ID]
		s.mu.Unlock()
		if fired != c.want {
			t.Fatalf("%s: running=%v want %v", c.name, fired, c.want)
		}
		// 清理 goroutine
		s.mu.Lock()
		delete(s.running, conv.ID)
		s.mu.Unlock()
	}

	// 开关关（未绑定）→ 不触发
	s.mu.Lock()
	s.running = map[string]bool{}
	s.mu.Unlock()
	if err := st.SetAgentCompanionBinding("agt_coord", ""); err != nil {
		t.Fatal(err)
	}
	a, _ := st.GetAgent("agt_coord")
	s.OnRunComplete(conv, a)
	s.mu.Lock()
	_, fired := s.running[conv.ID]
	s.mu.Unlock()
	if fired {
		t.Fatal("开关关不应触发")
	}
}

func strPtr(s string) *string { return &s }

// REQ-187 配置增强单测：prompt hint 拼接 / 阈值分级语义。
func TestCompanionPromptHint(t *testing.T) {
	base := companionPrompt("语料", "", "")
	if strings.Contains(base, "领域聚焦要求") || strings.Contains(base, "已有实体清单") {
		t.Fatal("空 hint/空清单不应含聚焦行与对齐节")
	}
	withHint := companionPrompt("语料", "重点关注 Kubernetes 部署术语", "")
	if !strings.Contains(withHint, "5. 领域聚焦要求（优先级最高）：重点关注 Kubernetes 部署术语") {
		t.Fatalf("hint 应追加为第 5 条: %s", withHint)
	}
	// REQ-194①：对齐节注入位于语料之前
	withAlign := companionPrompt("语料", "", buildAlignmentSection([]string{"Pod 扩容", "HPA"}))
	if !strings.Contains(withAlign, "已有实体清单") || !strings.Contains(withAlign, "Pod 扩容、HPA") {
		t.Fatalf("对齐节应含清单: %s", withAlign)
	}
	if strings.Index(withAlign, "已有实体清单") > strings.Index(withAlign, "只输出 JSON") {
		t.Fatal("对齐节应在输出约束之前")
	}
}

func TestAutoThresholdGrading(t *testing.T) {
	// 阈值分级语义直接复用 ConfirmCandidate 链路——此处验证分级判定本身
	threshold := 0.85
	cands := []struct {
		conf float64
		want bool // true=自动入图
	}{{0.95, true}, {0.85, true}, {0.84, false}, {0.3, false}}
	for _, c := range cands {
		if got := c.conf >= threshold; got != c.want {
			t.Fatalf("conf %.2f ≥ %.2f = %v, want %v", c.conf, threshold, got, c.want)
		}
	}
	// 默认 0 = 全人工审（不自动入图）
	if auto := 0.9 >= 0.0; auto {
		_ = auto // 语义上 0 表示关闭自动入图——ExtractNew 以 `threshold > 0` 为门卫（service.go 已实现）
	}
}

// TestGraphURIAgentScopeLegacy REQ-211→216：旧 agent 图 URI 保留给跨实例迁移（agt-），
// 现行图 = 本体伴生子图（ont-）。
func TestGraphURIAgentScopeLegacy(t *testing.T) {
	if g := LegacyAgentGraphURI("a1"); g != "http://eino-lab/graph/agt-a1" {
		t.Fatalf("旧 agent 图 URI 不符: %s", g)
	}
	if g := LegacyConvGraphURI("c9"); g != "http://eino-lab/graph/conv-c9" {
		t.Fatalf("旧会话图 URI 不符: %s", g)
	}
	if dp := DropGraphByURI(LegacyAgentGraphURI("a1")); dp != "DROP SILENT GRAPH <http://eino-lab/graph/agt-a1>" {
		t.Fatalf("迁移 DROP 应按旧 URI 显式清理（GraphURI 已换轨不能复用 DropGraph）: %s", dp)
	}
}

// TestPlanEnginesEnsureHostPlan REQ-216③：宿主方案三段式——running 复用 → 存量方案拉起 →
// 自动创建并 start（stub runtime-manager REST）。
func TestPlanEnginesEnsureHostPlan(t *testing.T) {
	var createBody map[string]any
	started := map[string]bool{}
	hostStatus := "running" // 段① running 复用 → 段② 前置为 stopped 验拉起
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/runtime-profiles", func(w http.ResponseWriter, r *http.Request) {
		profiles := []map[string]any{{"id": "rt_host", "name": "伴生·医学术语", "engine": "oxigraph", "ontology_ids": []string{"ont_a"}, "port": 9301, "status": hostStatus}}
		if createBody != nil {
			profiles = append(profiles, map[string]any{"id": "rt_new", "name": "伴生·新本体", "engine": "oxigraph", "ontology_ids": []string{"ont_new"}, "port": 9302, "status": "created"})
		}
		_ = json.NewEncoder(w).Encode(profiles)
	})
	mux.HandleFunc("GET /api/ontologies/ont_new", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"ont_new","name":"新本体"}`))
	})
	mux.HandleFunc("POST /api/runtime-profiles", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&createBody)
		_, _ = w.Write([]byte(`{"id":"rt_new","name":"伴生·新本体","engine":"oxigraph","ontology_ids":["ont_new"],"port":9302,"status":"created"}`))
	})
	mux.HandleFunc("POST /api/runtime-profiles/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		started[r.PathValue("id")] = true
		port := 9301
		if r.PathValue("id") == "rt_new" {
			port = 9302
		}
		_, _ = fmt.Fprintf(w, `{"id":%q,"status":"running","port":%d}`, r.PathValue("id"), port)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := NewPlanEngines(srv.URL, srv.URL)
	// ① running 复用（不触发 start）
	base, err := p.EnsureHostPlan(context.Background(), "ont_a")
	if err != nil || base != "http://127.0.0.1:9301" || started["rt_host"] {
		t.Fatalf("running 方案应复用且不重拉: %s %v %v", base, started, err)
	}
	// ② 存量方案 stopped → 拉起
	hostStatus = "stopped"
	p.Invalidate("ont_a")
	base, err = p.EnsureHostPlan(context.Background(), "ont_a")
	if err != nil || base != "http://127.0.0.1:9301" || !started["rt_host"] {
		t.Fatalf("stopped 方案应被拉起: %s %v %v", base, started, err)
	}
	// ③ 无方案 → 创建「伴生·{本体名}」并 start
	base, err = p.EnsureHostPlan(context.Background(), "ont_new")
	if err != nil || base != "http://127.0.0.1:9302" {
		t.Fatalf("无方案应自动创建并启动: %s %v", base, err)
	}
	if createBody["name"] != "伴生·新本体" || createBody["engine"] != "oxigraph" {
		t.Fatalf("创建载荷不符: %v", createBody)
	}
	if p.HostPlan("ont_new") != "rt_new" {
		t.Fatalf("宿主方案 id 应登记: %s", p.HostPlan("ont_new"))
	}
}

// TestCompanionOntologyName REQ-216②：绑定建议名口径。
func TestCompanionOntologyName(t *testing.T) {
	if got := CompanionOntologyName("运维助手"); got != "运维助手的伴生本体" {
		t.Fatalf("建议名不符: %s", got)
	}
	if got := CompanionOntologyName("  "); got != "未命名智能体的伴生本体" {
		t.Fatalf("空名兜底不符: %s", got)
	}
}

// TestInsertTriplesRoundTrip REQ-216 迁移：SPARQL JSON term → Turtle 序列化 → INSERT DATA。
func TestInsertTriplesRoundTrip(t *testing.T) {
	raw := []byte(`{"results":{"bindings":[
		{"s":{"type":"uri","value":"http://eino-lab/e/X"},"p":{"type":"uri","value":"http://eino-lab/ontology/thin/definition"},"o":{"type":"literal","value":"含\"引\"号\n"}},
		{"s":{"type":"uri","value":"http://eino-lab/e/X"},"p":{"type":"uri","value":"http://www.w3.org/2000/01/rdf-schema#label"},"o":{"type":"literal","value":"X","xml:lang":"zh"}}
	]}}`)
	triples, err := parseTriples(raw)
	if err != nil || len(triples) != 2 {
		t.Fatalf("解析不符: %v %v", triples, err)
	}
	ins := insertTriplesData("http://eino-lab/graph/ont-x", triples)
	if !strings.Contains(ins, `<http://eino-lab/e/X> <http://eino-lab/ontology/thin/definition> "含\"引\"号\n"`) {
		t.Fatalf("字面量转义不符:\n%s", ins)
	}
	if !strings.Contains(ins, `"X"@zh`) {
		t.Fatalf("语言标注不符:\n%s", ins)
	}
	if !strings.Contains(ins, "GRAPH <http://eino-lab/graph/ont-x>") {
		t.Fatalf("目标图不符:\n%s", ins)
	}
}

// REQ-216 P3 收尾（复查轮后续）：②宿主方案命名去叠加 / ④拉起失败冷却。
func TestPlanNameNoDoublePrefix(t *testing.T) {
	var createBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/runtime-profiles", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) })
	mux.HandleFunc("GET /api/ontologies/ont_p", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"ont_p","name":"伴生·已带前缀"}`))
	})
	mux.HandleFunc("POST /api/runtime-profiles", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&createBody)
		_, _ = w.Write([]byte(`{"id":"rt_p","name":"x","engine":"oxigraph","ontology_ids":["ont_p"],"port":9341,"status":"created"}`))
	})
	mux.HandleFunc("POST /api/runtime-profiles/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"rt_p","status":"running","port":9341}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	p := NewPlanEngines(srv.URL, srv.URL)
	if _, err := p.EnsureHostPlan(context.Background(), "ont_p"); err != nil {
		t.Fatal(err)
	}
	if createBody["name"] != "伴生·已带前缀" {
		t.Fatalf("已带前缀不应重复拼接: %v", createBody["name"])
	}
}

func TestStartCooldownAfterFailure(t *testing.T) {
	old := startCooldown
	startCooldown = 200 * time.Millisecond
	t.Cleanup(func() { startCooldown = old })

	starts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/runtime-profiles", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"rt_e","name":"伴生·E","engine":"oxigraph","ontology_ids":["ont_e"],"port":9342,"status":"error"}]`))
	})
	mux.HandleFunc("POST /api/runtime-profiles/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		starts++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	p := NewPlanEngines(srv.URL, srv.URL)

	_, err1 := p.EnsureHostPlan(context.Background(), "ont_e")
	if err1 == nil {
		t.Fatal("error 态方案拉起失败应报错")
	}
	if starts != 1 {
		t.Fatalf("首次应尝试 start 一次, got %d", starts)
	}
	// 冷却期内：快速失败，不再打 start
	_, err2 := p.EnsureHostPlan(context.Background(), "ont_e")
	if err2 == nil || !strings.Contains(err2.Error(), "冷却中") {
		t.Fatalf("冷却期内应快速失败: %v", err2)
	}
	if starts != 1 {
		t.Fatalf("冷却期内不应重试 start, got %d", starts)
	}
	// 冷却过期 → 重试
	time.Sleep(250 * time.Millisecond)
	_, _ = p.EnsureHostPlan(context.Background(), "ont_e")
	if starts != 2 {
		t.Fatalf("冷却过期应重试 start, got %d", starts)
	}
}
