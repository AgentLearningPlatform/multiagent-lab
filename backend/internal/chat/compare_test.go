package chat

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/inference"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// REQ-19e/19f 对比模式窗格覆盖与请求解码（零依赖单测）。

func strPtr(v string) *string { return &v }

func TestRunInputPanesDecode(t *testing.T) {
	body := `{"input":"同问对比","debug_level":1,"panes":[
		{"model_conn_id":"mc1"},
		{"kb_id":"kb2","runtime_profile_id":"rp2"},
		{"agent_id":"ag9","no_history":true},
		{}]}`
	var in RunInput
	if err := json.Unmarshal([]byte(body), &in); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if in.Input != "同问对比" || in.DebugLevel != 1 {
		t.Fatalf("base fields: %+v", in)
	}
	if len(in.Panes) != 4 {
		t.Fatalf("panes len = %d, want 4", len(in.Panes))
	}
	if in.Panes[2].AgentID != "ag9" || !in.Panes[2].NoHistory {
		t.Fatalf("pane2 REQ-143 字段: %+v", in.Panes[2])
	}
	if in.Panes[0].ModelConnID != "mc1" || in.Panes[0].KBID != "" {
		t.Fatalf("pane0 = %+v", in.Panes[0])
	}
	if in.Panes[1].KBID != "kb2" || in.Panes[1].RuntimeProfileID != "rp2" || in.Panes[1].ModelConnID != "" {
		t.Fatalf("pane1 = %+v", in.Panes[1])
	}
	if in.Panes[3] != (PaneConfig{}) {
		t.Fatalf("pane3 应为空（全继承）: %+v", in.Panes[3])
	}
	// 旧请求体（无 panes）兼容：单路现状不变
	var legacy RunInput
	if err := json.Unmarshal([]byte(`{"input":"hi","debug_level":0}`), &legacy); err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	if legacy.Panes != nil {
		t.Fatalf("legacy panes 应为 nil，got %+v", legacy.Panes)
	}
}

func TestPaneConversationOverrides(t *testing.T) {
	conv := &store.Conversation{
		ID: "c1", Scope: "agent", AgentID: strPtr("a1"),
		KBID: strPtr("kb-old"), EnableKB: true,
		InterruptState: `{"kind":"ask_human"}`,
	}

	// 空 PaneConfig：返回原指针（继承对话配置，含未开启状态），不复制不改动
	same := paneConversation(conv, PaneConfig{})
	if same != conv {
		t.Fatal("空覆盖应返回原对话")
	}

	// 仅知识库覆盖：kb 替换且开启；其余字段不动
	pc := paneConversation(conv, PaneConfig{KBID: "kb-new"})
	if pc == conv {
		t.Fatal("有覆盖时应返回副本")
	}
	if *pc.KBID != "kb-new" || !pc.EnableKB {
		t.Fatalf("kb 覆盖未生效: %+v", pc)
	}
	if conv.KBID == nil || *conv.KBID != "kb-old" {
		t.Fatal("原对话被改动")
	}
	if pc.RuntimeProfileID != conv.RuntimeProfileID {
		t.Fatal("未覆盖字段应保持")
	}
	if pc.InterruptState != "" {
		t.Fatal("窗格副本应清空挂起中断（组级已处理）")
	}

	// 运行方案覆盖：profile 替换且本体启用
	pc2 := paneConversation(conv, PaneConfig{RuntimeProfileID: "rp-new"})
	if pc2.RuntimeProfileID == nil || *pc2.RuntimeProfileID != "rp-new" || !pc2.OntologyEnabled {
		t.Fatalf("profile 覆盖未生效: %+v", pc2)
	}
	if pc2.KBID == nil || *pc2.KBID != "kb-old" || !pc2.EnableKB {
		t.Fatalf("未覆盖的 kb 应继承: %+v", pc2)
	}

	// 继承「未开启」状态：对话未开知识库、窗格不覆盖 → 副本仍不开（仅当窗格显式选择才开）
	convOff := &store.Conversation{ID: "c2", Scope: "agent", KBID: strPtr("kb-old"), EnableKB: false}
	pc3 := paneConversation(convOff, PaneConfig{RuntimeProfileID: "rp"})
	if pc3.EnableKB {
		t.Fatal("未覆盖 kb 开关时应继承 false")
	}
}

