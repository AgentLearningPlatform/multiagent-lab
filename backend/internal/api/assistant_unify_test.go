// REQ-192/M32 平台助手配置单源归一——单测：
// 归一引导（空 instruction 写默认+微调移植+幂等）、GET/PUT 读写穿透内置行（空=默认基座）、
// updateAgent 内置行字段白名单（配置放行/身份锁死）、「内容优化」会话幂等。
package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func newAssistantFixture(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &Server{Store: st}
}

func TestEnsureBuiltinAssistantInstruction(t *testing.T) {
	s := newAssistantFixture(t)
	a, _ := s.Store.GetAgent("builtin-assistant")
	if a == nil || !a.IsBuiltin {
		t.Fatal("迁移 023 应有内置行")
	}
	// 迁移 025 后 instruction 为空 → Ensure 写入默认
	if strings.TrimSpace(a.Instruction) != "" {
		t.Fatalf("迁移 025 应置空 instruction，得到 %q", a.Instruction[:min(20, len(a.Instruction))])
	}
	if err := s.Store.EnsureBuiltinAssistantInstruction("BASE-PROMPT"); err != nil {
		t.Fatal(err)
	}
	a, _ = s.Store.GetAgent("builtin-assistant")
	if a.Instruction != "BASE-PROMPT" {
		t.Fatalf("空行应写入默认，得到 %q", a.Instruction)
	}
	// 幂等：非空不动（含用户全量编辑值）
	if err := s.Store.EnsureBuiltinAssistantInstruction("OTHER"); err != nil {
		t.Fatal(err)
	}
	a, _ = s.Store.GetAgent("builtin-assistant")
	if a.Instruction != "BASE-PROMPT" {
		t.Fatal("非空行不应被覆盖")
	}
	// 存量微调移植：清空行 + assistant_config 有微调 → 拼接
	if err := s.Store.SaveAssistantConfig(&store.AssistantConfig{SystemPrompt: "微调内容XYZ"}); err != nil {
		t.Fatal(err)
	}
	a.Instruction = ""
	if _, err := s.Store.UpdateAgent(a); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.EnsureBuiltinAssistantInstruction("BASE-PROMPT"); err != nil {
		t.Fatal(err)
	}
	a, _ = s.Store.GetAgent("builtin-assistant")
	if !strings.Contains(a.Instruction, "BASE-PROMPT") || !strings.Contains(a.Instruction, "微调内容XYZ") {
		t.Fatalf("存量微调应移植进全量 instruction：%q", a.Instruction)
	}
}

func TestAssistantConfigReadWriteThrough(t *testing.T) {
	s := newAssistantFixture(t)
	if err := s.Store.EnsureBuiltinAssistantInstruction("BASE-PROMPT"); err != nil {
		t.Fatal(err)
	}
	// GET：读内置行
	w := httptest.NewRecorder()
	s.assistantConfigGet(w, httptest.NewRequest("GET", "/api/assistant/config", nil))
	if w.Code != 200 {
		t.Fatalf("GET 应 200：%d", w.Code)
	}
	var got store.AssistantConfig
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.SystemPrompt != "BASE-PROMPT" {
		t.Fatalf("GET 应返回行内 instruction：%q", got.SystemPrompt)
	}
	// PUT：非空全量写行；空=默认基座不可清空
	body, _ := json.Marshal(store.AssistantConfig{SystemPrompt: "MY-PROMPT", ModelConnID: "conn-x"})
	w = httptest.NewRecorder()
	s.assistantConfigPut(w, httptest.NewRequest("PUT", "/api/assistant/config", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("PUT 应 200：%d %s", w.Code, w.Body.String())
	}
	a, _ := s.Store.GetAgent("builtin-assistant")
	if a.Instruction != "MY-PROMPT" || derefStr(a.ModelConnID) != "conn-x" {
		t.Fatalf("PUT 应写内置行：%q / %q", a.Instruction, derefStr(a.ModelConnID))
	}
	body, _ = json.Marshal(store.AssistantConfig{SystemPrompt: ""})
	w = httptest.NewRecorder()
	s.assistantConfigPut(w, httptest.NewRequest("PUT", "/api/assistant/config", bytes.NewReader(body)))
	a, _ = s.Store.GetAgent("builtin-assistant")
	if a.Instruction != AssistantDefaultPrompt {
		t.Fatalf("空提交应恢复默认基座全文（不可清空）")
	}
}

func TestUpdateAgentBuiltinWhitelist(t *testing.T) {
	s := newAssistantFixture(t)
	if err := s.Store.EnsureBuiltinAssistantInstruction("BASE-PROMPT"); err != nil {
		t.Fatal(err)
	}
	prev, _ := s.Store.GetAgent("builtin-assistant")
	temp := 0.7
	// 配置字段放行：instruction/model_conn_id/temperature
	body, _ := json.Marshal(map[string]any{"name": "改名尝试", "instruction": "NEW-PROMPT", "model_conn_id": "conn-y", "temperature": temp, "tools": []string{"x"}, "is_builtin": false})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/agents/builtin-assistant", bytes.NewReader(body))
	req.SetPathValue("id", "builtin-assistant")
	s.updateAgent(w, req)
	if w.Code != 200 {
		t.Fatalf("内置行白名单 PUT 应 200：%d %s", w.Code, w.Body.String())
	}
	a, _ := s.Store.GetAgent("builtin-assistant")
	if a.Instruction != "NEW-PROMPT" || derefStr(a.ModelConnID) != "conn-y" || a.Temperature == nil || *a.Temperature != 0.7 {
		t.Fatalf("配置字段应放行：%+v", a)
	}
	// 身份/能力字段锁死（保持 prev）
	if a.Name != prev.Name || a.IsBuiltin != true {
		t.Fatalf("身份/能力字段应锁死：name=%q builtin=%v", a.Name, a.IsBuiltin)
	}
	// 非法 temperature 400
	body, _ = json.Marshal(map[string]any{"temperature": 5})
	w = httptest.NewRecorder()
	req2 := httptest.NewRequest("PUT", "/api/agents/builtin-assistant", bytes.NewReader(body))
	req2.SetPathValue("id", "builtin-assistant")
	s.updateAgent(w, req2)
	if w.Code != 400 {
		t.Fatalf("非法温度应 400：%d", w.Code)
	}
}

func TestEnsureAssistantContentConv(t *testing.T) {
	s := newAssistantFixture(t)
	id1, err := s.Store.EnsureAssistantContentConv()
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.Store.EnsureAssistantContentConv()
	if err != nil || id1 != id2 {
		t.Fatalf("内容优化会话应幂等复用：%s vs %s (%v)", id1, id2, err)
	}
	if _, err := s.Store.InsertMessage(&store.Message{ConversationID: id1, Role: "assistant", Content: "opt", Meta: `{"source":"content-optimization"}`}); err != nil {
		t.Fatal(err)
	}
	msgs, err := s.Store.ListMessages(id1)
	if err != nil || len(msgs) != 1 || msgs[0].Meta == "" || !strings.Contains(msgs[0].Meta, "content-optimization") {
		t.Fatalf("留痕消息应可还原：%v %+v", err, msgs)
	}
}
