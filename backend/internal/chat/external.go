package chat

// external.go M13/D-O13（方案 §6.16）：外部推理后端执行路径。
//
// 外部 CLI（claude-code/opencode/aider）不走平台内 eino-adk 管线：无消息列表、无原生工具，
// 以单条组装提示运行子进程。能力降级语义（§6.16.4）：
//   - 对话/流式：✅（CLI stdout 行级转译为 message.delta / tool.call）
//   - 技能：→ instruction 注入 ⚠️
//   - MCP 工具：→ prompt 段落注入 ⚠️（仅告知存在，无法调用）
//   - AgentAsTool / 工作流：❌（P1 不暴露）
//   - 中断恢复：⚠️ 仅 Cancel（终止子进程）
// 模型连接（agent.model_conn_id）对外部后端不生效：模型由 CLI 自身配置决定。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/inference"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// historyLimit 外部提示中携带的历史轮数上限（CLI 无服务端记忆，单次拼入）。
const historyLimit = 10

// runExternal 外部推理后端一次运行的完整编排（镜像 Run 的 inprocess 路径骨架）。
func (s *Service) runExternal(ctx context.Context, conv *store.Conversation, agent *store.Agent, runID, input string, emit EmitFn) (*RunResult, error) {
	name := agent.InferenceBackend
	b := s.Inference.Get(name)
	if b == nil {
		err := fmt.Errorf("未知推理后端 %q", name)
		s.emitAndRecord(ctx, conv, runID, newEvent("run.error", runID, map[string]any{"code": "backend_not_found", "message": err.Error()}), emit)
		return nil, err
	}

	res := &RunResult{}

	// 1) 持久化用户消息 + 历史
	userMsg := &store.Message{ConversationID: conv.ID, Role: "user", Content: input}
	if _, err := s.Store.InsertMessage(userMsg); err != nil {
		return nil, fmt.Errorf("save user message: %w", err)
	}
	history, err := s.Store.ListMessages(conv.ID)
	if err != nil {
		return nil, err
	}
	histMsgs := BuildHistoryMessages(history)

	// 2) 可取消 ctx（Stop 复用 cancels 表 → 终止 CLI 子进程）
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancels[conv.ID] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.cancels, conv.ID)
		s.mu.Unlock()
		cancel()
	}()

	// 3) 降级注入内容：技能 instruction + MCP 段落 + run.started（backend/版本/降级警告）
	skills := s.loadAgentSkills(agent)
	caps := b.Capabilities()
	warnings := degradationWarnings(caps, len(skills), len(agent.MCPServers))

	start := time.Now()
	startData := map[string]any{
		"conversation_id": conv.ID,
		"agent_name":      agent.Name,
		"model":           fmt.Sprintf("%s（由 %s 自身配置决定）", name, name),
		"backend":         name,
	}
	if len(warnings) > 0 {
		startData["warnings"] = warnings
	}
	s.emitAndRecord(runCtx, conv, runID, newEvent("run.started", runID, startData), emit)

	if len(skills) > 0 {
		sk := make([]map[string]string, 0, len(skills))
		for _, k := range skills {
			sk = append(sk, map[string]string{"id": k.ID, "name": k.Name})
		}
		s.emitAndRecord(runCtx, conv, runID, newEvent("skill.loaded", runID, map[string]any{"skills": sk}), emit)
	}

	// 4) 知识库召回（公共段；命中内容以 System 消息挂在 histMsgs 尾部）
	baseLen := len(histMsgs)
	histMsgs = s.recallKB(runCtx, conv, runID, input, histMsgs, emit)
	var kbCtx []string
	for _, m := range histMsgs[baseLen:] {
		if m.Role == schema.System {
			kbCtx = append(kbCtx, m.Content)
		}
	}

	// 5) 组装单条提示并执行（事件经转译回调流出并落库）
	prompt := assembleExternalPrompt(agent, skills, agent.MCPServers, kbCtx, histMsgs[:baseLen], input)
	var buf strings.Builder
	adaptEmit := func(ev inference.Event) {
		if ev.Type == "message.delta" {
			if d, ok := ev.Data["delta"].(string); ok {
				buf.WriteString(d)
			}
		}
		s.emitAndRecord(runCtx, conv, runID, newEvent(ev.Type, runID, ev.Data), emit)
	}
	err = b.Run(runCtx, &inference.RunRequest{Prompt: prompt, UserInput: input}, adaptEmit)

	// 6) 收尾：取消/错误/正常 → assistant 落库 + run.finished/run.error
	if err != nil {
		if errors.Is(err, context.Canceled) {
			res.Stopped = true
		} else {
			res.Error = err.Error()
			s.emitAndRecord(runCtx, conv, runID, newEvent("run.error", runID, map[string]any{"code": "backend_error", "message": err.Error()}), emit)
			return res, err
		}
	}
	if text := strings.TrimSpace(buf.String()); text != "" {
		asg := &store.Message{ConversationID: conv.ID, Role: "assistant", Content: text}
		if m, ierr := s.Store.InsertMessage(asg); ierr == nil {
			res.AssistantMessageID = m.ID
		}
	}
	finish := "stop"
	if res.Stopped {
		finish = "stopped"
	}
	s.emitAndRecord(runCtx, conv, runID, newEvent("run.finished", runID, map[string]any{
		"elapsed_ms":    time.Since(start).Milliseconds(),
		"finish_reason": finish,
	}), emit)
	return res, nil
}

