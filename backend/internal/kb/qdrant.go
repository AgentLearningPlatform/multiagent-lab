package kb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// QdrantStore Qdrant REST 直连（§336：collection=kb_{id}，cosine，point id=chunk id）。
// 仅用 REST（:6333），不引入 gRPC 依赖。
type QdrantStore struct {
	base string // e.g. http://127.0.0.1:6333
	hc   *http.Client
}

// NewQdrantStore 构造（base 形如 http://127.0.0.1:6333）。
func NewQdrantStore(base string) *QdrantStore {
	return &QdrantStore{base: strings.TrimRight(base, "/"), hc: &http.Client{Timeout: 30 * time.Second}}
}

// pointUUID Qdrant point id 必须为 uint 或 UUID：32 位 hex chunk id 转 8-4-4-4-12 UUID。
func pointUUID(chunkID string) string {
	h := strings.ReplaceAll(strings.ToLower(chunkID), "-", "")
	if len(h) != 32 {
		h = fmt.Sprintf("%032x", chunkID) // 非 hex id 的兜底（学习平台数据可控）
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

// ping Qdrant 可达性探测（healthz 汇总用）：GET /collections 返回 2xx 即视为可达。
func (q *QdrantStore) ping(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(q.base, "/")+"/collections", nil)
	if err != nil {
		return false
	}
	resp, err := q.hc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500
}

func (q *QdrantStore) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, q.base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := q.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant %s %s: %d %s", method, path, resp.StatusCode, truncate(string(raw), 200))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("qdrant decode: %w", err)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// EnsureCollection 集合不存在则创建（cosine，dim 维）。
func (q *QdrantStore) EnsureCollection(ctx context.Context, kbID string, dim int) error {
	c := collectionName(kbID)
	var exists struct {
		Result struct {
			Exists bool `json:"exists"`
		} `json:"result"`
	}
	if err := q.do(ctx, http.MethodGet, "/collections/"+c+"/exists", nil, &exists); err != nil {
		return err
	}
	if exists.Result.Exists {
		return nil
	}
	return q.do(ctx, http.MethodPut, "/collections/"+c, map[string]any{
		"vectors": map[string]any{"size": dim, "distance": "Cosine"},
	}, nil)
}

type qdrantPoint struct {
	ID      string         `json:"id"`
	Vector  []float32      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

// Upsert 批量写入向量点（payload 冗余存 chunk 元数据，检索免回查）。
func (q *QdrantStore) Upsert(ctx context.Context, kbID string, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	points := make([]qdrantPoint, 0, len(chunks))
	for _, ch := range chunks {
		points = append(points, qdrantPoint{
			ID:     pointUUID(ch.ID),
			Vector: ch.Vector,
			Payload: map[string]any{
				"chunk_id": ch.ID,
				"doc_id":   ch.DocID,
				"seq":      ch.Seq,
				"content":  ch.Content,
			},
		})
	}
	return q.do(ctx, http.MethodPut, "/collections/"+collectionName(kbID)+"/points?wait=true",
		map[string]any{"points": points}, nil)
}

type qdrantSearchResp struct {
	Result []struct {
		ID      string         `json:"id"`
		Score   float64        `json:"score"`
		Payload map[string]any `json:"payload"`
	} `json:"result"`
}

// Search 余弦 TopK + 阈值。
func (q *QdrantStore) Search(ctx context.Context, kbID string, vec []float32, topK int, minScore float64) ([]Hit, error) {
	body := map[string]any{
		"vector":       vec,
		"limit":        topK,
		"with_payload": true,
	}
	if minScore > 0 {
		body["score_threshold"] = minScore
	}
	var resp qdrantSearchResp
	if err := q.do(ctx, http.MethodPost, "/collections/"+collectionName(kbID)+"/points/search", body, &resp); err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(resp.Result))
	for _, r := range resp.Result {
		h := Hit{Score: r.Score}
		if v, ok := r.Payload["chunk_id"].(string); ok {
			h.ChunkID = v
		}
		if v, ok := r.Payload["doc_id"].(string); ok {
			h.DocID = v
		}
		if v, ok := r.Payload["content"].(string); ok {
			h.Content = v
		}
		if v, ok := r.Payload["seq"].(float64); ok {
			h.Seq = int(v)
		}
		out = append(out, h)
	}
	return out, nil
}

// DeleteByDoc 按 payload.doc_id 过滤删除。
func (q *QdrantStore) DeleteByDoc(ctx context.Context, kbID, docID string) error {
	return q.do(ctx, http.MethodPost, "/collections/"+collectionName(kbID)+"/points/delete", map[string]any{
		"filter": map[string]any{
			"must": []any{map[string]any{"key": "doc_id", "match": map[string]any{"value": docID}}},
		},
	}, nil)
}
