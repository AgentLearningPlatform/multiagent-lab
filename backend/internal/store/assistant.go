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
