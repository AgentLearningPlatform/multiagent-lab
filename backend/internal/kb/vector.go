// Package kb 知识库（M6，方案 §6.9）：检索后端抽象 + 切分 + Embedding + 索引/检索编排。
// 后端选择：KB_VECTOR_BACKEND=qdrant|sqlite（默认 qdrant，QDRANT_URL 未配或 dial 失败时可退 sqlite）。
// eino-ext components/retriever/qdrant 已核对（v0.0.0-20260916）：API 为 string query + 内部 Embedder + gRPC client，
// 与本平台流程（平台管 Embedding 连接、Search 直收向量）不匹配，故按 §344 采用 Qdrant 直连 REST（仍属开源路径）。
package kb

import (
	"context"
	"fmt"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// Chunk 待索引片段（向量已由 Service 层生成）。
type Chunk struct {
	ID      string // chunk id（SQLite 主键；Qdrant 路径转 UUID 作 point id）
	DocID   string
	Seq     int
	Content string
	Vector  []float32
}

// Hit 检索命中。
type Hit struct {
	ChunkID string
	DocID   string
	Seq     int
	Content string
	Score   float64
}

// VectorStore 检索后端抽象（方案 §6.9）。
type VectorStore interface {
	// EnsureCollection 确保 kb 对应的 collection 存在（dim=向量维度）。
	EnsureCollection(ctx context.Context, kbID string, dim int) error
	// Upsert 写入/覆盖向量（Qdrant：point id=chunk id UUID；SQLite：不适用，由 Service 直写 BLOB）。
	Upsert(ctx context.Context, kbID string, chunks []Chunk) error
	// Search 余弦 TopK + 阈值过滤。
	Search(ctx context.Context, kbID string, vec []float32, topK int, minScore float64) ([]Hit, error)
	// DeleteByDoc 按 doc 删除全部向量。
	DeleteByDoc(ctx context.Context, kbID, docID string) error
}

// collectionName 约定（§336）：kb_{id}。
func collectionName(kbID string) string { return "kb_" + kbID }

// NewVectorStore 按配置选择后端（st 供 SQLite fallback 检索使用）。
func NewVectorStore(backend, qdrantURL string, st *store.Store) (VectorStore, error) {
	switch backend {
	case "sqlite":
		return SQLiteStore{ST: st}, nil
	case "qdrant", "":
		if qdrantURL == "" {
			return nil, fmt.Errorf("KB_VECTOR_BACKEND=qdrant 需要 QDRANT_URL（或改用 sqlite）")
		}
		return NewQdrantStore(qdrantURL), nil
	default:
		return nil, fmt.Errorf("未知 KB_VECTOR_BACKEND %q（支持 qdrant|sqlite）", backend)
	}
}
