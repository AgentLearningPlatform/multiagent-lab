package kb

import (
	"context"
	"fmt"

	eopenai "github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/embedding"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// Embedder 文本向量化（§6.9：默认 embedding 连接，OpenAI 兼容 /embeddings）。
type Embedder struct {
	Store *store.Store
	Box   *secrets.Box
}

// resolve 解析默认 embedding 连接并构造 eino-ext openai Embedder。
func (e *Embedder) resolve(ctx context.Context) (embedding.Embedder, error) {
	def, err := e.Store.GetDefaultConnection("embedding")
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, fmt.Errorf("未配置可用的 Embedding 模型连接，请先到「设置-模型连接」添加 conn_type=embedding 的连接并设为默认")
	}
	if !def.Enabled {
		return nil, fmt.Errorf("Embedding 连接 %q 已停用，请启用或更换", def.Name)
	}
	rec, err := e.Store.GetConnectionRecord(def.ID)
	if err != nil {
		return nil, err
	}
	apiKey := ""
	if len(rec.Encrypted) > 0 {
		apiKey, err = e.Box.Decrypt(rec.Encrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt embedding api key: %w", err)
		}
	}
	emb, err := eopenai.NewEmbedder(ctx, &eopenai.EmbeddingConfig{
		APIKey:  apiKey,
		BaseURL: rec.Conn.BaseURL,
		Model:   rec.Conn.ModelName,
		Timeout: embedTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("create embedding client: %w", err)
	}
	return emb, nil
}

// EmbedTexts 批量向量化（返回 float32，供 VectorStore 使用）。
func (e *Embedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	emb, err := e.resolve(ctx)
	if err != nil {
		return nil, err
	}
	// 分批（embedding API 对批量大小敏感，64 一批稳妥）
	const batch = 64
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += batch {
		end := start + batch
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := emb.EmbedStrings(ctx, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("embedding: %w", err)
		}
		for _, v64 := range vecs {
			v32 := make([]float32, len(v64))
			for i, x := range v64 {
				v32[i] = float32(x)
			}
			out = append(out, v32)
		}
	}
	return out, nil
}

// EmbedOne 单文本向量化。
func (e *Embedder) EmbedOne(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.EmbedTexts(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embedding 返回空结果")
	}
	return vecs[0], nil
}
