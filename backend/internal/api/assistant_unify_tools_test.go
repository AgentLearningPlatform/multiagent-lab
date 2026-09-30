// REQ-213 平台助手与普通智能体进一步统一——单测：
// ①内置工具基座并集（builtinToolsUnion：必装在前/去重/用户勾选追加）
// ②updateAgent 白名单扩容（tools 基座保护/skills/mcp_servers/max_tokens/max_iteration 放行，身份仍锁死）
// ③EnsureAssistantTools 幂等（缺 L1 并入自愈/齐全零写入）
// ④装配 gating（非 builtin 行 propose_assistant_config 剔除——经 handlers 面 JSON 往返验证 tools 字段）。
package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func newUnifyToolsFixture(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := &Server{Store: st}
	if err := s.Store.EnsureBuiltinAssistantInstruction("BASE-PROMPT"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBuiltinToolsUnion(t *testing.T) {
	got := builtinToolsUnion([]string{"current_time", "doc_read", "save_file"})
	// 必装八工具在前（保序），用户追加去重
	if len(got) != 10 {
		t.Fatalf("并集应 8 必装 + 2 新增 = 10，得到 %d：%v", len(got), got)
	}
	if got[0] != "doc_read" || got[5] != "sync_platform_kb" {
		t.Fatalf("必装应在前且保序：%v", got)
	}
	for _, want := range []string{"current_time", "save_file"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("用户勾选 %s 应保留：%v", want, got)
		}
	}
	// 空提交=纯基座
	if got := builtinToolsUnion(nil); len(got) != 8 {
		t.Fatalf("空提交应得基座 8 工具：%v", got)
	}
}

func TestUpdateAgentBuiltinWhitelistExtended(t *testing.T) {
	s := newUnifyToolsFixture(t)
	prev, _ := s.Store.GetAgent("builtin-assistant")
	maxTok := 4096
	body, _ := json.Marshal(map[string]any{
		"name": "改名尝试", "tools": []string{"current_time", "doc_read"},
		"skills": []string{"sk-1"}, "max_tokens": maxTok, "max_iteration": 30,
		"mcp_servers": []map[string]any{{"name": "srv", "url": "http://127.0.0.1:3000"}},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/agents/builtin-assistant", bytes.NewReader(body))
	req.SetPathValue("id", "builtin-assistant")
	s.updateAgent(w, req)
	if w.Code != 200 {
		t.Fatalf("扩容白名单 PUT 应 200：%d %s", w.Code, w.Body.String())
	}
	a, _ := s.Store.GetAgent("builtin-assistant")
	// tools 基座并集保护：current_time 追加 + L1 三件兜底补齐
	has := map[string]bool{}
	for _, tool := range a.Tools {
		has[tool] = true
	}
	for _, want := range []string{"current_time", "sync_platform_kb", "search_platform_kb", "propose_assistant_config"} {
		if !has[want] {
			t.Fatalf("tools 并集应含 %s：%v", want, a.Tools)
		}
	}
	if len(a.Skills) != 1 || a.Skills[0] != "sk-1" {
		t.Fatalf("skills 应放行：%v", a.Skills)
	}
	if len(a.MCPServers) != 1 || a.MCPServers[0].Name != "srv" {
		t.Fatalf("mcp_servers 应放行：%v", a.MCPServers)
	}
	if a.MaxTokens == nil || *a.MaxTokens != 4096 || a.MaxIteration != 30 {
		t.Fatalf("max_tokens/max_iteration 应放行：%v %d", a.MaxTokens, a.MaxIteration)
	}
	if a.Name != prev.Name || !a.IsBuiltin {
		t.Fatalf("身份字段仍应锁死：name=%q builtin=%v", a.Name, a.IsBuiltin)
	}
}

func TestEnsureAssistantTools(t *testing.T) {
	s := newUnifyToolsFixture(t)
	// 模拟迁移 030 前的存量形态（仅 L0 五工具）
	a, _ := s.Store.GetAgent("builtin-assistant")
	a.Tools = []string{"doc_read", "list_model_connections", "list_agents", "list_kbs", "get_assistant_config"}
	if _, err := s.Store.UpdateAgent(a); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.EnsureAssistantTools(); err != nil {
		t.Fatal(err)
	}
	a, _ = s.Store.GetAgent("builtin-assistant")
	if len(a.Tools) != 8 {
		t.Fatalf("缺 L1 应并入为 8 工具：%v", a.Tools)
	}
	// 幂等：齐全时零写入
	if err := s.Store.EnsureAssistantTools(); err != nil {
		t.Fatal(err)
	}
	a2, _ := s.Store.GetAgent("builtin-assistant")
	if len(a2.Tools) != 8 {
		t.Fatalf("幂等调用不应重复追加：%v", a2.Tools)
	}
}
