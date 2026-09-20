// Package chat 负责 Agent 运行时装配与执行。
// 每次运行按当前配置装配（配置驱动，验收5），支持单 Agent 直聊与项目多 Agent 协作（M4）。
package chat

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

// Assembler 从平台配置装配 Eino Agent。
type Assembler struct {
	Store *store.Store
	Box   *secrets.Box
	Tools *tool.Registry
}

// BuildResult 装配产物。
type BuildResult struct {
	Runner     *adk.Runner
	AgentName  string            // 根 Agent 名
	ModelLabel string            // 根 Agent 的连接名@模型名
	ConnID     string            // 根 Agent 使用的连接
	SourceOf   map[string]string // 工具 function name -> 来源（tool.call 事件 source 标注）
	Warnings   []string          // 装配期告警（并入 run.started data）
}

// Runtime 兼容别名（历史调用方）。
type Runtime = BuildResult

// Assemble 按会话归属装配 Runner：agent 直聊单 Agent；项目会话按 collab_mode 装配多 Agent（§6.4）。
func (a *Assembler) Assemble(ctx context.Context, agent *store.Agent, conv *store.Conversation) (*BuildResult, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent is nil")
	}
	if conv == nil || conv.Scope != "project" {
		return a.assembleSingle(ctx, agent)
	}
	if conv.ProjectID == nil || *conv.ProjectID == "" {
		return nil, fmt.Errorf("project conversation missing project_id")
	}
	p, err := a.Store.GetProject(*conv.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("load project: %w", err)
	}
	return a.assembleProject(ctx, p)
}

// assembleSingle 单 Agent 装配（agent 直聊 / 项目 single 模式）。
func (a *Assembler) assembleSingle(ctx context.Context, ag *store.Agent) (*BuildResult, error) {
	b, err := a.buildOne(ctx, ag)
	if err != nil {
		return nil, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: b.Inst, EnableStreaming: true})
	return &BuildResult{
		Runner:     runner,
		AgentName:  ag.Name,
		ModelLabel: b.Meta.ModelLabel,
		ConnID:     b.Meta.ConnID,
		SourceOf:   b.Meta.SourceOf,
		Warnings:   b.Meta.Warnings,
	}, nil
}

// assembleProject 项目多 Agent 装配（§6.4：agent_as_tool 推荐 / transfer 对照 / single）。
// workflow_mode 非 free 时告警降级为 free（工作流编排 P1 后续）。
func (a *Assembler) assembleProject(ctx context.Context, p *store.Project) (*BuildResult, error) {
	members, err := a.Store.ListProjectAgents(p.ID)
	if err != nil {
		return nil, fmt.Errorf("load project members: %w", err)
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("项目还没有成员智能体，请先在项目配置中添加成员")
	}

	// 协调者：优先 project.coordinator，缺省第一个成员
	coordID := p.Coordinator
	if coordID == "" {
		coordID = members[0].AgentID
	}
	var warns []string

	var coord *store.Agent
	var subs []*store.Agent
	for _, m := range members {
		ag, gerr := a.Store.GetAgent(m.AgentID)
		if gerr != nil {
			return nil, fmt.Errorf("load member agent %s: %w", m.AgentID, gerr)
		}
		if m.AgentID == coordID {
			coord = ag
			continue
		}
		subs = append(subs, ag)
	}
	if coord == nil {
		// coordinator 指向的 Agent 不在成员表（数据不一致）：首个成员兜底
		coord, err = a.Store.GetAgent(members[0].AgentID)
		if err != nil {
			return nil, err
		}
		warns = append(warns, fmt.Sprintf("项目协调者 %q 不在成员列表，已回退首个成员", p.Coordinator))
	}

	// workflow_mode 预留（P1：Sequential/Parallel/Loop 包裹）
	if p.WorkflowMode != "" && p.WorkflowMode != "free" {
		warns = append(warns, fmt.Sprintf("工作流模式 %q 暂未启用，按自由协作处理", p.WorkflowMode))
	}

	mode := p.CollabMode
	if mode == "" {
		mode = "agent_as_tool"
	}
	if len(subs) == 0 {
		if mode != "single" {
			warns = append(warns, "项目仅有一个成员，按单 Agent 运行")
		}
		mode = "single"
	}

	switch mode {
	case "agent_as_tool":
		return a.assembleAgentAsTool(ctx, coord, subs, warns)
	case "transfer":
		return a.assembleTransfer(ctx, coord, subs, warns)
	case "single":
		return a.assembleSingle(ctx, coord)
	default:
		return nil, fmt.Errorf("不支持的协作模式 %q", p.CollabMode)
	}
}

