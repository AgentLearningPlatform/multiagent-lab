package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/inference"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/runtime"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

// Service 运行服务：装配、执行、停止。
type Service struct {
	Store     *store.Store
	Assembler *Assembler
	KB        *kb.Service         // M6：对话知识库召回（nil 时禁用）
	Runtime   runtime.Backend     // M10：沙箱执行后端（nil=inprocess；agent.RuntimeBackend=docker 时转发）
	Inference *inference.Registry // M13：推理后端注册表（nil=仅 eino-adk；外部 CLI 后端走 runExternal）

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // conversationID -> cancel
}

// NewService 构造。
func NewService(st *store.Store, asm *Assembler, kbSvc *kb.Service) *Service {
	return &Service{Store: st, Assembler: asm, KB: kbSvc, cancels: map[string]context.CancelFunc{}}
}

// EmitFn 平台 SSE 事件输出函数（由 API 层注入）。
type EmitFn func(ev *Event)

// Event 平台事件（方案 §7 统一协议）。
type Event struct {
	Type  string          `json:"type"`
	RunID string          `json:"run_id,omitempty"`
	Ts    string          `json:"ts"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// NewErrorEvent 构造 run.error 事件（供 API 层运行前错误直接输出）。
func NewErrorEvent(runID, code, msg string) *Event {
	return newEvent("run.error", runID, map[string]any{"code": code, "message": msg})
}

func newEvent(typ, runID string, data any) *Event {
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err == nil {
			raw = b
		}
	}
	return &Event{Type: typ, RunID: runID, Ts: time.Now().UTC().Format(time.RFC3339Nano), Data: raw}
}

// RunInput 用户输入。
type RunInput struct {
	Input        string `json:"input"`
	DebugLevel   int    `json:"debug_level"`   // REQ-117：观测级别 0 简洁 / 1 详细 / 2 调试
	DebugPersist bool   `json:"debug_persist"` // M17 阶段二：调试事件入库开关（model.step 等落 run_events）
	// Panes 对比模式窗格配置（REQ-19e/19f）：≥2 时一次提问 N 路并行；nil/1 = 单路（现状）
	Panes []PaneConfig `json:"panes,omitempty"`
}

// PaneConfig 对比窗格单项覆盖（REQ-19f/143/144）：智能体 / 模型连接 / 知识库 / 本体运行方案 /
// 推理参数（温度）/ 系统提示词临时改写 / 技能开关；空 = 继承对话当前配置。
// NoHistory = 窗格级「不携带历史」开关（干净对照，默认共享完整对话历史）。
type PaneConfig struct {
	AgentID          string   `json:"agent_id,omitempty"`
	ModelConnID      string   `json:"model_conn_id,omitempty"`
	KBID             string   `json:"kb_id,omitempty"`
	RuntimeProfileID string   `json:"runtime_profile_id,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`   // REQ-144：推理参数覆盖（作用于窗格 Agent 副本）
	Instruction      string   `json:"instruction,omitempty"`   // REQ-144：系统提示词临时改写（仅本窗格）
	EnableSkills     *bool    `json:"enable_skills,omitempty"` // REQ-144：技能开关（REQ-19g 口径，窗格级）
	NoHistory        bool     `json:"no_history,omitempty"`
}

// RunResult 运行结果摘要。
type RunResult struct {
	AssistantMessageID string `json:"assistant_message_id,omitempty"`
	Stopped            bool   `json:"stopped,omitempty"`
	Error              string `json:"error,omitempty"`
}

// Run 执行一次对话运行：持久化用户消息 → 装配（M4：单 Agent / 项目多 Agent）→ 流式执行 → 翻译事件 → 持久化。
func (s *Service) Run(ctx context.Context, conv *store.Conversation, agent *store.Agent, runID string, input string, debug int, debugPersist bool, emit EmitFn) (*RunResult, error) {
	if emit == nil {
		emit = func(*Event) {}
	}

	if conv.Scope == "agent" && conv.AgentID == nil {
		return nil, errors.New("conversation is not bound to an agent")
	}

	// REQ-19e/19f 对比模式由 API 层按 RunInput.Panes 分发到 RunCompare（SSE 契约与单路一致）

	// M10 §6.3：执行后端分发——docker 沙箱 → Start + /run SSE 透传；inprocess → 进程内装配执行
	if conv.Scope == "agent" && agent != nil && agent.RuntimeBackend == "docker" {
		if s.Runtime != nil {
			return s.runDocker(ctx, conv, agent, runID, input, emit)
		}
		emit(newEvent("run.warning", runID, map[string]any{"message": "agent 配置了 docker 执行后端但沙箱后端未启用，已回退 inprocess"}))
	}

	// M13/D-O13 §6.16：推理后端分发——外部 CLI 后端（非 eino-adk）走适配器路径（能力降级见 §6.16.4）
	if conv.Scope == "agent" && agent != nil && s.Inference != nil && s.Inference.IsExternal(agent.InferenceBackend) {
		return s.runExternal(ctx, conv, agent, runID, input, debug, emit)
	}

	// 可取消 ctx（单路注册；对比模式在 RunCompare 组级注册一处）
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

	return s.runOnce(runCtx, conv, agent, runID, input, debug, debugPersist, emit, runOpts{})
}

// runOpts 单路 inprocess 管线选项（REQ-19e/19f 对比窗格复用管线时的差异点）。
type runOpts struct {
	// HistMsgs 外部注入的历史快照（对比组级共享一次——先完成窗格的答案不进入其他窗格上下文）；
	// nil = 管线内自行加载。注入时按路复制，窗格各自追加召回 System 消息互不影响。
	HistMsgs []*schema.Message
	// SkipUserMessage 用户消息已由对比组级落库（共享提问仅存一条）
	SkipUserMessage bool
	// AssistantMeta 助手消息 meta JSON（对比窗格记录 pane 序号与单项覆盖，回放可溯源）
	AssistantMeta string
}