// loadAgentSkills 取 agent 挂载且启用的技能（降级为 instruction 注入的数据源）。
func (s *Service) loadAgentSkills(agent *store.Agent) []*store.Skill {
	var out []*store.Skill
	for _, id := range agent.Skills {
		sk, err := s.Store.GetSkill(id)
		if err != nil || sk == nil || !sk.Enabled {
			continue
		}
		if strings.TrimSpace(sk.Instruction) == "" {
			continue
		}
		out = append(out, sk)
	}
	return out
}

// degradationWarnings §6.16.4 能力降级提示（随 run.started.warnings 事件下发）。
func degradationWarnings(caps inference.Capabilities, skillN, mcpN int) []string {
	var w []string
	if skillN > 0 && caps.SkillsMode == "instruction" {
		w = append(w, fmt.Sprintf("外部推理后端不执行平台技能工具：已将 %d 个挂载技能降级为 instruction 注入", skillN))
	}
	if mcpN > 0 && caps.MCPMode == "instruction" {
		w = append(w, fmt.Sprintf("外部推理后端不可调用 MCP 工具：%d 个 MCP 服务仅作为说明段落注入提示", mcpN))
	}
	if !caps.AgentAsTool {
		w = append(w, "外部推理后端不支持多 Agent 编排（AgentAsTool/transfer），本次仅单 Agent 对话")
	}
	if !caps.Workflow {
		w = append(w, "外部推理后端不支持工作流编排")
	}
	if !caps.Resume {
		w = append(w, "外部推理后端仅支持中断（停止），不支持断点恢复")
	}
	w = append(w, "外部推理后端的模型由 CLI 自身配置决定，Agent 模型连接不生效")
	return w
}

// assembleExternalPrompt 组装外部后端单条提示：指令 + 技能/MCP 注入 + 知识库上下文 + 历史 + 用户输入。
func assembleExternalPrompt(agent *store.Agent, skills []*store.Skill, mcps []store.MCPServer, kbCtx []string, history []*schema.Message, input string) string {
	var b strings.Builder
	if ins := strings.TrimSpace(agent.Instruction); ins != "" {
		b.WriteString("## 系统指令\n\n" + ins + "\n\n")
	}
	if len(skills) > 0 {
		b.WriteString("## 已挂载技能（以说明形式注入，非工具调用）\n\n")
		for _, sk := range skills {
			b.WriteString("### 技能：" + sk.Name + "\n" + strings.TrimSpace(sk.Instruction) + "\n\n")
		}
	}
	if len(mcps) > 0 {
		b.WriteString("## 可用 MCP 服务（仅说明，本次无法调用）\n\n")
		for _, m := range mcps {
			b.WriteString("- " + m.Name + "：" + m.URL + "\n")
		}
		b.WriteString("\n")
	}
	for _, c := range kbCtx {
		b.WriteString(c + "\n\n")
	}
	if len(history) > historyLimit {
		history = history[len(history)-historyLimit:]
	}
	if len(history) > 0 {
		b.WriteString("## 对话历史（较新在前/在后按时间序）\n\n")
		for _, m := range history {
			switch m.Role {
			case schema.User:
				b.WriteString("用户：" + m.Content + "\n")
			case schema.Assistant:
				b.WriteString("助手：" + m.Content + "\n")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("## 用户输入\n\n" + input)
	return b.String()
}
