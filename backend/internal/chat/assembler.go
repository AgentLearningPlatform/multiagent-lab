// Package chat 负责 Agent 运行时装配与执行（Q-17：M2 默认 inprocess 后端）。
// 每次运行按当前配置装配（配置驱动，不追溯），对接 Eino ADK。
package chat

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// Assembler 从平台配置装配 Eino Runner。
type Assembler struct {
	Store *store.Store
	Box   *secrets.Box
}

// Runtime 装配产物。
type Runtime struct {
	Runner     *adk.Runner
	AgentName  string
	ModelLabel string // 连接名@模型名，便于展示
	ConnID     string
}

// Assemble 按对话所属 Agent 的当前配置装配 Runner（单 Agent inprocess，M2）。
func (a *Assembler) Assemble(ctx context.Context, agent *store.Agent, conv *store.Conversation) (*Runtime, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent is nil")
	}

	// 解析模型连接：agent 显式指定 > 默认 chat 连接
	var rec *store.ConnectionRecord
	var err error
	if agent.ModelConnID != nil && *agent.ModelConnID != "" {
		rec, err = a.Store.GetConnectionRecord(*agent.ModelConnID)
		if err != nil {
			return nil, fmt.Errorf("resolve model connection %s: %w", *agent.ModelConnID, err)
		}
	} else {
		def, derr := a.Store.GetDefaultConnection("chat")
		if derr != nil {
			return nil, derr
		}
		if def == nil {
			return nil, &ModelNotConfiguredError{}
		}
		rec, err = a.Store.GetConnectionRecord(def.ID)
		if err != nil {
			return nil, err
		}
	}
	if !rec.Conn.Enabled {
		return nil, fmt.Errorf("model connection %q is disabled", rec.Conn.Name)
	}

	// 解密 API Key
	apiKey := ""
	if len(rec.Encrypted) > 0 {
		apiKey, err = a.Box.Decrypt(rec.Encrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt api key: %w", err)
		}
	}

	// OpenAI 兼容 ChatModel（REQ-14）
	cm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  apiKey,
		BaseURL: rec.Conn.BaseURL,
		Model:   rec.Conn.ModelName,
	})
	if err != nil {
		return nil, fmt.Errorf("create chat model: %w", err)
	}

	maxIter := agent.MaxIteration
	if maxIter <= 0 {
		maxIter = 25
	}

	// Eino ADK ChatModelAgent（无工具，M2 单 Agent）
	agentInst, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          agent.Name,
		Description:   agent.Description,
		Instruction:   agent.Instruction,
		Model:         cm,
		MaxIterations: maxIter,
	})
	if err != nil {
		return nil, fmt.Errorf("create adk agent: %w", err)
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:          agentInst,
		EnableStreaming: true,
	})
	return &Runtime{
		Runner:     runner,
		AgentName:  agent.Name,
		ModelLabel: rec.Conn.Name + "@" + rec.Conn.ModelName,
		ConnID:     rec.Conn.ID,
	}, nil
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
