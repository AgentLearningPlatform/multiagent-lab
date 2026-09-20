package kb

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// Service 知识库编排：导入切分 → Embedding → VectorStore → 状态回写；检索；删除级联（§6.9）。
type Service struct {
	Store  *store.Store
	Box    *secrets.Box
	Vector VectorStore
}

// NewService 构造（backend=qdrant|sqlite，qdrantURL 见 §469 QDRANT_URL）。
func NewService(st *store.Store, box *secrets.Box, backend, qdrantURL string) (*Service, error) {
	vs, err := NewVectorStore(backend, qdrantURL, st)
	if err != nil {
		return nil, err
	}
	return &Service{Store: st, Box: box, Vector: vs}, nil
}

// embedder 惰性构造（依赖 Store/Box）。
func (s *Service) embedder() *Embedder { return &Embedder{Store: s.Store, Box: s.Box} }

// Import 粘贴文本导入并同步索引（学习平台数据量小，同步完成；状态机 pending→indexing→success|failed）。
func (s *Service) Import(ctx context.Context, kbID, title, content string) (*store.KnowledgeDoc, error) {
	if _, err := s.Store.GetKnowledgeBase(kbID); err != nil {
		return nil, err
	}
	pieces := SplitText(content)
	if len(pieces) == 0 {
		return nil, fmt.Errorf("内容为空，无法索引")
	}
	doc, err := s.Store.CreateKnowledgeDoc(&store.KnowledgeDoc{KBID: kbID, Title: title, Status: "indexing"})
	if err != nil {
		return nil, err
	}
	if err := s.indexDoc(ctx, kbID, doc, pieces); err != nil {
		s.Store.UpdateKnowledgeDocStatus(doc.ID, "failed", 0, err.Error())
		return nil, fmt.Errorf("索引失败: %w", err)
	}
	return s.Store.GetKnowledgeDoc(doc.ID)
}

// Reindex 重建文档索引（先清旧向量与 chunks）。
func (s *Service) Reindex(ctx context.Context, kbID, docID string) (*store.KnowledgeDoc, error) {
	doc, err := s.Store.GetKnowledgeDoc(docID)
	if err != nil {
		return nil, err
	}
	if doc.KBID != kbID {
		return nil, store.ErrNotFound
	}
	// 旧正文在删 chunks 前取出（重新切分用原文；正文即 chunk 拼接，避免额外存储）
	old, err := s.Store.ListKnowledgeChunksByDoc(docID)
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	for i, c := range old {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(c.Content)
	}
	if sb.Len() == 0 {
		return nil, fmt.Errorf("文档无可重建的原文内容")
	}
	if err := s.Vector.DeleteByDoc(ctx, kbID, docID); err != nil {
		return nil, fmt.Errorf("清理旧向量: %w", err)
	}
	if err := s.Store.DeleteKnowledgeChunksByDoc(docID); err != nil {
		return nil, err
	}
	s.Store.UpdateKnowledgeDocStatus(docID, "indexing", 0, "")
	if err := s.indexDoc(ctx, kbID, doc, SplitText(sb.String())); err != nil {
		s.Store.UpdateKnowledgeDocStatus(docID, "failed", 0, err.Error())
		return nil, fmt.Errorf("索引失败: %w", err)
	}
	return s.Store.GetKnowledgeDoc(docID)
}

// indexDoc 切分→embed→写入向量库与 chunks→状态回写。
func (s *Service) indexDoc(ctx context.Context, kbID string, doc *store.KnowledgeDoc, pieces []string) error {
	start := time.Now()
	vecs, err := s.embedder().EmbedTexts(ctx, pieces)
	if err != nil {
		return err
	}
	if len(vecs) != len(pieces) {
		return fmt.Errorf("embedding 数量不匹配: %d/%d", len(vecs), len(pieces))
	}
	dim := len(vecs[0])
	if dim == 0 {
		return fmt.Errorf("embedding 维度为 0")
	}
	// Qdrant 路径：确保集合（以首个向量维度建）+ Upsert 向量 + chunks 存正文/vector_ref
	// SQLite 路径：向量 BLOB 随 chunk 落库
	backend := s.backendName()
	if backend == "qdrant" {
		if err := s.Vector.EnsureCollection(ctx, kbID, dim); err != nil {
			return fmt.Errorf("qdrant collection: %w", err)
		}
	}
	chunks := make([]*store.KnowledgeChunk, 0, len(pieces))
	pts := make([]Chunk, 0, len(pieces))
	for i, piece := range pieces {
		c := &store.KnowledgeChunk{
			ID:           store.NewID(), // 先生成：Qdrant point id 与 chunk id 一一对应
			KBID:         kbID,
			DocID:        doc.ID,
			Seq:          i,
			Content:      piece,
			StoreBackend: backend,
		}
		if backend == "qdrant" {
			c.VectorRef = pointUUIDOf(c.ID)
		} else {
			c.Vector = EncodeVector(vecs[i])
		}
		chunks = append(chunks, c)
		pts = append(pts, Chunk{ID: c.ID, DocID: doc.ID, Seq: i, Content: piece, Vector: vecs[i]})
	}
	if backend == "qdrant" {
		if err := s.Vector.Upsert(ctx, kbID, pts); err != nil {
			return fmt.Errorf("qdrant upsert: %w", err)
		}
	}
	if err := s.Store.InsertKnowledgeChunks(chunks); err != nil {
		return fmt.Errorf("save chunks: %w", err)
	}
	if err := s.Store.UpdateKnowledgeDocStatus(doc.ID, "success", len(pieces), ""); err != nil {
		return err
	}
	log.Printf("[kb] doc %q indexed: %d chunks, dim=%d, %s", doc.Title, len(pieces), dim, time.Since(start).Round(time.Millisecond))
	return nil
}

