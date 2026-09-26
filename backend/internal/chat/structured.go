// structured.go 实现模型能力代理的结构化生成（M8 §6.10 / REQ-98）。
// 供构建平面 AI 创建复用主平台模型连接：模型能力归主平台，校验归构建平面。
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// Usage 结构化生成的 token 用量（透传给构建平面展示）。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// GenerateStructuredResult 结构化生成结果。
type GenerateStructuredResult struct {
	DraftJSON json.RawMessage `json:"draft_json"`
	Usage     *Usage          `json:"usage"`
}

// GenerateStructured 用指定（或全局默认 chat）模型连接做 JSON 结构化生成。
// schemaJSON 为目标 JSON Schema（字符串），模型被约束按 schema 输出 JSON。
// 注：P1 用提示词约束 + JSON 提取实现（对支持 response_format 的模型同样有效）。
func GenerateStructured(ctx context.Context, st *store.Store, box *secrets.Box, connID, prompt, schemaJSON string) (*GenerateStructuredResult, error) {
	// 1) 解析连接：显式 conn_id > 全局默认 chat 连接（与 buildModel 同规则）
	var rec *store.ConnectionRecord
	var err error
	if connID != "" {
		rec, err = st.GetConnectionRecord(connID)
		if err != nil {
			return nil, fmt.Errorf("resolve model connection %s: %w", connID, err)
		}
	} else {
		def, derr := st.GetDefaultConnection("chat")
		if derr != nil {
			return nil, derr
		}
		if def == nil {
			return nil, &ModelNotConfiguredError{}
		}
		rec, err = st.GetConnectionRecord(def.ID)
		if err != nil {
			return nil, err
		}
	}
	if !rec.Conn.Enabled {
		return nil, fmt.Errorf("模型连接 %q 已停用，请在「设置-模型连接」启用或更换", rec.Conn.Name)
	}
	apiKey := ""
	if len(rec.Encrypted) > 0 {
		apiKey, err = box.Decrypt(rec.Encrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt api key: %w", err)
		}
	}
	cm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  apiKey,
		BaseURL: rec.Conn.BaseURL,
		Model:   rec.Conn.ModelName,
	})
	if err != nil {
		return nil, fmt.Errorf("create chat model: %w", err)
	}

	// 2) 结构化生成：schema 注入 system，要求仅输出 JSON
	sys := "你是结构化数据生成器。请严格按给定的 JSON Schema 生成一份符合结构的 JSON 草稿。\n" +
		"要求：只输出一个 JSON 对象，不要输出任何解释、Markdown 代码围栏或其他文本。\n\nJSON Schema:\n" + schemaJSON
	msgs := []*schema.Message{
		schema.SystemMessage(sys),
		schema.UserMessage(prompt),
	}
	msg, err := cm.Generate(ctx, msgs)
	if err != nil {
		return nil, fmt.Errorf("generate: %w", err)
	}

	// 3) 提取并校验 JSON（容忍模型偶尔输出的围栏/前后噪声）
	raw := extractJSON(msg.Content)
	if raw == nil {
		return nil, fmt.Errorf("模型未返回有效 JSON")
	}

	// 4) usage（eino ResponseMeta.Usage，可能为 nil）
	var usage *Usage
	if meta := msg.ResponseMeta; meta != nil && meta.Usage != nil {
		usage = &Usage{
			PromptTokens:     meta.Usage.PromptTokens,
			CompletionTokens: meta.Usage.CompletionTokens,
			TotalTokens:      meta.Usage.TotalTokens,
		}
	}
	return &GenerateStructuredResult{DraftJSON: raw, Usage: usage}, nil
}

// extractJSON 从模型输出中提取首个完整 JSON 值（剥围栏、找首尾大括号）。
func extractJSON(content string) json.RawMessage {
	c := strings.TrimSpace(content)
	// 剥 ```json ... ``` 围栏
	if strings.HasPrefix(c, "```") {
		c = strings.TrimPrefix(c, "```json")
		c = strings.TrimPrefix(c, "```")
		if i := strings.LastIndex(c, "```"); i >= 0 {
			c = c[:i]
		}
		c = strings.TrimSpace(c)
	}
	if c == "" {
		return nil
	}
	if json.Valid([]byte(c)) {
		return json.RawMessage(c)
	}
	// 首个 { 到最后一个 }（对象）
	if i := strings.Index(c, "{"); i >= 0 {
		if j := strings.LastIndex(c, "}"); j > i {
			cand := c[i : j+1]
			if json.Valid([]byte(cand)) {
				return json.RawMessage(cand)
			}
		}
	}
	return nil
}

// GenerateText 通用自由文本生成（M27/REQ-166 平台助手用）：默认或指定连接 +
// system/user 双消息 + 可选温度 → 纯文本输出（不走 JSON schema 契约）。
func GenerateText(ctx context.Context, st *store.Store, box *secrets.Box, connID, system, user string, temperature *float64) (string, error) {
	var rec *store.ConnectionRecord
	var err error
	if connID != "" {
		rec, err = st.GetConnectionRecord(connID)
		if err != nil {
			return "", fmt.Errorf("resolve model connection %s: %w", connID, err)
		}
	} else {
		def, derr := st.GetDefaultConnection("chat")
		if derr != nil {
			return "", derr
		}
		if def == nil {
			return "", &ModelNotConfiguredError{}
		}
		rec, err = st.GetConnectionRecord(def.ID)
		if err != nil {
			return "", err
		}
	}
	if !rec.Conn.Enabled {
		return "", fmt.Errorf("模型连接 %q 已停用，请在「设置-模型连接」启用或更换", rec.Conn.Name)
	}
	apiKey := ""
	if len(rec.Encrypted) > 0 {
		apiKey, err = box.Decrypt(rec.Encrypted)
		if err != nil {
			return "", fmt.Errorf("decrypt api key: %w", err)
		}
	}
	cfg := &openai.ChatModelConfig{APIKey: apiKey, BaseURL: rec.Conn.BaseURL, Model: rec.Conn.ModelName}
	if temperature != nil {
		t := float32(*temperature)
		cfg.Temperature = &t
	}
	cm, err := openai.NewChatModel(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("create chat model: %w", err)
	}
	msg, err := cm.Generate(ctx, []*schema.Message{schema.SystemMessage(system), schema.UserMessage(user)})
	if err != nil {
		return "", fmt.Errorf("generate: %w", err)
	}
	return strings.TrimSpace(msg.Content), nil
}
