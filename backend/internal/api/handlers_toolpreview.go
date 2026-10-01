package api

// REQ-231⑤⑥（51 号 W3，M58 半场）：装配预览与 hooks 清单数据面。
// 与并行会话的审批面 WIP（approval/assembler/迁移 037）零文件交集——本文件只读聚合，
// 不复用装配期内部函数；合并顺序与遮蔽规则与 assembleTools 同口径（内置→技能→连接器，
// 先到先得、重名遮蔽告警），口径一致性由同段注释与 20 号冒烟锁定。

import (
	"net/http"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

type previewTool struct {
	Name   string `json:"name"`
	Source string `json:"source"` // builtin | skill:{id} | connector:{name}
}

// agentToolPreview GET /api/agents/{id}/tool-preview ——「这个 agent 运行时实际会拿到哪些工具」
// 的确定性预览（治 P-H4：四源合并先到先得遮蔽此前仅 debug 快照可见）。
// 零 MCP 拨号：连接器工具取「测试连接」时点落库的 tools 清单（未测试如实注记，运行时探测）；
// 本体 facade（onto_*）随对话挂载、agent 级不可预知，以注记呈现（诚实边界）。
func (s *Server) agentToolPreview(w http.ResponseWriter, r *http.Request) {
	ag, err := s.Store.GetAgent(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	asm := s.Chat.Assembler

	var entries []*tool.Entry
	var skills []*store.Skill
	var cons []*store.Connector
	if asm != nil {
		if asm.Tools != nil {
			entries = asm.Tools.List()
		}
		if asm.Composer != nil {
			skills = asm.Composer.LoadedSkills(ag)
		}
	}
	if len(ag.Connectors) > 0 {
		cons, _ = s.Store.ListConnectors()
	}

	tools, masked, notes := buildToolPreview(ag, entries, skills, cons)
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools, "masked": masked, "notes": notes})
}

// buildToolPreview 四源合并纯函数（与 assembleTools 同口径：内置→技能→连接器，先到先得、重名遮蔽）。
func buildToolPreview(ag *store.Agent, entries []*tool.Entry, skills []*store.Skill, cons []*store.Connector) (tools, masked []previewTool, notes []string) {
	seen := map[string]bool{}
	add := func(name, source string) {
		if name == "" {
			return
		}
		if seen[name] {
			masked = append(masked, previewTool{Name: name, Source: source})
			return
		}
		seen[name] = true
		tools = append(tools, previewTool{Name: name, Source: source})
	}

	// 源 1 内置：agent.tools 勾选 ∩ 注册表（模型可见名取 Entry.Name）+ 常备原语
	byID := map[string]*tool.Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	for _, tid := range ag.Tools {
		if e, ok := byID[tid]; ok {
			add(e.Name, "builtin")
		} else {
			add(tid, "builtin") // 注册表已无此 id（历史勾选残留）——如实按 id 呈现
		}
	}
	add("http_fetch", "builtin") // 装配期常备（勾选无关）
	add("load_skill", "builtin") // 技能渐进披露载体（装配期常备）

	// 源 2 技能：挂载且启用技能的 tools 白名单并集（source=skill:{id}）
	for _, sk := range skills {
		for _, t := range sk.Tools {
			add(t, "skill:"+sk.ID)
		}
	}

	// 源 3 连接器：ag.Connectors 白名单 → 实例 tools（前缀 {实例名}__{tool}）。
	// read_only 口径：kubernetes 连接器 read_only 时装配侧摘除 kubectl_apply——预览同口径。
	byCid := map[string]*store.Connector{}
	for _, c := range cons {
		byCid[c.ID] = c
	}
	for _, cid := range ag.Connectors {
		c := byCid[cid]
		if c == nil {
			continue
		}
		names := append([]string(nil), c.Tools...)
		if c.Kind == "kubernetes" && readOnlyOf(c) {
			filtered := names[:0]
			for _, t := range names {
				if t != "kubectl_apply" {
					filtered = append(filtered, t)
				}
			}
			names = filtered
		}
		for _, t := range names {
			add(c.Name+"__"+t, "connector:"+c.Name)
		}
		if len(c.Tools) == 0 {
			// 未测试/无清单——如实给出占位注记（运行时探测）
			add(c.Name+"__(未测试·运行时探测)", "connector:"+c.Name)
		}
	}

	notes = []string{
		"本体 facade 工具（onto_*）随对话挂载运行时并入，agent 级预览不含",
		"todo_write / 项目文件工具随会话形态装配（todo_write 仅会话内、文件原语按安全根），此处为能力面预览",
		"连接器工具为「测试连接」时点清单；未测试连接器运行时探测",
	}
	return tools, masked, notes
}

// readOnlyOf kubernetes 连接器只读开关（config.read_only；与插件服务注册侧同键）。
func readOnlyOf(c *store.Connector) bool {
	if c == nil || c.Config == nil {
		return false
	}
	v, ok := c.Config["read_only"].(bool)
	return ok && v
}

// listHooks GET /api/hooks —— hook 注册真相（REQ-231⑥：Harness 页签 hooks 卡改后端读取）。
func (s *Server) listHooks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"hooks": tool.DescribeHooks()})
}
