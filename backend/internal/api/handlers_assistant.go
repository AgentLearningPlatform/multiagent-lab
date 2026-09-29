// 内置系统级 Agent「平台助手」（M27/REQ-166/167；REQ-192/M32 配置单源归一）：
//   - GET/PUT /api/assistant/config：设置页「平台助手」分区——读写穿透 agent 内置行
//     （builtin-assistant）单源：system_prompt=行内 instruction（全量编辑，空=回落默认基座）、
//     model_conn_id/temperature 直通行内字段；assistant_config 表退役不再读写；
//   - POST /api/assistant/optimize：AI 内容优化（REQ-167）——由平台助手优化传入内容，
//     首批挂载 Agent 系统提示词与项目级约束；输出为自由文本（前端 diff 预览确认回填）；
//     REQ-192⑤ 调用留痕：请求与结果落 builtin-assistant 名下「内容优化」会话（来源标注）。
//
// 内置行不占用户 Agent 列表、不可删除（REQ-186）；配置经本面/白名单 PUT 修改（REQ-192②）。
package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// AssistantDefaultPrompt 内置固定系统提示词基座（REQ-166：角色=平台使用助手；REQ-177③：
// 按项目定位重写——智能体构建平台/七模块口径；REQ-192/M32：行内 instruction 空=回落本基座
// 「空=默认」，保存非空即全量快照（基座不可清空））。
const AssistantDefaultPrompt = `你是「平台助手」——智能体构建平台的内置使用助手（不占用用户智能体列表，仅用于平台使用辅助与内容优化）。职责：
1. 平台使用答疑：解释各模块（平台知识/智能体/项目/本体/知识库/技能/设置）的机制、概念与操作路径，给出面向操作的步骤建议；
2. 状态查询：如实报告模型连接/智能体/知识库清单与配置现状（用 list_* 工具查询，不凭记忆作答）；
3. 配置提案：协助配置变更（模型连接/提示词/温度）——用 propose_assistant_config 产出提案（两段式：暂存 10 分钟、确认前不落库），引导用户到「设置 → 平台助手」分区确认；绝不可声称已直接修改配置；
4. 内容优化：优化用户提交的文本（如智能体系统提示词、项目约束）——保持原始意图与全部约束条目完整，提升清晰度、结构与可执行性，不新增与原文无关的内容。

平台知识源（platform-knowledge/ 目录树，回答平台设计/功能问题前先检索或精读）：
- 01_整体设计/：00_整体设计（架构总览入口）、16_部署与运行、17_产品_信息架构与界面设计、31_赛道全景
- 02_智能体/：00_智能体模块、10_跨端改造、22_多类型智能体、27_外部MCP直连验证包、31_DeepSeek-Harness、32_扣子反推、33_沙箱调研、34_平台助手接入方案；01_运行时动态本体/（REQ-170 子课题四档）
- 03_项目/、06_技能/、07_设置/：各模块导读
- 04_本体/：00_本体模块、07/08/13/23/24/26 编号专题；01_开源项目分析/（14 档：oo/OpenBKN/EvoOntology/OntoFlow/可视化/工具链/semantica 存档等）
- 05_知识库/：00_知识库模块、21/25 调研、31~34 KG/Graphify/构建方式/行业报告
- docs/ 十编号：01 智能体需求、02 技术方案（§12 里程碑）、03 本体需求、04 本体方案、11/12 知识库、14 前端改造、15 登记簿、18 REQ 注册表、20 冒烟清单

工具优先级：平台知识问题先 sync_platform_kb 同步再 search_platform_kb 检索（向量 topK=5；不可用时用 doc_read 按上述路径词法精读）；清单与配置现状用 list_* / get_assistant_config。
风格：简明、面向操作、中文优先；不确定的平台细节如实说明并建议查看对应文档，不臆造功能。`

const optimizeTimeout = 60 * time.Second

// derefStr nil 安全解引用（agent.ModelConnID *string ↔ AssistantConfig.ModelConnID string）。
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// strPtrPtr 语义映射：空串=清除覆盖（nil，跟随全局默认）；非空=覆盖值。
func strPtrPtr(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// assistantConfigGet GET /api/assistant/config。
// REQ-192/M32：读写穿透 agent 内置行（builtin-assistant）单源——system_prompt=行内
// instruction（空=回落默认基座，REQ-177「所见即生效」语义保留）、model_conn_id/temperature
// 直通行内字段；assistant_config 表退役。
func (s *Server) assistantConfigGet(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.GetAgent("builtin-assistant")
	if err != nil {
		writeErr(w, err)
		return
	}
	c := &store.AssistantConfig{SystemPrompt: a.Instruction, ModelConnID: derefStr(a.ModelConnID), Temperature: a.Temperature}
	if strings.TrimSpace(c.SystemPrompt) == "" {
		c.SystemPrompt = AssistantDefaultPrompt
	}
	writeJSON(w, http.StatusOK, c)
}

// applyAssistantConfig REQ-192：将平台助手配置写入 agent 内置行单源（设置页 PUT 与
// L1 提案 apply 共用）：instruction 空回落默认基座（基座不可清空）；model/temperature 直写。
func (s *Server) applyAssistantConfig(in *store.AssistantConfig) error {
	a, err := s.Store.GetAgent("builtin-assistant")
	if err != nil {
		return err
	}
	prompt := strings.TrimSpace(in.SystemPrompt)
	if prompt == "" {
		prompt = AssistantDefaultPrompt
	}
	a.Instruction = prompt
	a.ModelConnID = strPtrPtr(in.ModelConnID)
	a.Temperature = in.Temperature
	_, err = s.Store.UpdateAgent(a)
	return err
}

// assistantConfigPut PUT /api/assistant/config：保存提示词/模型覆盖/温度（写内置行单源）。
// 语义（REQ-192/M32）：system_prompt 全量编辑（空=恢复默认基座全文）；temperature nil=不变。
func (s *Server) assistantConfigPut(w http.ResponseWriter, r *http.Request) {
	var in store.AssistantConfig
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if in.Temperature != nil && (*in.Temperature < 0 || *in.Temperature > 2) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "temperature 取值 0~2"})
		return
	}
	if err := s.applyAssistantConfig(&in); err != nil {
		writeErr(w, err)
		return
	}
	s.assistantConfigGet(w, r)
}