// runOnce 单路 inprocess 运行管线（Run 单路 / RunCompare 每窗格共用）：
// 装配 → 用户消息 → 历史组装 → run.started/技能/本体降级/召回 → 执行 → 助手消息 → 终态。
// 可取消 ctx 的注册由调用方负责（单路=Run；对比=RunCompare 组级一处，stop 整组终止）。
func (s *Service) runOnce(ctx context.Context, conv *store.Conversation, agent *store.Agent, runID string, input string, debug int, debugPersist bool, emit EmitFn, opts runOpts) (*RunResult, error) {
	// REQ-117/M17 调试模式：注入模型调用链路采集器（装饰器在模型调用期读取）
	if debug > 0 {
		rec := &debugRecorder{level: DebugLevel(debug), runID: runID, emit: emit}
		if debugPersist { // M17 阶段二：调试事件入库开关（model.step 同步落 run_events）
			rec.record = func(ev *Event) { s.emitAndRecord(ctx, conv, runID, ev, emit) }
		}
		ctx = withDebug(ctx, rec)
	}

	rt, err := s.Assembler.Assemble(ctx, agent, conv)
	if err != nil {
		return nil, err
	}

	res := &RunResult{}

	// 1) 持久化用户消息（多轮记忆来源；对比模式组级已落库，各窗格跳过）
	if !opts.SkipUserMessage {
		userMsg := &store.Message{ConversationID: conv.ID, Role: "user", Content: input}
		if _, err := s.Store.InsertMessage(userMsg); err != nil {
			return nil, fmt.Errorf("save user message: %w", err)
		}
	}

	// 2) 组装输入：历史（含刚落库的用户消息；对比窗格用组级共享快照的按路副本）
	var histMsgs []*schema.Message
	if opts.HistMsgs != nil {
		histMsgs = append([]*schema.Message(nil), opts.HistMsgs...)
	} else {
		history, err := s.Store.ListMessages(conv.ID)
		if err != nil {
			return nil, err
		}
		histMsgs = BuildHistoryMessages(history)
	}

	runCtx := ctx

	// run.started（M4：backend 标注 + 装配期告警）
	start := time.Now()
	startData := map[string]any{
		"conversation_id": conv.ID,
		"agent_name":      rt.AgentName,
		"agent_id":        agentIDOf(agent),
		"model":           rt.ModelLabel,
		"backend":         "inprocess", // M10 接入执行后端后按实际后端标注
	}
	if len(rt.Warnings) > 0 {
		startData["warnings"] = rt.Warnings
	}
	// REQ-117：装配快照（详细档=结构；调试档含最终指令全文）
	if debug >= 1 && rt.Snapshot != nil {
		startData["assembly"] = rt.Snapshot
		if debug < 2 {
			startData["assembly"] = snapshotWithoutInstructions(rt.Snapshot)
		}
	}
	s.emitAndRecord(runCtx, conv, runID, newEvent("run.started", runID, startData), emit)

	// skill.loaded（§6.12：本次运行实际生效的技能，M9 挂载生效）
	if len(rt.LoadedSkills) > 0 {
		skills := make([]map[string]string, 0, len(rt.LoadedSkills))
		for _, sk := range rt.LoadedSkills {
			skills = append(skills, map[string]string{"id": sk.ID, "name": sk.Name})
		}
		s.emitAndRecord(runCtx, conv, runID, newEvent("skill.loaded", runID, map[string]any{"skills": skills}), emit)
	}

	// 本体降级（M8 §6.10-4：挂载运行方案但 facade 不可达/方案停止 → 单次失败即降级，
	// ontology.unavailable 事件，不重试风暴，普通对话/知识库不受影响，恢复后下一条消息自动恢复）
	if rt.OntoUnavailable != nil {
		s.emitAndRecord(runCtx, conv, runID, newEvent("ontology.unavailable", runID, map[string]any{
			"profile_id": rt.OntoUnavailable.ProfileID,
			"reason":     rt.OntoUnavailable.Reason,
		}), emit)
	}

	// 知识库召回（M6，§11/§6.9：提问先检索 → retrieval 事件 → 上下文注入；失败降级不阻断）
	histMsgs = s.recallKB(runCtx, conv, runID, input, histMsgs, emit)

	// 中断恢复（M11 收尾）：新消息运行会放弃既有挂起中断（checkpoint 清理 + 状态清除）
	if conv.InterruptState != "" {
		s.abandonInterrupt(runCtx, conv, runID, emit)
	}

	rc := newRunConsumer(s, runCtx, conv, runID, rt, emit, start)
	// WithCheckPointID（M11 收尾）：中断时 ADK 自动存 checkpoint，供 Resume 定向恢复
	rc.consume(rt.Runner.Run(runCtx, histMsgs, adk.WithCheckPointID(checkPointIDOf(runID))))
	rc.subagentLeave(rc.lastAgent)

	// 4) 持久化助手消息（对比窗格：meta 记录 pane 序号与单项覆盖）
	if len(rc.buf) > 0 {
		asg := &store.Message{ConversationID: conv.ID, Role: "assistant", Content: string(rc.buf), Meta: opts.AssistantMeta}
		if m, err := s.Store.InsertMessage(asg); err == nil {
			res.AssistantMessageID = m.ID
		} else {
			log.Printf("[chat] save assistant message: %v", err)
		}
	}

	// 5) 终态事件（附带耗时 / token 用量 / finish_reason，供前端执行细节展示）
	finishData := func(reason string) map[string]any {
		data := map[string]any{"reason": reason, "elapsed_ms": time.Since(start).Milliseconds()}
		if rc.lastUsage != nil {
			data["usage"] = map[string]any{
				"prompt_tokens":     rc.lastUsage.PromptTokens,
				"completion_tokens": rc.lastUsage.CompletionTokens,
				"total_tokens":      rc.lastUsage.TotalTokens,
			}
		}
		if rc.lastFinish != "" {
			data["finish_reason"] = rc.lastFinish
		}
		return data
	}
	switch {
	case rc.stopped:
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.finished", runID, finishData("stopped")), emit)
		res.Stopped = true
	case rc.runErr != "":
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.error", runID, map[string]any{
			"code": "run_failed", "message": rc.runErr, "elapsed_ms": time.Since(start).Milliseconds(),
		}), emit)
		res.Error = rc.runErr
	case rc.interrupted:
		// M11 收尾：挂起等待答复（区别于 completed / stopped）
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.finished", runID, finishData("interrupted")), emit)
	default:
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.finished", runID, finishData("completed")), emit)
	}
	return res, nil
}