// assembleAgentAsTool 协调者以工具形式调用成员（推荐路径，ADK AgentTool）。
func (a *Assembler) assembleAgentAsTool(ctx context.Context, coord *store.Agent, subs []*store.Agent, warns []string) (*BuildResult, error) {
	// 协调者基础装配（模型 + 勾选工具）
	cm, label, connID, err := a.buildModel(ctx, coord)
	if err != nil {
		return nil, fmt.Errorf("build coordinator: %w", err)
	}
	composed, err := a.Tools.Compose(ctx, coord.Tools)
	if err != nil {
		return nil, fmt.Errorf("compose coordinator tools: %w", err)
	}
	tools := append([]einotool.BaseTool{}, composed.Tools...)
	src := copySourceOf(composed.SourceOf)
	allWarns := append(append([]string{}, composed.Warnings...), warns...)

	// 成员 → AgentTool（成员全量装配：各自模型/工具；ADK 要求 Name/Description 非空）
	for _, s := range subs {
		if err := requireAgentToolFields(s); err != nil {
			return nil, err
		}
		sb, berr := a.buildOne(ctx, s)
		if berr != nil {
			return nil, fmt.Errorf("build member %q: %w", s.Name, berr)
		}
		tools = append(tools, adk.NewAgentTool(ctx, sb.Inst))
		for k, v := range sb.Meta.SourceOf {
			if _, dup := src[k]; !dup {
				src[k] = v
			}
		}
		allWarns = append(allWarns, sb.Meta.Warnings...)
		src[s.Name] = fmt.Sprintf("agent:%s", s.ID) // AgentTool 的 function name = 成员名
	}

	inst, err := newChatModelAgent(ctx, coord, cm, tools)
	if err != nil {
		return nil, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: inst, EnableStreaming: true})
	return &BuildResult{
		Runner: runner, AgentName: coord.Name,
		ModelLabel: label, ConnID: connID,
		SourceOf: src, Warnings: allWarns,
	}, nil
}

// assembleTransfer 协调者把控制权转移给成员（ADK SetSubAgents，对照路径）。
func (a *Assembler) assembleTransfer(ctx context.Context, coord *store.Agent, subs []*store.Agent, warns []string) (*BuildResult, error) {
	cm, label, connID, err := a.buildModel(ctx, coord)
	if err != nil {
		return nil, fmt.Errorf("build coordinator: %w", err)
	}
	composed, err := a.Tools.Compose(ctx, coord.Tools)
	if err != nil {
		return nil, fmt.Errorf("compose coordinator tools: %w", err)
	}
	src := copySourceOf(composed.SourceOf)
	allWarns := append(append([]string{}, composed.Warnings...), warns...)

	inst, err := newChatModelAgent(ctx, coord, cm, composed.Tools)
	if err != nil {
		return nil, err
	}
	subAgents := make([]adk.Agent, 0, len(subs))
	for _, s := range subs {
		sb, berr := a.buildOne(ctx, s)
		if berr != nil {
			return nil, fmt.Errorf("build member %q: %w", s.Name, berr)
		}
		subAgents = append(subAgents, sb.Inst)
		for k, v := range sb.Meta.SourceOf {
			if _, dup := src[k]; !dup {
				src[k] = v
			}
		}
		allWarns = append(allWarns, sb.Meta.Warnings...)
	}
	root, err := adk.SetSubAgents(ctx, inst, subAgents)
	if err != nil {
		return nil, fmt.Errorf("set sub agents: %w", err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: root, EnableStreaming: true})
	return &BuildResult{
		Runner: runner, AgentName: coord.Name,
		ModelLabel: label, ConnID: connID,
		SourceOf: src, Warnings: allWarns,
	}, nil
}

// buildOne 装配单个 Agent 实例：模型解析（显式>默认，M3）→ 工具合并（§6.8）→ ChatModelAgent。
func (a *Assembler) buildOne(ctx context.Context, ag *store.Agent) (*agentBuild, error) {
	cm, label, connID, err := a.buildModel(ctx, ag)
	if err != nil {
		return nil, err
	}
	// 工具合并（M5）：agent.tools 勾选 → 注册表实例化。
	// M7/M9 扩展点：技能白名单、MCP servers、本体 onto_* 均并入同一装配管线。
	composed, err := a.Tools.Compose(ctx, append([]string{}, ag.Tools...))
	if err != nil {
		return nil, fmt.Errorf("compose tools: %w", err)
	}
	inst, err := newChatModelAgent(ctx, ag, cm, composed.Tools)
	if err != nil {
		return nil, err
	}
	return &agentBuild{
		Inst: inst,
		Meta: &agentMeta{
			ModelLabel: label,
			ConnID:     connID,
			SourceOf:   composed.SourceOf,
			Warnings:   composed.Warnings,
		},
	}, nil
}

