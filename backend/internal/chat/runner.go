package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// Service 运行服务：装配、执行、停止。
type Service struct {
	Store     *store.Store
	Assembler *Assembler
	KB        *kb.Service // M6：对话知识库召回（nil 时禁用）

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
	Input string `json:"input"`
}

// RunResult 运行结果摘要。
type RunResult struct {
	AssistantMessageID string `json:"assistant_message_id,omitempty"`
	Stopped            bool   `json:"stopped,omitempty"`
	Error              string `json:"error,omitempty"`
}

// Run 执行一次对话运行：持久化用户消息 → 装配（M4：单 Agent / 项目多 Agent）→ 流式执行 → 翻译事件 → 持久化。
func (s *Service) Run(ctx context.Context, conv *store.Conversation, agent *store.Agent, runID string, input string, emit EmitFn) (*RunResult, error) {
	if emit == nil {
		emit = func(*Event) {}
	}

	if conv.Scope == "agent" && conv.AgentID == nil {
		return nil, errors.New("conversation is not bound to an agent")
	}

	rt, err := s.Assembler.Assemble(ctx, agent, conv)
	if err != nil {
		return nil, err
	}

	res := &RunResult{}

	// 1) 持久化用户消息（多轮记忆来源）
	userMsg := &store.Message{ConversationID: conv.ID, Role: "user", Content: input}
	if _, err := s.Store.InsertMessage(userMsg); err != nil {
		return nil, fmt.Errorf("save user message: %w", err)
	}

	// 2) 组装输入：历史（含刚落库的用户消息）
	history, err := s.Store.ListMessages(conv.ID)
	if err != nil {
		return nil, err
	}
	histMsgs := BuildHistoryMessages(history)

	// 3) 可取消 ctx
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

	// run.started（M4：backend 标注 + 装配期告警）
	start := time.Now()
	startData := map[string]any{
		"conversation_id": conv.ID,
		"agent_name":      rt.AgentName,
		"model":           rt.ModelLabel,
		"backend":         "inprocess", // M10 接入执行后端后按实际后端标注
	}
	if len(rt.Warnings) > 0 {
		startData["warnings"] = rt.Warnings
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

	// 知识库召回（M6，§11/§6.9：提问先检索 → retrieval 事件 → 上下文注入；失败降级不阻断）
	if conv.EnableKB && conv.KBID != nil && *conv.KBID != "" && s.KB != nil {
		kbcfg, kerr := s.Store.GetKnowledgeBase(*conv.KBID)
		if kerr != nil {
			s.emitAndRecord(runCtx, conv, runID, newEvent("run.warning", runID, map[string]any{"message": "知识库加载失败，本次回答未注入知识库内容: " + kerr.Error()}), emit)
		} else {
			hits, serr := s.KB.Search(runCtx, kbcfg, input, conv.TopK, conv.MinScore)
			switch {
			case serr != nil:
				s.emitAndRecord(runCtx, conv, runID, newEvent("run.warning", runID, map[string]any{"message": "知识库检索失败，本次回答未注入知识库内容: " + serr.Error()}), emit)
			case len(hits) > 0:
				hd := make([]map[string]any, 0, len(hits))
				for _, h := range hits {
					hd = append(hd, map[string]any{"doc": h.Doc, "seq": h.Seq, "score": h.Score, "excerpt": h.Excerpt})
				}
				s.emitAndRecord(runCtx, conv, runID, newEvent("retrieval", runID, map[string]any{"kb_id": kbcfg.ID, "hits": hd}), emit)
				if ctxText := kb.RenderContext(kbcfg.Name, hits); ctxText != "" {
					histMsgs = append(histMsgs, schema.SystemMessage(ctxText))
				}
			}
		}
	}

	var (
		buf        []byte
		stopped    bool
		runErr     string
		lastUsage  *schema.TokenUsage           // 最后一片的 token 用量（流式 usage 在尾片）
		lastFinish string                       // 最后一次 finish_reason
		toolAgg    = map[int]*pendingToolCall{} // 流式 tool_calls 增量聚合（按 Index）
		lastAgent  string                       // M4：subagent.enter/exit 检测
	)
	rootAgent := rt.AgentName

	// subagentLeave 发出子 Agent 退出事件
	subagentLeave := func(name string) {
		if name != "" && name != rootAgent {
			s.emitAndRecord(runCtx, conv, runID, newEvent("subagent.exit", runID, map[string]any{"agent": name}), emit)
		}
	}
	// trackAgent AgentName 变化 → enter/exit 事件（AgentTool 内部事件 / transfer 转移）
	trackAgent := func(name string) {
		if name == "" || name == lastAgent {
			return
		}
		if lastAgent != "" {
			subagentLeave(lastAgent)
		}
		if name != rootAgent {
			s.emitAndRecord(runCtx, conv, runID, newEvent("subagent.enter", runID, map[string]any{"agent": name}), emit)
		}
		lastAgent = name
	}

	// 深度思考内容（reasoner 类模型）独立事件流，不进正文
	emitReasoning := func(delta string) {
		if delta != "" {
			s.emitAndRecord(runCtx, conv, runID, newEvent("reasoning.delta", runID, map[string]any{"delta": delta}), emit)
		}
	}
	recordDelta := func(delta string) {
		buf = append(buf, delta...)
		if len(delta) > 0 {
			emit(newEvent("message.delta", runID, map[string]any{"delta": delta}))
		}
	}

	iter := rt.Runner.Run(runCtx, histMsgs)
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			if errors.Is(ev.Err, context.Canceled) {
				stopped = true
			} else {
				runErr = ev.Err.Error()
			}
			break
		}
		trackAgent(ev.AgentName)
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
						stopped = true
					} else {
						runErr = err.Error()
					}
					break
				}
				if chunk == nil {
					continue
				}
				if chunk.ReasoningContent != "" {
					emitReasoning(chunk.ReasoningContent)
				}
				if chunk.Content != "" {
					recordDelta(chunk.Content)
				}
				mergeToolCallChunk(toolAgg, chunk.ToolCalls)
				if chunk.ResponseMeta != nil {
					if chunk.ResponseMeta.Usage != nil {
						lastUsage = chunk.ResponseMeta.Usage
					}
					if chunk.ResponseMeta.FinishReason != "" {
						lastFinish = chunk.ResponseMeta.FinishReason
					}
				}
			}
			// 流结束：输出聚合完成的 tool.call（含入参 JSON 与 source）
			flushToolCalls(s, runCtx, conv, runID, rt, toolAgg, emit)
			for k := range toolAgg {
				delete(toolAgg, k)
			}
		case mo.Message != nil && mo.Role == schema.Tool:
			// 工具执行结果（对应发起见 tool.call 事件）
			data := map[string]any{"tool_name": mo.Message.ToolName, "content": mo.Message.Content}
			if mo.Message.ToolCallID != "" {
				data["tool_call_id"] = mo.Message.ToolCallID
			}
			if src := rt.SourceOf[mo.Message.ToolName]; src != "" {
				data["source"] = src
			}
			s.emitAndRecord(runCtx, conv, runID, newEvent("tool.result", runID, data), emit)
		case mo.Message != nil && mo.Role == schema.Assistant && !mo.IsStreaming:
			// 非流式完整输出（兜底）
			if mo.Message.ReasoningContent != "" {
				emitReasoning(mo.Message.ReasoningContent)
			}
			if mo.Message.Content != "" {
				recordDelta(mo.Message.Content)
			}
			for _, tc := range mo.Message.ToolCalls {
				emitToolCall(s, runCtx, conv, runID, rt, tc, emit)
			}
			if mo.Message.ResponseMeta != nil {
				if mo.Message.ResponseMeta.Usage != nil {
					lastUsage = mo.Message.ResponseMeta.Usage
				}
				if mo.Message.ResponseMeta.FinishReason != "" {
					lastFinish = mo.Message.ResponseMeta.FinishReason
				}
			}
		}
	}
	// 流收尾：仍在子 Agent 中 → 发 exit
	subagentLeave(lastAgent)

	// 4) 持久化助手消息
	if len(buf) > 0 {
		asg := &store.Message{ConversationID: conv.ID, Role: "assistant", Content: string(buf)}
		if m, err := s.Store.InsertMessage(asg); err == nil {
			res.AssistantMessageID = m.ID
		} else {
			log.Printf("[chat] save assistant message: %v", err)
		}
	}

	// 5) 终态事件（附带耗时 / token 用量 / finish_reason，供前端执行细节展示）
	finishData := func(reason string) map[string]any {
		data := map[string]any{"reason": reason, "elapsed_ms": time.Since(start).Milliseconds()}
		if lastUsage != nil {
			data["usage"] = map[string]any{
				"prompt_tokens":     lastUsage.PromptTokens,
				"completion_tokens": lastUsage.CompletionTokens,
				"total_tokens":      lastUsage.TotalTokens,
			}
		}
		if lastFinish != "" {
			data["finish_reason"] = lastFinish
		}
		return data
	}
	switch {
	case stopped:
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.finished", runID, finishData("stopped")), emit)
		res.Stopped = true
	case runErr != "":
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.error", runID, map[string]any{
			"code": "run_failed", "message": runErr, "elapsed_ms": time.Since(start).Milliseconds(),
		}), emit)
		res.Error = runErr
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
func flushToolCalls(s *Service, ctx context.Context, conv *store.Conversation, runID string, rt *BuildResult, agg map[int]*pendingToolCall, emit EmitFn) {
	for _, p := range agg {
		if p.Name == "" {
			continue
		}
		emitToolCall(s, ctx, conv, runID, rt, schema.ToolCall{
			ID: p.ID, Function: schema.FunctionCall{Name: p.Name, Arguments: p.Args.String()},
		}, emit)
	}
}

// emitToolCall 输出模型发起的工具调用事件（含入参 JSON 字符串与 source 标注）。
func emitToolCall(s *Service, ctx context.Context, conv *store.Conversation, runID string, rt *BuildResult, tc schema.ToolCall, emit EmitFn) {
	data := map[string]any{"tool_name": tc.Function.Name, "arguments": tc.Function.Arguments}
	if tc.ID != "" {
		data["tool_call_id"] = tc.ID
	}
	if rt != nil {
		if src := rt.SourceOf[tc.Function.Name]; src != "" {
			data["source"] = src
		}
	}
	s.emitAndRecord(ctx, conv, runID, newEvent("tool.call", runID, data), emit)
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