// RunCompare 对比模式运行（REQ-19e/19f）：一次提问 → N 路（2~4 窗格）并行，事件经同一 SSE 流
// 以窗格 run_id 区分（契约与单路一致）。语义：
//  1. 用户消息组级仅落一条（共享提问）；
//  2. 历史快照组级取一次再按路复制——先完成窗格的答案不进入其他窗格上下文（对照实验隔离）；
//  3. 每窗格独立 runID 独立装配（单项覆盖见 PaneConfig，未设置项继承对话/智能体当前配置）；
//  4. 停止为整组（cancels[convID] 组级一处注册，stop 终止全部 N 路，已生成内容保留落库）；
//  5. 助手消息按窗格落库，meta 记录 pane/覆盖配置（回放可溯源）。
//
// 边界：docker 沙箱与外部 CLI 推理后端暂不支持对比（单项覆盖在这两类后端不可保证），整组报错不运行；
// 窗格内 ask_human/审批中断仍走会话级单槽 interrupt_state（多窗格同时中断时以最后写入为准，学习尺度可接受）。
func (s *Service) RunCompare(ctx context.Context, conv *store.Conversation, agent *store.Agent, groupRunID string, paneRunIDs []string, in RunInput, emit EmitFn) (*RunResult, error) {
	if emit == nil {
		emit = func(*Event) {}
	}
	groupErr := func(code, msg string) (*RunResult, error) {
		emit(newEvent("run.error", groupRunID, map[string]any{"code": code, "message": msg}))
		return nil, errors.New(msg)
	}
	if np := len(in.Panes); np < 2 || np > 4 || np != len(paneRunIDs) {
		return groupErr("compare_invalid_panes", fmt.Sprintf("对比窗格数须为 2~4 且与 run_id 数对齐（got %d/%d）", len(in.Panes), len(paneRunIDs)))
	}
	if agent == nil {
		return groupErr("agent_missing", "conversation has no agent to run")
	}
	// REQ-143：后端能力守卫从组级下沉为窗格级——各窗格按所选 Agent 独立判定（继承默认 Agent 的窗格
	// 若为 docker/外部 CLI 后端同样报错该窗格），其余窗格不受阻

	// 挂起中断组级统一放弃一次（窗格内 conv 副本已清空 InterruptState，不再重复告警/清库）
	if conv.InterruptState != "" {
		s.abandonInterrupt(ctx, conv, groupRunID, emit)
		conv.InterruptState = ""
	}

	// 1) 共享用户消息（仅一条）
	userMsg := &store.Message{ConversationID: conv.ID, Role: "user", Content: in.Input}
	if _, err := s.Store.InsertMessage(userMsg); err != nil {
		return nil, fmt.Errorf("save user message: %w", err)
	}

	// 2) 组级历史快照（含刚落库的提问）；NoHistory 窗格用「仅本轮提问」快照（REQ-143③ 干净对照）
	history, err := s.Store.ListMessages(conv.ID)
	if err != nil {
		return nil, err
	}
	histMsgs := BuildHistoryMessages(history)
	noHistMsgs := BuildHistoryMessages([]*store.Message{userMsg})

	// 3) 组级取消注册（stop 整组终止）
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

	// 4) 每窗格独立管线（REQ-143：按窗格解析 Agent 并独立装配；失败各路独立报错不互相阻断）
	var wg sync.WaitGroup
	for i := range in.Panes {
		pc, runID := in.Panes[i], paneRunIDs[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			pa, err := s.resolvePaneAgent(agent, pc)
			if err != nil {
				emit(newEvent("run.error", runID, map[string]any{"code": "compare_backend_unsupported", "message": fmt.Sprintf("窗格 %d：%v", i+1, err)}))
				return
			}
			hm := histMsgs
			if pc.NoHistory {
				hm = noHistMsgs
			}
			_, _ = s.runOnce(runCtx, paneConversation(conv, pc), paneAgent(pa, pc), runID,
				in.Input, in.DebugLevel, in.DebugPersist, emit,
				runOpts{HistMsgs: hm, SkipUserMessage: true, AssistantMeta: paneMetaJSON(i, runID, pa, pc)})
		}()
	}
	wg.Wait()
	return &RunResult{}, nil
}

// resolvePaneAgent 解析窗格 Agent（REQ-143②）：窗格指定 agent_id → 加载该 Agent（完整配置独立装配）；
// 空 → 继承对话当前 Agent。docker 沙箱 / 外部 CLI 推理后端的窗格明确报错（对比窗格的单项覆盖语义
// 在这两类后端不可保证），错误为窗格级不阻断整组。
func (s *Service) resolvePaneAgent(def *store.Agent, pc PaneConfig) (*store.Agent, error) {
	ag := def
	if pc.AgentID != "" {
		if pc.AgentID != def.ID {
			loaded, err := s.Store.GetAgent(pc.AgentID)
			if err != nil {
				return nil, fmt.Errorf("所选智能体不存在或已删除（%s）", pc.AgentID)
			}
			ag = loaded
		}
	}
	if ag.RuntimeBackend == "docker" {
		return nil, fmt.Errorf("智能体 %q 为 docker 沙箱执行后端，不支持对比窗格", ag.Name)
	}
	if s.Inference != nil && s.Inference.IsExternal(ag.InferenceBackend) {
		return nil, fmt.Errorf("智能体 %q 为外部 CLI 推理后端（%s），不支持对比窗格", ag.Name, ag.InferenceBackend)
	}
	return ag, nil
}