// agentBuild 单个 Agent 装配产物（可独立运行，也可并入协作结构）。
type agentBuild struct {
	Inst adk.Agent
	Meta *agentMeta
}

// agentMeta 单 Agent 元信息。
type agentMeta struct {
	ModelLabel string
	ConnID     string
	SourceOf   map[string]string
	Warnings   []string
}

// newChatModelAgent 构造 ADK ChatModelAgent（统一 ToolsConfig / EmitInternalEvents）。
func newChatModelAgent(ctx context.Context, ag *store.Agent, cm *openai.ChatModel, tools []einotool.BaseTool) (adk.Agent, error) {
	inst, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          ag.Name,
		Description:   ag.Description,
		Instruction:   ag.Instruction, // M7：ComposeInstruction 技能注入
		Model:         cm,
		MaxIterations: normalizeMaxIter(ag.MaxIteration),
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
			// agent_as_tool / transfer 模式下内层 Agent 事件流出（subagent 事件时间线，M4）
			EmitInternalEvents: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create adk agent %q: %w", ag.Name, err)
	}
	return inst, nil
}

// requireAgentToolFields AgentTool 要求 Name/Description 非空（装配期前置校验，给清晰错误）。
func requireAgentToolFields(ag *store.Agent) error {
	if ag.Name == "" {
		return fmt.Errorf("成员智能体缺少名称，无法作为工具挂载")
	}
	if ag.Description == "" {
		return fmt.Errorf("成员智能体 %q 缺少描述，无法作为工具挂载（供协调者判断何时调用）", ag.Name)
	}
	return nil
}

// buildModel 解析模型连接（Agent 显式指定 > 全局默认 chat 连接）并构造 ChatModel。
func (a *Assembler) buildModel(ctx context.Context, ag *store.Agent) (*openai.ChatModel, string, string, error) {
	var rec *store.ConnectionRecord
	var err error
	if ag.ModelConnID != nil && *ag.ModelConnID != "" {
		rec, err = a.Store.GetConnectionRecord(*ag.ModelConnID)
		if err != nil {
			return nil, "", "", fmt.Errorf("resolve model connection %s: %w", *ag.ModelConnID, err)
		}
	} else {
		def, derr := a.Store.GetDefaultConnection("chat")
		if derr != nil {
			return nil, "", "", derr
		}
		if def == nil {
			return nil, "", "", &ModelNotConfiguredError{}
		}
		rec, err = a.Store.GetConnectionRecord(def.ID)
		if err != nil {
			return nil, "", "", err
		}
	}
	if !rec.Conn.Enabled {
		return nil, "", "", fmt.Errorf("模型连接 %q 已停用，请在「设置-模型连接」启用或更换", rec.Conn.Name)
	}

	apiKey := ""
	if len(rec.Encrypted) > 0 {
		apiKey, err = a.Box.Decrypt(rec.Encrypted)
		if err != nil {
			return nil, "", "", fmt.Errorf("decrypt api key: %w", err)
		}
	}

	cfg := &openai.ChatModelConfig{
		APIKey:  apiKey,
		BaseURL: rec.Conn.BaseURL,
		Model:   rec.Conn.ModelName,
	}
	if ag.Temperature != nil {
		t := float32(*ag.Temperature)
		cfg.Temperature = &t
	}
	if ag.MaxTokens != nil {
		mt := *ag.MaxTokens
		cfg.MaxTokens = &mt
	}
	cm, err := openai.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, "", "", fmt.Errorf("create chat model: %w", err)
	}
	return cm, rec.Conn.Name + "@" + rec.Conn.ModelName, rec.Conn.ID, nil
}

func copySourceOf(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func normalizeMaxIter(n int) int {
	if n <= 0 {
		return 25
	}
	return n
}

// ModelNotConfiguredError 未配置可用 chat 模型连接。
type ModelNotConfiguredError struct{}

func (e *ModelNotConfiguredError) Error() string {
	return "未配置可用的对话模型连接，请先到「设置-模型连接」添加并启用（可设为默认）"
}

// BuildHistoryMessages 将对话历史转为 Eino 消息（多轮记忆还原）。
func BuildHistoryMessages(msgs []*store.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "user":
			out = append(out, schema.UserMessage(m.Content))
		case "assistant":
			out = append(out, schema.AssistantMessage(m.Content, nil))
		case "system":
			out = append(out, schema.SystemMessage(m.Content))
		}
	}
	return out
}