// DeleteDoc 删除文档：向量库级联清理 + chunks + doc。
func (s *Service) DeleteDoc(ctx context.Context, kbID, docID string) error {
	if _, err := s.Store.GetKnowledgeDoc(docID); err != nil {
		return err
	}
	if err := s.Vector.DeleteByDoc(ctx, kbID, docID); err != nil {
		// 向量库清理失败不阻塞删除（chunks 已清，残留 point 无引用；SQLite 路径无操作）
		log.Printf("[kb] warn: delete vectors for doc %s: %v", docID, err)
	}
	if err := s.Store.DeleteKnowledgeChunksByDoc(docID); err != nil {
		return err
	}
	return s.Store.DeleteKnowledgeDoc(kbID, docID)
}

// DeleteKB 删除整个知识库（逐 doc 清向量）。
func (s *Service) DeleteKB(ctx context.Context, kbID string) error {
	docs, err := s.Store.ListKnowledgeDocs(kbID)
	if err != nil {
		return err
	}
	for _, d := range docs {
		s.Vector.DeleteByDoc(ctx, kbID, d.ID)
	}
	return s.Store.DeleteKnowledgeBase(kbID)
}

// Search 对话召回 / 试运行：问题 Embedding → TopK + 阈值 → hits（doc 标题已解析）。
func (s *Service) Search(ctx context.Context, kb *store.KnowledgeBase, query string, topK int, minScore float64) ([]RetrievalHit, error) {
	if query == "" {
		return nil, nil
	}
	if topK <= 0 {
		topK = kb.TopK
	}
	if topK <= 0 {
		topK = 4
	}
	if minScore <= 0 {
		minScore = kb.MinScore
	}
	vec, err := s.embedder().EmbedOne(ctx, query)
	if err != nil {
		return nil, err
	}
	hits, err := s.Vector.Search(ctx, kb.ID, vec, topK, minScore)
	if err != nil {
		return nil, err
	}
	// 解析文档标题
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.DocID)
	}
	titles, err := s.Store.DocTitles(ids)
	if err != nil {
		return nil, err
	}
	out := make([]RetrievalHit, 0, len(hits))
	for _, h := range hits {
		name := titles[h.DocID]
		if name == "" {
			name = h.DocID
		}
		out = append(out, RetrievalHit{Doc: name, Seq: h.Seq, Score: h.Score, Excerpt: truncateRunes(h.Content, 200)})
	}
	return out, nil
}

// RetrievalHit retrieval 事件 / search-preview 响应条目（§306：hits[{doc,seq,score,excerpt}]）。
type RetrievalHit struct {
	Doc     string  `json:"doc"`
	Seq     int     `json:"seq"`
	Score   float64 `json:"score"`
	Excerpt string  `json:"excerpt"`
}

// RenderContext 检索结果注入文本（§341：[片段 doc:seq score] 格式）。
func RenderContext(kbName string, hits []RetrievalHit) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# 知识库参考（来自知识库「" + kbName + "」，回答时优先依据以下片段并标注来源）\n")
	for _, h := range hits {
		fmt.Fprintf(&b, "[片段 %s:%d 得分%.3f]\n%s\n\n", h.Doc, h.Seq, h.Score, h.Excerpt)
	}
	return strings.TrimRight(b.String(), "\n")
}

// backendName 当前向量后端（sqlite|qdrant）。
func (s *Service) backendName() string {
	switch s.Vector.(type) {
	case *QdrantStore:
		return "qdrant"
	default:
		return "sqlite"
	}
}

// pointUUIDOf 与 qdrant.go 的 pointUUID 一致（避免循环依赖，此包内直接复用）。
func pointUUIDOf(chunkID string) string { return pointUUID(chunkID) }

func truncateRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}
