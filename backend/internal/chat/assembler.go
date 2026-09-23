// Package chat 负责 Agent 运行时装配与执行。
// 每次运行按当前配置装配（配置驱动，验收5），支持单 Agent 直聊与项目多 Agent 协作（M4）。
// M9：技能挂载生效（§6.12 注入）+ MCP servers 工具装载（§6.11，失败降级）。
package chat

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/fsutil"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/ontology"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/skill"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

// mcpFetchTimeout 单个 MCP server 连接+列工具的超时（§13：不做重试风暴）。
const mcpFetchTimeout = 10 * time.Second

// Assembler 从平台配置装配 Eino Agent。
type Assembler struct {
	Store     *store.Store
	Box       *secrets.Box
	Tools     *tool.Registry
	Composer  *skill.Composer   // M9：技能注入（nil 时技能不生效）
	Ontology  *ontology.Service // M8：本体对接（nil 时本体不生效）
	FilesRoot string            // M11：项目文件根目录（空=save_file 不启用），如 ./data/projects
	CheckPoints CheckPoints     // M11 收尾：中断检查点存储（nil=中断不持久化、无法恢复）
}

// BuildResult 装配产物。
type BuildResult struct {
	Runner          *adk.Runner
	AgentName       string            // 根 Agent 名
	ModelLabel      string            // 根 Agent 的连接名@模型名
	ConnID          string            // 根 Agent 使用的连接
	SourceOf        map[string]string // 工具 function name -> 来源（tool.call 事件 source 标注）
	Warnings        []string          // 装配期告警（并入 run.started data）
	LoadedSkills    []*store.Skill    // 本次运行生效的技能（skill.loaded 事件，M9）
	OntoUnavailable *ontology.Issue   // M8：本体挂载降级（ontology.unavailable 事件，§6.10-4）
}

// Runtime 兼容别名（历史调用方）。
type Runtime = BuildResult

// Assemble 按会话归属装配 Runner：agent 直聊单 Agent；项目会话按 collab_mode 装配多 Agent（§6.4）。
// assembleScope 装配期会话范围（M8 mount + M11 项目文件上下文）。
type assembleScope struct {
	Mount          string // 本体运行方案 profile id（空=未挂载）
	ProjectID      string // 项目文件目录归属（M11；空=agent 会话）
	ConversationID string // 产物归属会话
	SkillsDisabled bool   // 会话级技能开关：enable_skills=false 时本次装配不注入技能
}

func (a *Assembler) Assemble(ctx context.Context, agent *store.Agent, conv *store.Conversation) (*BuildResult, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent is nil")
	}
	// M8：会话挂载的运行方案（O-6；runtime_profile_id + ontology_enabled）+ M11 项目上下文
	sc := assembleScope{}
	if conv != nil {
		sc.Mount, _ = ontology.MountOf(conv.RuntimeProfileID, conv.OntologyEnabled)
		sc.ConversationID = conv.ID
		if conv.ProjectID != nil {
			sc.ProjectID = *conv.ProjectID
		}
		// 会话级技能开关（§6.12）：false 时本次装配剥离 Skills（单点生效，覆盖指令注入 + 工具白名单 + skill.loaded）
		if conv.EnableSkills != nil && !*conv.EnableSkills {
			sc.SkillsDisabled = true
		}
	}
	if conv == nil || conv.Scope != "project" {
		return a.assembleSingle(ctx, sc.skillGated(agent), sc)
	}
	if sc.ProjectID == "" {
		return nil, fmt.Errorf("project conversation missing project_id")
	}
	p, err := a.Store.GetProject(sc.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("load project: %w", err)
	}
	return a.assembleProject(ctx, p, sc)
}

// skillGated 会话级技能开关：enable_skills=false 时返回 Skills 置空的 Agent 副本。
// ComposeInstruction（注入指令）与 assembleTools（技能工具白名单）均读取 ag.Skills，
// 剥离后二者自然跳过，LoadedSkills 随之为空 → runner 不发出 skill.loaded 事件。
func (sc assembleScope) skillGated(ag *store.Agent) *store.Agent {
	if ag == nil || !sc.SkillsDisabled {
		return ag
	}
	cp := *ag
	cp.Skills = []string{}
	return &cp
}

