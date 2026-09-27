package chat

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/modelproto"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// anthropicDefaultMaxTokens Anthropic Messages API 必填参数的兜底值（连接/Agent 均未配置 MaxTokens 时）。
// 4096 在推理模型下会被打爆：思考、回复与流式工具调用参数共享该输出预算，复杂任务中途截断
// （工具参数 JSON 未写完即断流 → eino 解析 eof → 整个 run 报错，用户观感是「答到一半卡住」）。
// 16384 覆盖主流 Anthropic 兼容端点的模型上限（GLM 系最大输出 ≥128K、Kimi 系 ≥8K）；个别
// 上限更小的端点若拒绝该值，首轮即报 400（不静默），在 Agent 配置调小「最大回复 tokens」即可。
const anthropicDefaultMaxTokens = 16384

// buildChatModel 按连接协议构造 eino ChatModel（REQ-172）：
//   - anthropic → eino-ext/components/model/claude（Messages API；BaseURL 传网关根地址，
//     SDK 自拼 /v1/messages，兼容 DeepSeek/智谱/Kimi 等 Anthropic 兼容端点）；
//   - 其余（含空 = 默认 openai_compat）→ eino-ext/components/model/openai（OpenAI 兼容，现状）。
//
// Anthropic 侧特有约束：MaxTokens 为 API 必填（未配置兜底 anthropicDefaultMaxTokens）；温度域 [0,1]（超界钳制，
// Agent/助手侧 0~2 的取值进入该通道时按上限 1 处理）。
func buildChatModel(ctx context.Context, conn *store.ModelConnection, apiKey string, temperature *float64, maxTokens *int) (model.BaseChatModel, error) {
	if modelproto.IsAnthropic(conn.Protocol) {
		base := modelproto.NormalizeAnthropicBase(conn.BaseURL)
		cfg := &claude.Config{
			APIKey:    apiKey,
			BaseURL:   &base,
			Model:     conn.ModelName,
			MaxTokens: anthropicDefaultMaxTokens,
		}
		if maxTokens != nil && *maxTokens > 0 {
			cfg.MaxTokens = *maxTokens
		}
		if temperature != nil {
			t := float32(*temperature)
			if t > 1 {
				t = 1
			} else if t < 0 {
				t = 0
			}
			cfg.Temperature = &t
		}
		cm, err := claude.NewChatModel(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("create chat model (anthropic): %w", err)
		}
		return cm, nil
	}
	cfg := &openai.ChatModelConfig{
		APIKey:  apiKey,
		BaseURL: conn.BaseURL,
		Model:   conn.ModelName,
	}
	if temperature != nil {
		t := float32(*temperature)
		cfg.Temperature = &t
	}
	if maxTokens != nil {
		mt := *maxTokens
		cfg.MaxTokens = &mt
	}
	cm, err := openai.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create chat model: %w", err)
	}
	return cm, nil
}