// paneConversation 应用窗格知识库/本体运行方案/技能开关覆盖（REQ-143/144；
// 空 = 继承对话配置，含继承「未开启」状态）。
func paneConversation(conv *store.Conversation, pc PaneConfig) *store.Conversation {
	if pc.KBID == "" && pc.RuntimeProfileID == "" && pc.EnableSkills == nil {
		return conv
	}
	cp := *conv
	cp.InterruptState = "" // 组级已处理，窗格内不再触发
	if pc.EnableSkills != nil {
		v := *pc.EnableSkills
		cp.EnableSkills = &v
	}
	if pc.KBID != "" {
		id := pc.KBID
		cp.KBID = &id
		cp.EnableKB = true
	}
	if pc.RuntimeProfileID != "" {
		id := pc.RuntimeProfileID
		cp.RuntimeProfileID = &id
		cp.OntologyEnabled = true
	}
	return &cp
}

// paneAgent 应用窗格智能体覆盖（REQ-143/144）：模型连接 / 推理参数（温度）/ 系统提示词临时改写；
// 任一字段覆盖即克隆 Agent 副本，不污染原对象。无效连接由装配层报错，与单路一致。
func paneAgent(ag *store.Agent, pc PaneConfig) *store.Agent {
	if ag == nil || (pc.ModelConnID == "" && pc.Temperature == nil && pc.Instruction == "") {
		return ag
	}
	cp := *ag
	if pc.ModelConnID != "" {
		id := pc.ModelConnID
		cp.ModelConnID = &id
	}
	if pc.Temperature != nil {
		t := *pc.Temperature
		cp.Temperature = &t
	}
	if pc.Instruction != "" {
		cp.Instruction = pc.Instruction
	}
	return &cp
}

// paneMetaJSON 窗格助手消息 meta（有效配置快照：pane 序号 + run_id + Agent 引用 + 单项覆盖 + 历史口径）。
func agentIDOf(ag *store.Agent) string {
	if ag == nil {
		return ""
	}
	return ag.ID
}

func paneMetaJSON(idx int, runID string, ag *store.Agent, pc PaneConfig) string {
	ov := map[string]any{
		"model_conn_id": pc.ModelConnID, "kb_id": pc.KBID, "runtime_profile_id": pc.RuntimeProfileID,
	}
	if pc.Temperature != nil {
		ov["temperature"] = *pc.Temperature
	}
	if pc.Instruction != "" {
		ov["instruction"] = pc.Instruction
	}
	if pc.EnableSkills != nil {
		ov["enable_skills"] = *pc.EnableSkills
	}
	b, err := json.Marshal(map[string]any{
		"compare": true, "pane": idx, "run_id": runID,
		"agent_id": agentIDOf(ag), "no_history": pc.NoHistory,
		"overrides": ov,
	})
	if err != nil {
		return ""
	}
	return string(b)
}

// checkPointIDOf 运行 → 中断检查点 ID（Run 传入 WithCheckPointID 与中断落库共用）。
func checkPointIDOf(runID string) string { return "ckpt_" + runID }

// interruptState 会话中断挂起信息（conversation.interrupt_state JSON）。
// TargetID 为中断点地址串（InterruptCtx.ID），恢复时作为 ResumeParams.Targets 的键定向投递答复。
// Kind：ask_human（自由答复）/ approval（工具审批，恢复数据 approve|deny）。
type interruptState struct {
	Kind         string   `json:"kind"`
	CheckpointID string   `json:"checkpoint_id"`
	TargetID     string   `json:"target_id"`
	Question     string   `json:"question"`
	Choices      []string `json:"choices,omitempty"`
	ToolName     string   `json:"tool_name,omitempty"`
	Arguments    string   `json:"arguments,omitempty"`
	RunID        string   `json:"run_id"`
}

// handleInterrupted 捕获运行中断（ask_human / 工具审批）：提取根因中断点信息，
// 挂起状态落 conversation.interrupt_state，发 run.interrupted 事件（前端渲染答复/审批卡）。
func (s *Service) handleInterrupted(ctx context.Context, conv *store.Conversation, runID string, ii *adk.InterruptInfo, emit EmitFn) {
	st := interruptState{CheckpointID: checkPointIDOf(runID), RunID: runID}
	if ii != nil {
		var chosen *adk.InterruptCtx
		for _, c := range ii.InterruptContexts {
			if c == nil {
				continue
			}
			if chosen == nil || (c.IsRootCause && !chosen.IsRootCause) {
				chosen = c
			}
			if c.IsRootCause {
				break
			}
		}
		if chosen != nil {
			st.TargetID = chosen.ID
			if b, err := json.Marshal(chosen.Info); err == nil {
				var hi struct {
					Question  string   `json:"question"`
					Choices   []string `json:"choices"`
					ToolName  string   `json:"tool_name"`
					Arguments string   `json:"arguments"`
				}
				if json.Unmarshal(b, &hi) == nil {
					switch {
					case hi.Question != "":
						st.Kind, st.Question, st.Choices = "ask_human", hi.Question, hi.Choices
					case hi.ToolName != "":
						st.Kind, st.ToolName, st.Arguments = "approval", hi.ToolName, hi.Arguments
					}
				}
			}
		}
	}
	if st.Kind == "" {
		st.Kind = "ask_human" // 未知中断信息形态的兜底（答复卡至少可展示并定向续跑）
	}
	if b, err := json.Marshal(st); err == nil {
		_ = s.Store.SetConversationInterruptState(conv.ID, string(b))
	}
	data := map[string]any{
		"kind": st.Kind, "checkpoint_id": st.CheckpointID, "target_id": st.TargetID,
		"question": st.Question, "choices": st.Choices,
		"tool_name": st.ToolName, "arguments": st.Arguments,
	}
	s.emitAndRecord(ctx, conv, runID, newEvent("run.interrupted", runID, data), emit)
}

