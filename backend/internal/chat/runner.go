package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// Service 运行服务：装配、执行、停止。
type Service struct {
	Store     *store.Store
	Assembler *Assembler

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // conversationID -> cancel
}

// NewService 构造。
func NewService(st *store.Store, asm *Assembler) *Service {
	return &Service{Store: st, Assembler: asm, cancels: map[string]context.CancelFunc{}}
}

// EmitFn 平台 SSE 事件输出函数（由 API 层注入）。
type EmitFn func(ev *Event)

// Event 平台事件（方案 §7 统一协议）。
type Event struct {
	Type      string          `json:"type"`
	RunID     string          `json:"run_id,omitempty"`
	Ts        string          `json:"ts"`
	Data      json.RawMessage `json:"data,omitempty"`
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

// Run 执行一次对话运行：持久化用户消息 → 装配 → 流式执行 → 翻译事件 → 持久化助手回复。
// emit 事件若为 nil 则仅落库（供测试）。
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

	// 2) 组装输入：历史（含刚落库的用户消息）+ 新输入由 Run 逐条传入
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

	// run.started
	s.emitAndRecord(runCtx, conv, runID, newEvent("run.started", runID, map[string]any{
		"conversation_id": conv.ID,
		"agent_name":      rt.AgentName,
		"model":           rt.ModelLabel,
	}), emit)

	var (
		buf        []byte
		stopped    bool
		runErr     string
		msgEmitted bool
	)
	recordDelta := func(delta string) {
		buf = append(buf, delta...)
		if len(delta) > 0 {
			emit(newEvent("message.delta", runID, map[string]any{"delta": delta}))
			msgEmitted = true
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
				if chunk != nil && chunk.Content != "" {
					recordDelta(chunk.Content)
				}
			}
		case mo.Message != nil && mo.Role == schema.Tool:
			// 工具结果事件（M2 无工具，翻译层预留）
			s.emitAndRecord(runCtx, conv, runID, newEvent("tool.result", runID, map[string]any{
				"tool_name": mo.ToolName, "content": mo.Message.Content,
			}), emit)
		case mo.Message != nil && mo.Role == schema.Assistant && !mo.IsStreaming:
			// 非流式完整输出（兜底）
			if mo.Message.Content != "" {
				recordDelta(mo.Message.Content)
			}
		}
	}

	// 4) 持久化助手消息
	if len(buf) > 0 {
		asg := &store.Message{ConversationID: conv.ID, Role: "assistant", Content: string(buf)}
		if m, err := s.Store.InsertMessage(asg); err == nil {
			res.AssistantMessageID = m.ID
		} else {
			log.Printf("[chat] save assistant message: %v", err)
		}
	}

	// 5) 终态事件
	switch {
	case stopped:
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.finished", runID, map[string]any{"reason": "stopped"}), emit)
		res.Stopped = true
	case runErr != "":
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.error", runID, map[string]any{"code": "run_failed", "message": runErr}), emit)
		res.Error = runErr
	default:
		_ = msgEmitted
		s.emitAndRecord(runCtx, conv, runID, newEvent("run.finished", runID, map[string]any{"reason": "completed"}), emit)
	}
	return res, nil
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