// assembleSingle 单 Agent 装配（agent 直聊 / 项目 single 模式）。
func (a *Assembler) assembleSingle(ctx context.Context, ag *store.Agent, sc assembleScope) (*BuildResult, error) {
	b, err := a.buildOne(ctx, ag, sc)
	if err != nil {
		return nil, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: b.Inst, EnableStreaming: true, CheckPointStore: a.CheckPoints})
	return &BuildResult{
		Runner:          runner,
		AgentName:       ag.Name,
		ModelLabel:      b.Meta.ModelLabel,
		ConnID:          b.Meta.ConnID,
		SourceOf:        b.Meta.SourceOf,
		Warnings:        b.Meta.Warnings,
		LoadedSkills:    b.Meta.LoadedSkills,
		OntoUnavailable: b.Meta.OntoUnavailable,
	}, nil
}

// assembleProject 项目多 Agent 装配（§6.4：agent_as_tool 推荐 / transfer 对照 / single）。
// workflow_mode 非 free 时告警降级为 free（工作流编排 P1 后续）。
func (a *Assembler) assembleProject(ctx context.Context, p *store.Project, sc assembleScope) (*BuildResult, error) {
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
		ag = sc.skillGated(ag) // 会话级技能开关：项目成员同样受控
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
		coord = sc.skillGated(coord)
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
		return a.assembleAgentAsTool(ctx, coord, subs, warns, sc)
	case "transfer":
		return a.assembleTransfer(ctx, coord, subs, warns, sc)
	case "single":
		return a.assembleSingle(ctx, coord, sc)
	default:
		return nil, fmt.Errorf("不支持的协作模式 %q", p.CollabMode)
	}
}

// assembleAgentAsTool 协调者以工具形式调用成员（推荐路径，ADK AgentTool）。
func (a *Assembler) assembleAgentAsTool(ctx context.Context, coord *store.Agent, subs []*store.Agent, warns []string, sc assembleScope) (*BuildResult, error) {
	// 协调者基础装配（模型 + 技能 + MCP + 勾选工具）
	cm, label, connID, err := a.buildModel(ctx, coord)
	if err != nil {
		return nil, fmt.Errorf("build coordinator: %w", err)
	}
	tbc, err := a.assembleTools(ctx, coord, sc)
	if err != nil {
		return nil, fmt.Errorf("compose coordinator tools: %w", err)
	}
	tools := append([]einotool.BaseTool{}, tbc.Tools...)
	src := copySourceOf(tbc.SourceOf)
	allWarns := append(append([]string{}, tbc.Warnings...), warns...)
	loaded := appendLoadedSkills(nil, tbc.LoadedSkills)

	// 成员 → AgentTool（成员全量装配：各自模型/技能/MCP/工具；ADK 要求 Name/Description 非空）
	for _, s := range subs {
		if err := requireAgentToolFields(s); err != nil {
			return nil, err
		}
		sb, berr := a.buildOne(ctx, s, sc)
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
		loaded = appendLoadedSkills(loaded, sb.Meta.LoadedSkills)
		src[s.Name] = fmt.Sprintf("agent:%s", s.ID) // AgentTool 的 function name = 成员名
	}

	inst, err := newChatModelAgent(ctx, coord.Name, coord.Description, a.composeInstructionWithScope(coord, sc), coord.MaxIteration, cm, tools)
	if err != nil {
		return nil, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: inst, EnableStreaming: true, CheckPointStore: a.CheckPoints})
	return &BuildResult{
		Runner: runner, AgentName: coord.Name,
		ModelLabel: label, ConnID: connID,
		SourceOf: src, Warnings: allWarns, LoadedSkills: loaded,
	}, nil
}