// abandonInterrupt 新消息运行时放弃挂起中断（清 checkpoint + 挂起状态，发警告事件）。
func (s *Service) abandonInterrupt(ctx context.Context, conv *store.Conversation, runID string, emit EmitFn) {
	var st interruptState
	if json.Unmarshal([]byte(conv.InterruptState), &st) == nil && st.CheckpointID != "" && s.Assembler.CheckPoints != nil {
		_ = s.Assembler.CheckPoints.Delete(ctx, st.CheckpointID)
	}
	_ = s.Store.SetConversationInterruptState(conv.ID, "")
	s.emitAndRecord(ctx, conv, runID, newEvent("run.warning", runID, map[string]any{
		"message": "已有挂起的提问未答复，本次新消息按新问题运行（原提问已放弃）",
	}), emit)
}

// runConsumer 单次运行的事件消费状态（Run 与 Resume 共用同一事件翻译管线）。
type runConsumer struct {
	s     *Service
	ctx   context.Context
	conv  *store.Conversation
	runID string
	rt    *BuildResult
	emit  EmitFn
	start time.Time

	rootAgent     string
	subagentLeave func(string)
	trackAgent    func(string)
	emitReasoning func(string)
	recordDelta   func(string)

	buf         []byte
	stopped     bool
	runErr      string
	interrupted bool
	lastUsage   *schema.TokenUsage
	lastFinish  string
	toolAgg     map[int]*pendingToolCall
	toolCallAt  map[string]time.Time // REQ-117：tool.call 发出时刻 → tool.result 计算执行耗时
	lastAgent   string
}

// newRunConsumer 构造事件消费者（Run 与 Resume 共用；闭包绑定自身状态）。
func newRunConsumer(s *Service, ctx context.Context, conv *store.Conversation, runID string, rt *BuildResult, emit EmitFn, start time.Time) *runConsumer {
	rc := &runConsumer{
		s: s, ctx: ctx, conv: conv, runID: runID, rt: rt, emit: emit, start: start,
		rootAgent:  rt.AgentName,
		toolAgg:    map[int]*pendingToolCall{},
		toolCallAt: map[string]time.Time{},
	}
	rc.subagentLeave = func(name string) {
		if name != "" && name != rc.rootAgent {
			s.emitAndRecord(rc.ctx, conv, runID, newEvent("subagent.exit", runID, map[string]any{"agent": name}), emit)
		}
	}
	rc.trackAgent = func(name string) {
		if name == "" || name == rc.lastAgent {
			return
		}
		if rc.lastAgent != "" {
			rc.subagentLeave(rc.lastAgent)
		}
		if name != rc.rootAgent {
			s.emitAndRecord(rc.ctx, conv, runID, newEvent("subagent.enter", runID, map[string]any{"agent": name}), emit)
		}
		rc.lastAgent = name
	}
	rc.emitReasoning = func(delta string) {
		if delta != "" {
			s.emitAndRecord(rc.ctx, conv, runID, newEvent("reasoning.delta", runID, map[string]any{"delta": delta}), emit)
		}
	}
	rc.recordDelta = func(delta string) {
		rc.buf = append(rc.buf, delta...)
		if len(delta) > 0 {
			emit(newEvent("message.delta", runID, map[string]any{"delta": delta}))
		}
	}
	return rc
}

// consume 消费 ADK 事件流并翻译为平台事件（方案 §7）；中断事件在此捕获。
func (rc *runConsumer) consume(iter *adk.AsyncIterator[*adk.AgentEvent]) {
	s, runCtx, conv, runID, rt, emit := rc.s, rc.ctx, rc.conv, rc.runID, rc.rt, rc.emit
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			if errors.Is(ev.Err, context.Canceled) {
				rc.stopped = true
			} else {
				rc.runErr = ev.Err.Error()
			}
			break
		}
		rc.trackAgent(ev.AgentName)
		// 中断恢复（M11 收尾）：挂起运行等待用户答复（Output 通常为空，须在 Output 判空前处理）
		if ev.Action != nil && ev.Action.Interrupted != nil {
			rc.interrupted = true
			s.handleInterrupted(runCtx, conv, runID, ev.Action.Interrupted, emit)
			continue
		}
		// transfer 模式：动作级转移提示
		if ev.Action != nil && ev.Action.TransferToAgent != nil {
			dest := ev.Action.TransferToAgent.DestAgentName
			if dest != "" {
				s.emitAndRecord(runCtx, conv, runID, newEvent("subagent.enter", runID, map[string]any{"agent": dest, "via": "transfer"}), emit)
			}
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mo := ev.Output.MessageOutput
		switch {
		case mo.IsStreaming && mo.MessageStream != nil:
			if mo.Role == schema.User {
				continue
			}
			for {
				chunk, err := mo.MessageStream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					if errors.Is(err, context.Canceled) {
						rc.stopped = true
					} else {
						rc.runErr = err.Error()
					}
					break
				}
				if chunk == nil {
					continue
				}
				if chunk.ReasoningContent != "" {
					rc.emitReasoning(chunk.ReasoningContent)
				}
				if chunk.Content != "" {
					rc.recordDelta(chunk.Content)
				}
				mergeToolCallChunk(rc.toolAgg, chunk.ToolCalls)
				if chunk.ResponseMeta != nil {
					if chunk.ResponseMeta.Usage != nil {
						rc.lastUsage = chunk.ResponseMeta.Usage
					}
					if chunk.ResponseMeta.FinishReason != "" {
						rc.lastFinish = chunk.ResponseMeta.FinishReason
					}
				}
			}
			// 流结束：输出聚合完成的 tool.call（含入参 JSON 与 source）
			flushToolCalls(s, rc, conv, runID, rt, emit)
			for k := range rc.toolAgg {
				delete(rc.toolAgg, k)
			}
		case mo.Message != nil && mo.Role == schema.Tool:
			// 工具执行结果（对应发起见 tool.call 事件）
			data := map[string]any{"tool_name": mo.Message.ToolName, "content": mo.Message.Content}
			if mo.Message.ToolCallID != "" {
				data["tool_call_id"] = mo.Message.ToolCallID
				// REQ-117：工具执行耗时（tool.call 发出 → tool.result 到达）
				if t0, ok := rc.toolCallAt[mo.Message.ToolCallID]; ok {
					data["duration_ms"] = time.Since(t0).Milliseconds()
					delete(rc.toolCallAt, mo.Message.ToolCallID)
				}
			}
			if src := rt.SourceOf[mo.Message.ToolName]; src != "" {
				data["source"] = src
			}
			// M11 §6.13：save_file 成功 → artifact.saved 事件（对话产物面板数据源）
			if mo.Message.ToolName == "save_file" {
				if fid, name, path, ok := tool.ParseSaveFileResult(mo.Message.Content); ok {
					s.emitAndRecord(runCtx, conv, runID, newEvent("artifact.saved", runID, map[string]any{
						"file_id": fid, "name": name, "path": path,
					}), emit)
				}
			}
			s.emitAndRecord(runCtx, conv, runID, newEvent("tool.result", runID, data), emit)
		case mo.Message != nil && mo.Role == schema.Assistant && !mo.IsStreaming:
			// 非流式完整输出（兜底）
			if mo.Message.ReasoningContent != "" {
				rc.emitReasoning(mo.Message.ReasoningContent)
			}
			if mo.Message.Content != "" {
				rc.recordDelta(mo.Message.Content)
			}
			for _, tc := range mo.Message.ToolCalls {
				emitToolCall(s, rc, conv, runID, rt, tc, emit)
			}
			if mo.Message.ResponseMeta != nil {
				if mo.Message.ResponseMeta.Usage != nil {
					rc.lastUsage = mo.Message.ResponseMeta.Usage
				}
				if mo.Message.ResponseMeta.FinishReason != "" {
					rc.lastFinish = mo.Message.ResponseMeta.FinishReason
				}
			}
		}
	}
}