// assistantOptimize POST /api/assistant/optimize：body {kind, content} → {optimized}。
func (s *Server) assistantOptimize(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind    string `json:"kind"`
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content 必填"})
		return
	}
	var task string
	switch in.Kind {
	case "agent_instruction":
		task = "以下是平台中一个智能体的系统提示词。请优化它：保持原始意图、约束与输出要求完整，提升清晰度、结构（可用分节/列表）与可执行性；不新增与原文无关的能力。"
	case "project_constraints":
		task = "以下是平台中一个项目的级约束文本（将注入项目内所有成员智能体）。请优化它：保持全部约束条目完整不丢失，提升结构清晰度与可执行性；不新增与原文无关的约束。"
	default:
		task = "请优化以下文本内容：保持原意完整，提升清晰度与结构。"
	}
	// REQ-192/M32：读内置行单源（instruction 即完整系统提示词，空回落默认基座；模型/温度随行）
	a, err := s.Store.GetAgent("builtin-assistant")
	if err != nil {
		writeErr(w, err)
		return
	}
	system := a.Instruction
	if strings.TrimSpace(system) == "" {
		system = AssistantDefaultPrompt
	}
	ctx, cancel := context.WithTimeout(r.Context(), optimizeTimeout)
	defer cancel()
	temp := 0.3
	if a.Temperature != nil {
		temp = *a.Temperature
	}
	optimized, err := chat.GenerateText(ctx, s.Store, s.Box, derefStr(a.ModelConnID), system, task+"\n\n【待优化内容】\n"+content, &temp)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "平台助手优化失败: " + err.Error()})
		return
	}
	if strings.TrimSpace(optimized) == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "平台助手返回空结果"})
		return
	}
	// REQ-192⑤ 调用留痕：落 builtin-assistant 名下「内容优化」会话（user=优化请求、
	// assistant=优化结果，meta source=content-optimization 来源标注，历史还原可见）
	if convID, cerr := s.Store.EnsureAssistantContentConv(); cerr == nil {
		kindLabel := in.Kind
		if kindLabel == "" {
			kindLabel = "general"
		}
		_, _ = s.Store.InsertMessage(&store.Message{ConversationID: convID, Role: "user",
			Content: "【内容优化 · " + kindLabel + "】\n" + content, Meta: `{"source":"content-optimization","kind":"` + kindLabel + `"}`})
		_, _ = s.Store.InsertMessage(&store.Message{ConversationID: convID, Role: "assistant",
			Content: optimized, Meta: `{"source":"content-optimization","kind":"` + kindLabel + `"}`})
	}
	writeJSON(w, http.StatusOK, map[string]any{"optimized": optimized})
}

// assistantProposalGet M-O14 阶段三（REQ-186 L1 两段式）：拉取最新暂存提案（不消费）。
func (s *Server) assistantProposalGet(w http.ResponseWriter, r *http.Request) {
	p := chat.PeekAssistantProposal()
	if p == nil {
		writeJSON(w, http.StatusOK, map[string]any{"pending": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending": true, "proposal": p})
}

// assistantProposalApply POST /api/assistant/proposal/{id}/apply：取出提案并写入配置（用户在设置页确认触发）。
// REQ-192/M32④：提案写入目标随配置归一——写 agent 内置行单源（不再落 assistant_config）。
func (s *Server) assistantProposalApply(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := chat.TakeAssistantProposal(id)
	if p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "提案不存在或已过期（10 分钟 TTL）"})
		return
	}
	if err := s.applyAssistantConfig(p.Proposed); err != nil {
		writeErr(w, err)
		return
	}
	a, err := s.Store.GetAgent("builtin-assistant")
	if err != nil {
		writeErr(w, err)
		return
	}
	c := &store.AssistantConfig{SystemPrompt: a.Instruction, ModelConnID: derefStr(a.ModelConnID), Temperature: a.Temperature}
	if strings.TrimSpace(c.SystemPrompt) == "" {
		c.SystemPrompt = AssistantDefaultPrompt
	}
	writeJSON(w, http.StatusOK, map[string]any{"applied": true, "config": c})
}

// assistantProposalDiscard POST /api/assistant/proposal/{id}/discard：忽略提案（不落库）。
func (s *Server) assistantProposalDiscard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := chat.TakeAssistantProposal(id)
	if p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "提案不存在或已过期"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"discarded": true})
}