// assembleTransfer 协调者把控制权转移给成员（ADK SetSubAgents，对照路径）。
func (a *Assembler) assembleTransfer(ctx context.Context, coord *store.Agent, subs []*store.Agent, warns []string, sc assembleScope) (*BuildResult, error) {
	cm, label, connID, err := a.buildModel(ctx, coord)
	if err != nil {
		return nil, fmt.Errorf("build coordinator: %w", err)
	}
	tbc, err := a.assembleTools(ctx, coord, sc)
	if err != nil {
		return nil, fmt.Errorf("compose coordinator tools: %w", err)
	}
	src := copySourceOf(tbc.SourceOf)
	allWarns := append(append([]string{}, tbc.Warnings...), warns...)
	loaded := appendLoadedSkills(nil, tbc.LoadedSkills)

	inst, err := newChatModelAgent(ctx, coord.Name, coord.Description, a.composeInstructionWithScope(coord, sc), coord.MaxIteration, cm, tbc.Tools)
	if err != nil {
		return nil, err
	}
	subAgents := make([]adk.Agent, 0, len(subs))
	for _, s := range subs {
		sb, berr := a.buildOne(ctx, s, sc)
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
		loaded = appendLoadedSkills(loaded, sb.Meta.LoadedSkills)
	}
	root, err := adk.SetSubAgents(ctx, inst, subAgents)
	if err != nil {
		return nil, fmt.Errorf("set sub agents: %w", err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: root, EnableStreaming: true, CheckPointStore: a.CheckPoints})
	return &BuildResult{
		Runner: runner, AgentName: coord.Name,
		ModelLabel: label, ConnID: connID,
		SourceOf: src, Warnings: allWarns, LoadedSkills: loaded,
		OntoUnavailable: tbc.OntoIssue,
	}, nil
}

// buildOne 装配单个 Agent 实例：模型解析（显式>默认，M3）→ 工具合并（§6.8/§6.12/§6.11）→ ChatModelAgent。
func (a *Assembler) buildOne(ctx context.Context, ag *store.Agent, sc assembleScope) (*agentBuild, error) {
	cm, label, connID, err := a.buildModel(ctx, ag)
	if err != nil {
		return nil, err
	}
	tb, err := a.assembleTools(ctx, ag, sc)
	if err != nil {
		return nil, err
	}
	// M8 §6.10-3：guide 注入——装配时取运行方案指引拼进 Agent 指引（失败并入降级）
	instruction := a.composeInstructionWithScope(ag, sc)
	if sc.Mount != "" && a.Ontology != nil {
		guide, gerr := a.Ontology.FetchGuide(ctx, sc.Mount, "")
		if gerr != nil {
			if tb.OntoIssue == nil {
				tb.OntoIssue = &ontology.Issue{ProfileID: sc.Mount, Reason: gerr.Error()}
			}
		} else if guide != "" {
			instruction += "\n\n# 本体运行方案指引（runtime_profile: " + sc.Mount + "）\n" + guide
		}
	}
	inst, err := newChatModelAgent(ctx, ag.Name, ag.Description, instruction, ag.MaxIteration, cm, tb.Tools)
	if err != nil {
		return nil, err
	}
	return &agentBuild{
		Inst: inst,
		Meta: &agentMeta{
			ModelLabel:      label,
			ConnID:          connID,
			SourceOf:        tb.SourceOf,
			Warnings:        tb.Warnings,
			LoadedSkills:    tb.LoadedSkills,
			OntoUnavailable: tb.OntoIssue,
		},
	}, nil
}

// toolBundle 单 Agent 的工具装配产物。
type toolBundle struct {
	Tools        []einotool.BaseTool
	SourceOf     map[string]string
	Warnings     []string
	LoadedSkills []*store.Skill
	OntoIssue    *ontology.Issue // M8：本体 facade 不可达/方案停止（单次失败即降级，不重试风暴）
}