// Resume 恢复挂起的中断（M11 收尾）：以用户答复按 InterruptCtx.ID 定向恢复 ask_human 中断点，
// 复用 Run 的事件翻译管线；恢复过程若再次中断（如连环提问），照常落新的挂起状态。
func (s *Service) Resume(ctx context.Context, conv *store.Conversation, agent *store.Agent, runID, answer string, debug int, debugPersist bool, emit EmitFn) (*RunResult, error) {
	if emit == nil {
		emit = func(*Event) {}
	}
	var st interruptState
	if conv.InterruptState == "" || json.Unmarshal([]byte(conv.InterruptState), &st) != nil || st.CheckpointID == "" || st.TargetID == "" {
		return nil, errors.New("该会话没有挂起的中断提问")
	}
	if debug > 0 {
		rec := &debugRecorder{level: DebugLevel(debug), runID: runID, emit: emit}
		if debugPersist { // M17 阶段二：调试事件入库开关（model.step 同步落 run_events）
			rec.record = func(ev *Event) { s.emitAndRecord(ctx, conv, runID, ev, emit) }
		}
		ctx = withDebug(ctx, rec)
	}
	rt, err := s.Assembler.Assemble(ctx, agent, conv)
	if err != nil {
		return nil, err
	}
	res := &RunResult{}
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

	start := time.Now()
	startData := map[string]any{
		"conversation_id": conv.ID, "agent_name": rt.AgentName, "model": rt.ModelLabel,
		"backend": "inprocess", "resumed": true,
	}
	if debug >= 1 && rt.Snapshot != nil {
		startData["assembly"] = rt.Snapshot
		if debug < 2 {
			startData["assembly"] = snapshotWithoutInstructions(rt.Snapshot)
		}
	}
	s.emitAndRecord(runCtx, conv, runID, newEvent("run.started", runID, startData), emit)
	// 先清挂起状态：恢复过程中若再次中断，handleInterrupted 会写入新状态
	_ = s.Store.SetConversationInterruptState(conv.ID, "")

	iter, err := rt.Runner.ResumeWithParams(runCtx, st.CheckpointID, &adk.ResumeParams{
		Targets: map[string]any{st.TargetID: answer},
	})
	if err != nil {
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.error", runID, map[string]any{
			"code": "resume_failed", "message": err.Error(), "elapsed_ms": time.Since(start).Milliseconds(),
		}), emit)
		return nil, err
	}

	rc := newRunConsumer(s, runCtx, conv, runID, rt, emit, start)
	rc.consume(iter)
	rc.subagentLeave(rc.lastAgent)

	if len(rc.buf) > 0 {
		asg := &store.Message{ConversationID: conv.ID, Role: "assistant", Content: string(rc.buf)}
		if m, err := s.Store.InsertMessage(asg); err == nil {
			res.AssistantMessageID = m.ID
		} else {
			log.Printf("[chat] save assistant message: %v", err)
		}
	}
	finishData := func(reason string) map[string]any {
		data := map[string]any{"reason": reason, "elapsed_ms": time.Since(start).Milliseconds()}
		if rc.lastUsage != nil {
			data["usage"] = map[string]any{
				"prompt_tokens":     rc.lastUsage.PromptTokens,
				"completion_tokens": rc.lastUsage.CompletionTokens,
				"total_tokens":      rc.lastUsage.TotalTokens,
			}
		}
		return data
	}
	switch {
	case rc.stopped:
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.finished", runID, finishData("stopped")), emit)
		res.Stopped = true
	case rc.runErr != "":
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.error", runID, map[string]any{
			"code": "run_failed", "message": rc.runErr, "elapsed_ms": time.Since(start).Milliseconds(),
		}), emit)
		res.Error = rc.runErr
	case rc.interrupted:
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.finished", runID, finishData("interrupted")), emit)
	default:
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.finished", runID, finishData("completed")), emit)
	}
	return res, nil
}