func TestPaneAgentModelOverride(t *testing.T) {
	ag := &store.Agent{ID: "a1", Name: "A", ModelConnID: strPtr("mc-old")}

	if paneAgent(ag, PaneConfig{}) != ag {
		t.Fatal("空覆盖应返回原智能体")
	}
	if paneAgent(nil, PaneConfig{ModelConnID: "x"}) != nil {
		t.Fatal("nil agent 应保持 nil")
	}
	pc := paneAgent(ag, PaneConfig{ModelConnID: "mc-new"})
	if *pc.ModelConnID != "mc-new" {
		t.Fatalf("模型覆盖未生效: %+v", pc)
	}
	if ag.ModelConnID == nil || *ag.ModelConnID != "mc-old" {
		t.Fatal("原智能体被改动")
	}
	if pc.ID != "a1" || pc.Name != "A" {
		t.Fatal("其余字段应保持")
	}
}

func TestPaneMetaJSON(t *testing.T) {
	meta := paneMetaJSON(2, "run-3", &store.Agent{ID: "ag-x"}, PaneConfig{ModelConnID: "mc1", KBID: "kb1", NoHistory: true})
	var m map[string]any
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		t.Fatalf("meta 非合法 JSON: %v", err)
	}
	if m["compare"] != true || m["pane"].(float64) != 2 || m["run_id"] != "run-3" {
		t.Fatalf("meta 基本字段: %v", m)
	}
	if m["agent_id"] != "ag-x" || m["no_history"] != true {
		t.Fatalf("meta REQ-143 快照字段: %v", m)
	}
	ov, _ := m["overrides"].(map[string]any)
	if ov["model_conn_id"] != "mc1" || ov["kb_id"] != "kb1" || ov["runtime_profile_id"] != "" {
		t.Fatalf("overrides: %v", ov)
	}
}

// REQ-143 跨智能体窗格：Agent 解析与窗格级能力守卫。

func TestResolvePaneAgent(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	def := &store.Agent{ID: "def", Name: "默认", RuntimeBackend: "inprocess", InferenceBackend: "eino-adk"}
	cli := &store.Agent{ID: "cli", Name: "CLI", RuntimeBackend: "inprocess", InferenceBackend: "claude-code"}
	docker := &store.Agent{ID: "dk", Name: "Docker", RuntimeBackend: "docker", InferenceBackend: "eino-adk"}
	for _, a := range []*store.Agent{def, cli, docker} {
		if _, err := st.CreateAgent(a); err != nil {
			t.Fatalf("seed %s: %v", a.ID, err)
		}
	}
	svc := &Service{Store: st, Inference: inference.NewRegistry()}

	// 空 agent_id → 继承默认；指定 agent_id → 窗格级独立装配（同 ID 不重复查库）
	ag, err := svc.resolvePaneAgent(def, PaneConfig{})
	if err != nil || ag != def {
		t.Fatalf("空覆盖应继承默认: %v %v", ag, err)
	}
	ag, err = svc.resolvePaneAgent(def, PaneConfig{AgentID: "def"})
	if err != nil || ag != def {
		t.Fatalf("同 ID 不应查库: %v %v", ag, err)
	}

	// 窗格级守卫：外部 CLI / docker 报错该窗格（不阻断整组）
	if _, err := svc.resolvePaneAgent(def, PaneConfig{AgentID: "cli"}); err == nil {
		t.Fatal("外部 CLI 窗格应报错")
	}
	if _, err := svc.resolvePaneAgent(def, PaneConfig{AgentID: "dk"}); err == nil {
		t.Fatal("docker 窗格应报错")
	}
	if _, err := svc.resolvePaneAgent(cli, PaneConfig{}); err == nil {
		t.Fatal("继承默认为外部 CLI 时也应报错")
	}
	// 不存在的 agent_id 报错
	if _, err := svc.resolvePaneAgent(def, PaneConfig{AgentID: "missing"}); err == nil {
		t.Fatal("所选智能体不存在应报错")
	}
}
