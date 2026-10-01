package api

import (
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

// REQ-231⑤：装配预览四源合并（内置→技能→连接器，先到先得遮蔽）。
func TestBuildToolPreview(t *testing.T) {
	ag := &store.Agent{
		Tools:      []string{"current_time", "grep", "ghost_tool"}, // ghost_tool=注册表已无的历史勾选
		Connectors: []string{"c1", "c2", "c3"},
	}
	entries := []*tool.Entry{
		{ID: "current_time", Name: "current_time"},
		{ID: "grep", Name: "grep"},
	}
	skills := []*store.Skill{{ID: "s1", Tools: []string{"grep", "skill_only"}}} // grep 已被内置占坑→遮蔽
	cons := []*store.Connector{
		{ID: "c1", Kind: "mcp", Name: "mcpA", Tools: []string{"fetch", "grep"}},
		{ID: "c2", Kind: "kubernetes", Name: "kind-dev", Tools: []string{"kubectl_get", "kubectl_apply"}, Config: map[string]any{"read_only": true}},
		{ID: "c3", Kind: "ssh", Name: "sshB", Tools: nil}, // 未测试
	}

	tools, masked, notes := buildToolPreview(ag, entries, skills, cons)

	has := func(name, source string) bool {
		for _, t := range tools {
			if t.Name == name && t.Source == source {
				return true
			}
		}
		return false
	}
	// 内置勾选 + 常备原语
	if !has("current_time", "builtin") || !has("grep", "builtin") || !has("http_fetch", "builtin") || !has("load_skill", "builtin") {
		t.Fatalf("builtin tools missing: %+v", tools)
	}
	if has("current_time", "builtin") && !has("ghost_tool", "builtin") {
		t.Fatalf("ghost_tool（历史勾选）应如实按 id 呈现: %+v", tools)
	}
	// 技能并集：grep 遮蔽、skill_only 入列
	if !has("skill_only", "skill:s1") {
		t.Fatalf("skill_only missing: %+v", tools)
	}
	maskedOK := false
	for _, m := range masked {
		if m.Name == "grep" && m.Source == "skill:s1" {
			maskedOK = true
		}
	}
	if !maskedOK {
		t.Fatalf("grep 应被遮蔽告警（skill:s1）: %+v", masked)
	}
	// 连接器：mcp 前缀 + k8s read_only 摘 apply + ssh 未测试占位
	if !has("mcpA__fetch", "connector:mcpA") {
		t.Fatalf("mcpA__fetch missing: %+v", tools)
	}
	if !has("kind-dev__kubectl_get", "connector:kind-dev") {
		t.Fatalf("kind-dev__kubectl_get missing: %+v", tools)
	}
	for _, tc := range tools {
		if tc.Name == "kind-dev__kubectl_apply" {
			t.Fatalf("read_only k8s 连接器应摘除 apply: %+v", tools)
		}
	}
	if !has("sshB__(未测试·运行时探测)", "connector:sshB") {
		t.Fatalf("ssh 未测试占位 missing: %+v", tools)
	}
	if len(notes) == 0 {
		t.Fatalf("诚实边界注记缺失")
	}
}