// pendingToolCall 流式 tool_calls 增量聚合（Index -> 名称/参数累积）。
type pendingToolCall struct {
	ID   string
	Name string
	Args strings.Builder
}

// mergeToolCallChunk 合并流式 tool_calls 增量分片。
func mergeToolCallChunk(agg map[int]*pendingToolCall, tcs []schema.ToolCall) {
	for _, tc := range tcs {
		idx := 0
		if tc.Index != nil {
			idx = *tc.Index
		}
		p := agg[idx]
		if p == nil {
			p = &pendingToolCall{}
			agg[idx] = p
		}
		if tc.ID != "" && p.ID == "" {
			p.ID = tc.ID
		}
		if tc.Function.Name != "" {
			p.Name = tc.Function.Name // 名称一次性到达
		}
		p.Args.WriteString(tc.Function.Arguments) // 参数为增量分片
	}
}

// flushToolCalls 输出聚合完成的 tool.call 事件（含入参 JSON 与 source）。
func flushToolCalls(s *Service, rc *runConsumer, conv *store.Conversation, runID string, rt *BuildResult, emit EmitFn) {
	for _, p := range rc.toolAgg {
		if p.Name == "" {
			continue
		}
		emitToolCall(s, rc, conv, runID, rt, schema.ToolCall{
			ID: p.ID, Function: schema.FunctionCall{Name: p.Name, Arguments: p.Args.String()},
		}, emit)
	}
}

// emitToolCall 输出模型发起的工具调用事件（含入参 JSON 字符串与 source 标注）。
func emitToolCall(s *Service, rc *runConsumer, conv *store.Conversation, runID string, rt *BuildResult, tc schema.ToolCall, emit EmitFn) {
	if rc != nil && tc.ID != "" && rc.toolCallAt != nil {
		rc.toolCallAt[tc.ID] = time.Now()
	}
	data := map[string]any{"tool_name": tc.Function.Name, "arguments": tc.Function.Arguments}
	if tc.ID != "" {
		data["tool_call_id"] = tc.ID
	}
	if rt != nil {
		if src := rt.SourceOf[tc.Function.Name]; src != "" {
			data["source"] = src
			// M8 §11：本体查询事件语义——onto_* 工具（source=ontology:facade）附加 profile/via
			if src == "ontology:facade" && conv != nil && conv.RuntimeProfileID != nil {
				data["profile_id"] = *conv.RuntimeProfileID
				data["via"] = "mcp"
			}
		}
	}
	ctx := context.Background()
	if rc != nil {
		ctx = rc.ctx
	}
	s.emitAndRecord(ctx, conv, runID, newEvent("tool.call", runID, data), emit)
}

// snapshotWithoutInstructions 装配快照脱敏副本：详细档（<2）不含最终指令全文。
func snapshotWithoutInstructions(snap map[string]any) map[string]any {
	out := map[string]any{"mode": snap["mode"]}
	if agents, ok := snap["agents"].([]map[string]any); ok {
		outAgents := make([]map[string]any, 0, len(agents))
		for _, a := range agents {
			cp := map[string]any{}
			for k, v := range a {
				if k == "instruction" {
					continue
				}
				cp[k] = v
			}
			outAgents = append(outAgents, cp)
		}
		out["agents"] = outAgents
	}
	return out
}

// Stop 停止对话正在进行的运行。
func (s *Service) Stop(conversationID string) bool {
	s.mu.Lock()
	cancel, ok := s.cancels[conversationID]
	s.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// emitAndRecord 发送事件并落 run_event 表（时间线可回放，验收5）。
// agentdRunRequest 沙箱 /run 请求体（与 cmd/agentd 对齐）。
type agentdRunRequest struct {
	Input            string          `json:"input"`
	RunID            string          `json:"run_id"`
	DebugLevel       int             `json:"debug_level,omitempty"` // M17 阶段二：调试档经沙箱请求透传
	DebugPersist     bool            `json:"debug_persist,omitempty"`
	History          []store.Message `json:"history,omitempty"` // 不含最后一条 user（沙箱 Run 会存 input）
	RuntimeProfileID *string         `json:"runtime_profile_id,omitempty"`
	OntologyEnabled  bool            `json:"ontology_enabled,omitempty"`
}

// runDocker docker 沙箱后端执行（§6.3，M10）：用户消息主平台落库 → 配置经启动时下发、
// history 随请求下发 → agentd 容器内同一装配代码运行 → SSE 事件透传并记录 →
// assistant 文本聚合落主平台库。对话历史权威数据在主平台（容器可随时重建）。
func (s *Service) runDocker(ctx context.Context, conv *store.Conversation, agent *store.Agent, runID string, input string, emit EmitFn) (*RunResult, error) {
	res := &RunResult{}
	start := time.Now()
	if emit == nil {
		emit = func(*Event) {}
	}

	// 1) 用户消息主平台落库（历史权威在主平台）
	userMsg := &store.Message{ConversationID: conv.ID, Role: "user", Content: input}
	if _, err := s.Store.InsertMessage(userMsg); err != nil {
		return nil, fmt.Errorf("save user message: %w", err)
	}
	history, err := s.Store.ListMessages(conv.ID)
	if err != nil {
		return nil, err
	}
	// 去掉最后一条 user（agentd 端 Run 内部会存 input，避免重复）
	if len(history) > 0 && history[len(history)-1].Role == "user" {
		history = history[:len(history)-1]
	}
	histVals := make([]store.Message, len(history))
	for i, m := range history {
		histVals[i] = *m
	}

	// 2) 确保沙箱实例就绪
	ep, err := s.Runtime.Start(ctx, agent.ID)
	if err != nil {
		s.emitAndRecord(ctx, conv, runID, newEvent("run.error", runID, map[string]any{
			"code": "sandbox_start_failed", "message": err.Error(), "elapsed_ms": time.Since(start).Milliseconds(),
		}), emit)
		res.Error = err.Error()
		return res, nil
	}

	// 3) POST {endpoint}/run 并透传 SSE
	body, _ := json.Marshal(agentdRunRequest{
		Input: input, RunID: runID, History: histVals,
		RuntimeProfileID: conv.RuntimeProfileID, OntologyEnabled: conv.OntologyEnabled,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL+"/run", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		s.emitAndRecord(ctx, conv, runID, newEvent("run.error", runID, map[string]any{
			"code": "sandbox_unreachable", "message": err.Error(), "elapsed_ms": time.Since(start).Milliseconds(),
		}), emit)
		res.Error = err.Error()
		return res, nil
	}
	defer resp.Body.Close()

	var buf strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	curEvent := ""
	rewrite := func(raw json.RawMessage) json.RawMessage {
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			return raw
		}
		m["conversation_id"] = conv.ID // 事件流会话 ID 重写为主平台会话
		if curEvent == "run.started" {
			m["backend"] = "docker"
		}
		b, err := json.Marshal(m)
		if err != nil {
			return raw
		}
		return b
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			curEvent = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
		case strings.HasPrefix(line, "data: "):
			raw := json.RawMessage(strings.TrimSpace(strings.TrimPrefix(line, "data: ")))
			ev := &Event{Type: curEvent, RunID: runID, Ts: time.Now().UTC().Format(time.RFC3339Nano), Data: rewrite(raw)}
			// assistant 文本聚合（message.delta 与主平台 inprocess 语义一致）
			if curEvent == "message.delta" {
				var d struct {
					Delta string `json:"delta"`
				}
				if json.Unmarshal(raw, &d) == nil {
					buf.WriteString(d.Delta)
				}
			}
			if curEvent == "run.error" {
				var d struct {
					Message string `json:"message"`
				}
				if json.Unmarshal(raw, &d) == nil && d.Message != "" {
					res.Error = d.Message
				}
			}
			s.emitAndRecord(ctx, conv, runID, ev, emit)
			curEvent = ""
		}
	}

	// 4) assistant 消息聚合落库（无错误且有输出时）
	if res.Error == "" && buf.Len() > 0 {
		asg := &store.Message{ConversationID: conv.ID, Role: "assistant", Content: buf.String()}
		if m, err := s.Store.InsertMessage(asg); err == nil {
			res.AssistantMessageID = m.ID
		}
	}
	return res, nil
}

