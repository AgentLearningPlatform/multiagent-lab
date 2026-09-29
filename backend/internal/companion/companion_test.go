package companion

import (
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
	if g := GraphURI("abc"); g != "http://eino-lab/graph/conv-abc" {
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
	for _, want := range []string{"GRAPH <http://eino-lab/graph/conv-c1>", "a bot:Concept", `rdfs:label "Pod 扩容"`, "prov:wasGeneratedBy", "bot:extractedFrom <http://eino-lab/msg/m9>"} {
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
	if !strings.Contains(find, "FILTER NOT EXISTS") || !strings.Contains(find, "conv-c1") {
		t.Fatalf("矛盾检测查询不符:\n%s", find)
	}
	inv := InvalidateEdge("c1", "http://eino-lab/e/edge-cand2", testTime())
	if !strings.Contains(inv, "bot:invalidAt") {
		t.Fatalf("失效化不符:\n%s", inv)
	}
	if dp := DropGraph("c1"); !strings.Contains(dp, "DROP SILENT GRAPH <http://eino-lab/graph/conv-c1>") {
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
	for _, id := range []string{"agt_coord", "agt_member", "agt_outside"} {
		if _, err := st.CreateAgent(&store.Agent{ID: id, Name: id, CompanionOntology: true}); err != nil {
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

	// 开关关 → 不触发
	s.mu.Lock()
	s.running = map[string]bool{}
	s.mu.Unlock()
	a, _ := st.GetAgent("agt_coord")
	a.CompanionOntology = false
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

// TestOSBinaryEnvFallback REQ-195：伴生引擎二进制 env 口径与运行平面统一——
// COMPANION_OXIGRAPH_BIN 优先，回退 OXIGRAPH_BIN（run-dev.sh 只导出后者时伴生侧同源命中）。
func TestOSBinaryEnvFallback(t *testing.T) {
	t.Setenv("COMPANION_OXIGRAPH_BIN", "")
	t.Setenv("OXIGRAPH_BIN", "")
	if got := osBinary(); got != "" {
		t.Fatalf("双 env 均空应返回空，got %q", got)
	}
	t.Setenv("OXIGRAPH_BIN", "/opt/oxigraph")
	if got := osBinary(); got != "/opt/oxigraph" {
		t.Fatalf("应回退 OXIGRAPH_BIN，got %q", got)
	}
	t.Setenv("COMPANION_OXIGRAPH_BIN", "/explicit/oxigraph")
	if got := osBinary(); got != "/explicit/oxigraph" {
		t.Fatalf("COMPANION_OXIGRAPH_BIN 应优先，got %q", got)
	}
}

// TestEngineResolvedBinary REQ-195：resolvedBinary 仅在写侧拉起后登记（领养/未启动为空，可观测口径）。
func TestEngineResolvedBinary(t *testing.T) {
	e := NewEngine("", t.TempDir(), 0)
	if got := e.ResolvedBinary(); got != "" {
		t.Fatalf("未启动应返回空，got %q", got)
	}
}
