// 内置系统级 Agent「平台助手」配置（M27/REQ-166）：单行存储（id='default'）。
// 平台助手不落 agent 表——不占用户 Agent 列表、不可删除（REQ-166 硬性要求）；
// system_prompt 为用户微调（追加到内置提示词后），model_conn_id/temperature 覆盖默认。
package store

import (
	"database/sql"
	"strings"
)

// AssistantConfig 平台助手配置（单例）。
type AssistantConfig struct {
	SystemPrompt string   `json:"system_prompt,omitempty"`
	ModelConnID  string   `json:"model_conn_id,omitempty"`
	Temperature  *float64 `json:"temperature,omitempty"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
}

const assistantCols = `system_prompt,model_conn_id,temperature,updated_at`

// GetAssistantConfig 读取配置（无行/未配置时返回零值结构，不视为错误）。
func (s *Store) GetAssistantConfig() (*AssistantConfig, error) {
	row := s.DB.QueryRow(`SELECT ` + assistantCols + ` FROM assistant_config WHERE id='default'`)
	var c AssistantConfig
	var temp sql.NullFloat64
	if err := row.Scan(&c.SystemPrompt, &c.ModelConnID, &temp, &c.UpdatedAt); err != nil {
		// 表中必有 default 种子行；防御性返回零值
		return &AssistantConfig{}, nil
	}
	if temp.Valid {
		t := temp.Float64
		c.Temperature = &t
	}
	return &c, nil
}

// SaveAssistantConfig 保存配置（upsert；仅覆盖提供的字段——空串/nil 保留原值，
// 需要"清空回默认"时由调用方传占位语义）。
func (s *Store) SaveAssistantConfig(c *AssistantConfig) error {
	cur, err := s.GetAssistantConfig()
	if err != nil {
		return err
	}
	sp := cur.SystemPrompt
	if strings.TrimSpace(c.SystemPrompt) != "" || c.SystemPrompt != "" {
		sp = c.SystemPrompt
	}
	mc := cur.ModelConnID
	if c.ModelConnID != "" {
		mc = c.ModelConnID
	}
	t := cur.Temperature
	if c.Temperature != nil {
		t = c.Temperature
	}
	_, err = s.DB.Exec(`INSERT INTO assistant_config(id,system_prompt,model_conn_id,temperature,updated_at)
		VALUES ('default',?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET system_prompt=excluded.system_prompt, model_conn_id=excluded.model_conn_id,
		temperature=excluded.temperature, updated_at=excluded.updated_at`,
		sp, mc, t, now())
	return err
}

// EnsureBuiltinAssistantInstruction REQ-192/M32 配置单源归一引导：builtin 行 instruction 空
// 时写入 defaultPrompt（一次性移植 assistant_config 表存量微调——「追加到基座后」旧语义物化
// 为全量文本），此后 agent 内置行即配置单源、assistant_config 退役；幂等（行内非空则不动）。
func (s *Store) EnsureBuiltinAssistantInstruction(defaultPrompt string) error {
	var legacy sql.NullString
	_ = s.DB.QueryRow(`SELECT system_prompt FROM assistant_config WHERE id='default'`).Scan(&legacy)
	a, err := s.GetAgent("builtin-assistant")
	if err != nil {
		return err
	}
	if a == nil || strings.TrimSpace(a.Instruction) != "" {
		return nil
	}
	final := defaultPrompt
	if strings.TrimSpace(legacy.String) != "" {
		final = defaultPrompt + "\n\n# 用户微调（自 assistant_config 存量移植，REQ-192）\n" + strings.TrimSpace(legacy.String)
	}
	a.Instruction = final
	_, err = s.UpdateAgent(a)
	return err
}

// EnsureAssistantContentConv REQ-192⑤ 调用留痕：确保「内容优化」会话存在（builtin-assistant
// 名下，AI 内容优化统一落此会话，来源标注在消息 meta），返回会话 ID。
func (s *Store) EnsureAssistantContentConv() (string, error) {
	list, err := s.ListConversations(ConversationFilter{Scope: "agent", AgentID: "builtin-assistant"})
	if err != nil {
		return "", err
	}
	for _, c := range list {
		if c.Title == "内容优化" {
			return c.ID, nil
		}
	}
	id := "builtin-assistant"
	c, err := s.CreateConversation(&Conversation{Scope: "agent", AgentID: &id, Title: "内容优化"})
	if err != nil {
		return "", err
	}
	return c.ID, nil
}
