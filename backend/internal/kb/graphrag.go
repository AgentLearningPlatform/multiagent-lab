package kb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---- GraphRAG 子模块（M14，D-KB4：KB 双子模块 mode=rag|graphrag）----
//
// graphrag 模式的 KB 在向量索引（chunks/embedding，与 rag 共用）之外，
// 将 chunk 同步抽取为 KG（semantica worker :8093 POST /graphrag/ingest），
// 检索走 worker /query（GraphRAG claims）。worker 不可达时统一降级（M14 ⑥）：
// ingest 记 degraded 不阻断导入，query 回退向量检索。

const defaultWorkerURL = "http://127.0.0.1:8093"

// workerURL semantica worker 地址（SEMANTICA_WORKER_URL，与本体平面 §4.9 D-O10 同源）。
func workerURL() string {
	if v := os.Getenv("SEMANTICA_WORKER_URL"); v != "" {
		return v
	}
	return defaultWorkerURL
}

// workerPost 调 worker JSON 端点（短超时，M14 ⑥：任何失败都按降级处理不阻断主流程）。
func workerPost(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, workerURL()+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("worker %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// GraphragIngest graphrag 模式文档索引成功后，把 chunks 推给 worker 抽 KG（M14 ②）。
// 永不返回 error：失败封装为 degraded 结果（M14 ⑥ 非阻断，主流程已成功）。
func (s *Service) GraphragIngest(ctx context.Context, kbID string, chunks []*store.KnowledgeChunk) *store.GraphragInfo {
	items := make([]map[string]any, 0, len(chunks))
	for _, c := range chunks {
		items = append(items, map[string]any{"id": c.ID, "doc_id": c.DocID, "seq": c.Seq, "content": c.Content})
	}
	var out struct {
		Method        string   `json:"method"`
		Chunks        int      `json:"chunks"`
		Entities      int      `json:"entities"`
		Relationships int      `json:"relationships"`
		Warnings      []string `json:"warnings"`
	}
	if err := workerPost(ctx, "/graphrag/ingest", map[string]any{"kb_id": kbID, "chunks": items}, &out); err != nil {
		log.Printf("[kb] graphrag ingest degraded (kb=%s): %v", kbID, err)
		return &store.GraphragInfo{Degraded: true, Error: "semantica worker 不可达，KG 抽取已降级跳过: " + err.Error()}
	}
	log.Printf("[kb] graphrag ingest ok (kb=%s): method=%s entities=%d rels=%d", kbID, out.Method, out.Entities, out.Relationships)
	return &store.GraphragInfo{OK: true, Method: out.Method, Chunks: out.Chunks, Entities: out.Entities, Relationships: out.Relationships, Warnings: out.Warnings}
}

// GraphragQuery worker /query 检索（M14 ③），claims → RetrievalHit。
func (s *Service) GraphragQuery(ctx context.Context, k *store.KnowledgeBase, query string, maxResults int) ([]RetrievalHit, error) {
	if query == "" {
		return nil, nil
	}
	if maxResults <= 0 {
		maxResults = k.TopK
	}
	if maxResults <= 0 {
		maxResults = 4
	}
	var out struct {
		Claims []struct {
			Text       string   `json:"text"`
			SourceNode *string  `json:"source_node"`
			Score      *float64 `json:"score"`
		} `json:"claims"`
	}
	if err := workerPost(ctx, "/query", map[string]any{"q": query, "max_results": maxResults}, &out); err != nil {
		return nil, err
	}
	hits := make([]RetrievalHit, 0, len(out.Claims))
	for i, c := range out.Claims {
		name := k.Name + " · KG"
		if c.SourceNode != nil && *c.SourceNode != "" {
			name = k.Name + " · " + *c.SourceNode
		}
		score := 0.0
		if c.Score != nil {
			score = *c.Score
		}
		hits = append(hits, RetrievalHit{Doc: name, Seq: i, Score: score, Excerpt: truncateRunes(c.Text, 200)})
	}
	// score 可能为 0（worker 未回传），按文本长度稳定排序仅保证确定性，不改变 worker 顺序
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Seq < hits[j].Seq })
	return hits, nil
}

// GraphragQueryWithFallback M14 ⑥：graphrag 检索失败 → 回退向量检索，返回 (hits, mode, degraded, err)。
func (s *Service) GraphragQueryWithFallback(ctx context.Context, k *store.KnowledgeBase, query string, maxResults int, minScore float64) ([]RetrievalHit, string, bool, error) {
	if k.Mode != "graphrag" {
		hits, err := s.Search(ctx, k, query, maxResults, minScore)
		return hits, "rag", false, err
	}
	hits, err := s.GraphragQuery(ctx, k, query, maxResults)
	if err == nil {
		return hits, "graphrag", false, nil
	}
	log.Printf("[kb] graphrag query degraded (kb=%s): %v → 回退向量检索", k.ID, err)
	fb, ferr := s.Search(ctx, k, query, maxResults, minScore)
	return fb, "rag", true, ferr
}
