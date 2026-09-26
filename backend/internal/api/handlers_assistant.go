// 内置系统级 Agent「平台助手」（M27/REQ-166/167）：
//   - GET/PUT /api/assistant/config：设置页「平台助手」分区（提示词微调/模型覆盖/温度）；
//   - POST /api/assistant/optimize：AI 内容优化（REQ-167）——由平台助手优化传入内容，
//     首批挂载 Agent 系统提示词与项目级约束；输出为自由文本（前端 diff 预览确认回填）。
//
// 平台助手不落 agent 表：inprocess、不占用户 Agent 列表、不可删除（REQ-166）。
package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// assistantDefaultPrompt 内置固定系统提示词（REQ-166：角色=平台使用助手）。
const assistantDefaultPrompt = `你是「平台助手」——多智能体构建平台（Eino/ADK + React）的内置使用助手。职责：
1. 解释平台各模块（智能体/项目/本体/知识库/技能/设置）的机制与概念；
2. 优化用户的文本内容（系统提示词、项目约束等），保持原意与约束完整，提升清晰度、结构与可执行性；
3. 辅助用户完成平台配置操作。
风格：简明、面向操作、中文优先。`

const optimizeTimeout = 60 * time.Second

// assistantConfigGet GET /api/assistant/config
func (s *Server) assistantConfigGet(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.GetAssistantConfig()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// assistantConfigPut PUT /api/assistant/config：保存提示词微调/模型覆盖/温度。
// 语义：system_prompt/model_conn_id 全量覆盖（前端回显后提交）；temperature nil = 清除（跟随默认采样）。
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
	if err := s.Store.SaveAssistantConfig(&in); err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.Store.GetAssistantConfig()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
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
	cfg, err := s.Store.GetAssistantConfig()
	if err != nil {
		writeErr(w, err)
		return
	}
	system := assistantDefaultPrompt
	if strings.TrimSpace(cfg.SystemPrompt) != "" { // REQ-166：用户微调追加到内置提示词后
		system += "\n\n# 用户微调\n" + strings.TrimSpace(cfg.SystemPrompt)
	}
	ctx, cancel := context.WithTimeout(r.Context(), optimizeTimeout)
	defer cancel()
	optimized, err := chat.GenerateText(ctx, s.Store, s.Box, cfg.ModelConnID, system, task+"\n\n【待优化内容】\n"+content, cfg.Temperature)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "平台助手优化失败: " + err.Error()})
		return
	}
	if strings.TrimSpace(optimized) == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "平台助手返回空结果"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"optimized": optimized})
}