// assembleTools 工具合并管线（§6.8 M5 / §6.12 M9 技能 / §6.11 M9 MCP / §6.10 M8 本体）：
// 1) builtin 注册表按 agent.tools 勾选实例化；
// 2) 技能白名单（挂载且启用）并集补充，source=skill:{id}；
// 3) MCP servers 拉取远端工具，{server}__{tool} 前缀，source=mcp:{server}，失败降级告警；
// 4) 本体 facade（挂载运行方案时）onto_* 工具，source=ontology:facade，失败 ontology.unavailable 降级；
// function name 冲突先到先得 + 告警。
func (a *Assembler) assembleTools(ctx context.Context, ag *store.Agent, sc assembleScope) (*toolBundle, error) {
	tb := &toolBundle{Tools: []einotool.BaseTool{}, SourceOf: map[string]string{}, Warnings: []string{}}

	// 1) builtin 勾选
	composed, err := a.Tools.Compose(ctx, append([]string{}, ag.Tools...))
	if err != nil {
		return nil, fmt.Errorf("compose tools: %w", err)
	}
	tb.Tools = append(tb.Tools, composed.Tools...)
	for k, v := range composed.SourceOf {
		tb.SourceOf[k] = v
	}
	tb.Warnings = append(tb.Warnings, composed.Warnings...)

	// 2) 技能白名单（M9 挂载生效）
	if a.Composer != nil {
		tb.LoadedSkills = a.Composer.LoadedSkills(ag)
		for _, sk := range tb.LoadedSkills {
			for _, tid := range sk.Tools {
				e, ok := a.Tools.Get(tid)
				if !ok {
					tb.Warnings = append(tb.Warnings, fmt.Sprintf("技能 %q 引用未知工具 %q，已跳过", sk.Name, tid))
					continue
				}
				if _, dup := tb.SourceOf[e.Name]; dup {
					continue // agent.tools 已勾选或前序技能已装
				}
				bt, berr := e.New(ctx)
				if berr != nil {
					tb.Warnings = append(tb.Warnings, fmt.Sprintf("技能 %q 工具 %q 实例化失败: %v", sk.Name, tid, berr))
					continue
				}
				tb.Tools = append(tb.Tools, bt)
				tb.SourceOf[e.Name] = "skill:" + sk.ID
			}
		}
	}

	// 3) MCP servers（M9；连接失败降级继续，不阻断运行）
	for _, ms := range ag.MCPServers {
		if ms.URL == "" {
			continue
		}
		bts, ferr := tool.FetchMCPTools(ctx, ms.Name, ms.URL, mcpFetchTimeout)
		if ferr != nil {
			tb.Warnings = append(tb.Warnings, fmt.Sprintf("MCP %s(%s) 连接失败，本次运行不加载其工具: %v", ms.Name, ms.URL, ferr))
			continue
		}
		if len(bts) == 0 {
			tb.Warnings = append(tb.Warnings, fmt.Sprintf("MCP %s 未暴露任何工具", ms.Name))
			continue
		}
		for _, bt := range bts {
			ti, ierr := bt.Info(ctx)
			if ierr != nil || ti == nil || ti.Name == "" {
				continue
			}
			if _, dup := tb.SourceOf[ti.Name]; dup {
				tb.Warnings = append(tb.Warnings, fmt.Sprintf("MCP 工具 %q 与已有工具重名，已跳过", ti.Name))
				continue
			}
			tb.Tools = append(tb.Tools, bt)
			tb.SourceOf[ti.Name] = "mcp:" + ms.Name
		}
	}

	// 4) 本体 facade（M8 §6.10-1：挂载运行方案时并入 onto_* 工具；
	// 单次失败即降级——ontology.unavailable 事件，不重试风暴，普通对话/知识库不受影响）
	if sc.Mount != "" && a.Ontology != nil {
		bts, ferr := a.Ontology.FetchTools(ctx)
		if ferr != nil {
			tb.OntoIssue = &ontology.Issue{ProfileID: sc.Mount, Reason: ferr.Error()}
		} else {
			for _, bt := range bts {
				ti, ierr := bt.Info(ctx)
				if ierr != nil || ti == nil || ti.Name == "" {
					continue
				}
				if _, dup := tb.SourceOf[ti.Name]; dup {
					tb.Warnings = append(tb.Warnings, fmt.Sprintf("本体工具 %q 与已有工具重名，已跳过", ti.Name))
					continue
				}
				tb.Tools = append(tb.Tools, bt)
				tb.SourceOf[ti.Name] = "ontology:facade"
			}
		}
	}

	// 5) save_file 产物工具 + list_files/read_file 目录浏览读取（M11 §6.13 / REQ-102：
	// 项目会话且文件根目录已配置时启用；写盘成功由 runner 依 tool.result 发 artifact.saved 事件）
	if sc.ProjectID != "" && a.FilesRoot != "" && a.Store != nil {
		if sf, serr := tool.NewSaveFileTool(tool.SaveFileDeps{
			Store: a.Store, ProjectID: sc.ProjectID, ConversationID: sc.ConversationID, Root: a.FilesRoot,
		}); serr != nil {
			tb.Warnings = append(tb.Warnings, "save_file 工具实例化失败: "+serr.Error())
		} else if _, dup := tb.SourceOf["save_file"]; !dup {
			tb.Tools = append(tb.Tools, sf)
			tb.SourceOf["save_file"] = "builtin"
		}
		pd := tool.ProjectDirDeps{Store: a.Store, ProjectID: sc.ProjectID, FilesRoot: a.FilesRoot}
		if bt, berr := tool.NewListFilesTool(pd); berr != nil {
			tb.Warnings = append(tb.Warnings, "list_files 工具实例化失败: "+berr.Error())
		} else if _, dup := tb.SourceOf["list_files"]; !dup {
			tb.Tools = append(tb.Tools, bt)
			tb.SourceOf["list_files"] = "builtin"
		}
		if bt, berr := tool.NewReadFileTool(pd); berr != nil {
			tb.Warnings = append(tb.Warnings, "read_file 工具实例化失败: "+berr.Error())
		} else if _, dup := tb.SourceOf["read_file"]; !dup {
			tb.Tools = append(tb.Tools, bt)
			tb.SourceOf["read_file"] = "builtin"
		}
	}
	return tb, nil
}