func (s *Service) emitAndRecord(_ context.Context, conv *store.Conversation, runID string, ev *Event, emit EmitFn) {
	emit(ev)
	re := &store.RunEvent{ConversationID: conv.ID, RunID: runID, Type: ev.Type, Data: string(ev.Data)}
	if _, err := s.Store.InsertEvent(re); err != nil {
		log.Printf("[chat] record run event: %v", err)
	}
}

// AssembleSummary 供 API 返回 agent 概要信息（模型标签）。
func (s *Service) AssembleSummary(ctx context.Context, agent *store.Agent) (string, error) {
	rt, err := s.Assembler.Assemble(ctx, agent, &store.Conversation{})
	if err != nil {
		return "", err
	}
	return rt.ModelLabel, nil
}

// recallKB 知识库召回公共段（M13 抽取：inprocess 与外部推理后端两条路径共用）。
// 检索 → retrieval 事件 → 命中内容作为 System 消息追加到 histMsgs；任何失败降级不阻断。
func (s *Service) recallKB(ctx context.Context, conv *store.Conversation, runID, input string, histMsgs []*schema.Message, emit EmitFn) []*schema.Message {
	if !(conv.EnableKB && conv.KBID != nil && *conv.KBID != "" && s.KB != nil) {
		return histMsgs
	}
	kbcfg, kerr := s.Store.GetKnowledgeBase(*conv.KBID)
	if kerr != nil {
		s.emitAndRecord(ctx, conv, runID, newEvent("run.warning", runID, map[string]any{"message": "知识库加载失败，本次回答未注入知识库内容: " + kerr.Error()}), emit)
		return histMsgs
	}
	// M14 D-KB4：graphrag 模式 KB 走 KG 扩展检索；KG 无命中降级向量检索（不阻断，事件标注 degraded）。
	// M16/REQ-128：detail 版返回命中路径上的实体/关系/claims 明细，随 retrieval 事件外显（过程可观测）。
	detail, mode, degraded, serr := s.KB.GraphragQueryWithFallbackDetail(ctx, kbcfg, input, conv.TopK, conv.MinScore, kb.GraphragOpts{})
	hits := []kb.RetrievalHit{}
	if detail != nil {
		hits = detail.Hits
	}
	switch {
	case serr != nil:
		s.emitAndRecord(ctx, conv, runID, newEvent("run.warning", runID, map[string]any{"message": "知识库检索失败，本次回答未注入知识库内容: " + serr.Error()}), emit)
	case len(hits) > 0:
		hd := make([]map[string]any, 0, len(hits))
		for _, h := range hits {
			hd = append(hd, map[string]any{"doc": h.Doc, "seq": h.Seq, "score": h.Score, "excerpt": h.Excerpt})
		}
		data := map[string]any{"kb_id": kbcfg.ID, "mode": mode, "hits": hd} // M14 ④：retrieval 事件带 mode
		if degraded {
			data["degraded"] = true
		}
		if mode == "graphrag" && detail != nil { // M16 ②：实体/关系明细（教学：KG 扩展路径可见）
			data["entities"] = detail.Entities
			data["relationships"] = detail.Relationships
			data["claims"] = detail.Claims
		}
		s.emitAndRecord(ctx, conv, runID, newEvent("retrieval", runID, data), emit)
		if ctxText := kb.RenderContext(kbcfg.Name, hits); ctxText != "" {
			histMsgs = append(histMsgs, schema.SystemMessage(ctxText))
		}
	}
	return histMsgs
}