// composeInstruction 技能注入后的最终系统提示词（§6.12；无 Composer/无技能时即原指令）。
func (a *Assembler) composeInstruction(ag *store.Agent) string {
	if a.Composer == nil {
		return ag.Instruction
	}
	return a.Composer.ComposeInstruction(ag)
}

// composeInstructionWithScope 技能注入 + 项目本地目录说明（REQ-101/102）。
// 项目会话且绑定 local_dir 时，告知模型可通过 list_files/read_file/save_file 访问目录内文件，
// 并给出绑定目录的绝对路径（与工具根解析同一归一化规则）。
func (a *Assembler) composeInstructionWithScope(ag *store.Agent, sc assembleScope) string {
	inst := a.composeInstruction(ag)
	if sc.ProjectID == "" || a.FilesRoot == "" || a.Store == nil {
		return inst
	}
	p, err := a.Store.GetProject(sc.ProjectID)
	if err != nil || p == nil || p.LocalDir == "" {
		return inst
	}
	dir := fsutil.NormalizeDir(p.LocalDir)
	if !filepath.IsAbs(dir) {
		return inst
	}
	return inst + "\n\n# 项目本地目录\n" +
		"本项目已绑定本地目录：" + dir + "\n" +
		"- list_files：列出目录（或子目录）下的文件与子目录；\n" +
		"- read_file：读取目录内文本文件内容（≤1MB）；\n" +
		"- save_file：把生成内容保存为目录内文件。\n" +
		"所有路径均为该目录下的相对路径，不要访问该目录之外的文件。"
}

// appendLoadedSkills 合并去重（多 Agent 协作时 skill.loaded 汇总）。
func appendLoadedSkills(dst, add []*store.Skill) []*store.Skill {
	seen := map[string]bool{}
	for _, sk := range dst {
		seen[sk.ID] = true
	}
	for _, sk := range add {
		if !seen[sk.ID] {
			seen[sk.ID] = true
			dst = append(dst, sk)
		}
	}
	return dst
}

// agentBuild 单个 Agent 装配产物（可独立运行，也可并入协作结构）。
type agentBuild struct {
	Inst adk.Agent
	Meta *agentMeta
}

// agentMeta 单 Agent 元信息。
type agentMeta struct {
	ModelLabel      string
	ConnID          string
	SourceOf        map[string]string
	Warnings        []string
	LoadedSkills    []*store.Skill
	OntoUnavailable *ontology.Issue // M8：本体挂载降级（首个 Agent 的失败即代表整体降级）
}

// newChatModelAgent 构造 ADK ChatModelAgent（统一 ToolsConfig / EmitInternalEvents）。
func newChatModelAgent(ctx context.Context, name, description, instruction string, maxIter int, cm *openai.ChatModel, tools []einotool.BaseTool) (adk.Agent, error) {
	inst, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          name,
		Description:   description,
		Instruction:   instruction, // M9：ComposeInstruction 技能注入后文本
		Model:         cm,
		MaxIterations: normalizeMaxIter(maxIter),
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
			// agent_as_tool / transfer 模式下内层 Agent 事件流出（subagent 事件时间线，M4）
			EmitInternalEvents: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create adk agent %q: %w", name, err)
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
