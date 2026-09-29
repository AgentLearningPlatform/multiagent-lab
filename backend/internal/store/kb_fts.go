// KB-10①（M35/37 号方案）：chunk 词法索引——SQLite FTS5 trigram 分词（CJK 友好）。
// 惰性建表 + 存量回填：modernc sqlite 内建 FTS5，但创建失败（编译裁剪等）不阻断启动，
// 词法臂自动退役（混合检索退化为纯向量，语义与历史一致）。
// FTS 行为辅助索引：写失败仅告警；检索经 chunk_id 回连 knowledge_chunk，残留行自动失效。
package store

import (
	"log"
	"strings"
)

// ChunkFTSReady 惰性探测 FTS5 可用性并确保虚拟表与存量回填（进程内一次）。
func (s *Store) ChunkFTSReady() bool {
	if s == nil || s.DB == nil {
		return false
	}
	s.ftsOnce.Do(func() {
		if _, err := s.DB.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS chunk_fts USING fts5(content, chunk_id UNINDEXED, doc_id UNINDEXED, kb_id UNINDEXED, tokenize='trigram')`); err != nil {
			log.Printf("[store] FTS5 不可用，混合检索词法臂退役（纯向量照常）: %v", err)
			return
		}
		// 存量 chunks 一次性回填（建表前入库的旧数据；幂等——已在 FTS 的 chunk 跳过）
		if _, err := s.DB.Exec(`INSERT INTO chunk_fts(content, chunk_id, doc_id, kb_id)
			SELECT content, id, doc_id, kb_id FROM knowledge_chunk
			WHERE id NOT IN (SELECT chunk_id FROM chunk_fts)`); err != nil {
			log.Printf("[store] chunk_fts 存量回填失败（词法覆盖不全，重建索引可修复）: %v", err)
		}
		s.ftsOK = true
	})
	return s.ftsOK
}

// FTSHit 词法检索命中（经 knowledge_chunk 回连补全 doc/seq/content）。
type FTSHit struct {
	ChunkID string
	DocID   string
	Seq     int
	Content string
	Score   float64 // bm25 取负转正：越大越相关
}

// SearchChunksFTS 词法检索（BM25 排序；query 须ftsQuote 转义后传入，<3 rune 由调用方跳过）。
func (s *Store) SearchChunksFTS(kbID, query string, limit int) ([]FTSHit, error) {
	if !s.ChunkFTSReady() || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.DB.Query(`SELECT chunk_fts.chunk_id, knowledge_chunk.doc_id, knowledge_chunk.seq, knowledge_chunk.content, -bm25(chunk_fts)
		FROM chunk_fts JOIN knowledge_chunk ON knowledge_chunk.id = chunk_fts.chunk_id
		WHERE chunk_fts MATCH ? AND chunk_fts.kb_id = ?
		ORDER BY 5 DESC LIMIT ?`, query, kbID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FTSHit{}
	for rows.Next() {
		var h FTSHit
		if err := rows.Scan(&h.ChunkID, &h.DocID, &h.Seq, &h.Content, &h.Score); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// UpsertChunkFTS 批量写词法索引（InsertKnowledgeChunks 提交后调用；失败仅告警不回滚主数据）。
func (s *Store) UpsertChunkFTS(chunks []*KnowledgeChunk) {
	if !s.ChunkFTSReady() || len(chunks) == 0 {
		return
	}
	for _, c := range chunks {
		if _, err := s.DB.Exec(`INSERT INTO chunk_fts(content, chunk_id, doc_id, kb_id) VALUES (?,?,?,?)`,
			c.Content, c.ID, c.DocID, c.KBID); err != nil {
			log.Printf("[store] chunk_fts 写入失败 (chunk=%s): %v", c.ID, err)
			return
		}
	}
}

// DeleteChunkFTSByDoc 按 doc 清理词法索引（chunk 删除路径同步调用）。
func (s *Store) DeleteChunkFTSByDoc(docID string) {
	if !s.ChunkFTSReady() {
		return
	}
	_, _ = s.DB.Exec(`DELETE FROM chunk_fts WHERE doc_id = ?`, docID)
}

// DeleteChunkFTSByKB 按库清理词法索引（删库路径同步调用）。
func (s *Store) DeleteChunkFTSByKB(kbID string) {
	if !s.ChunkFTSReady() {
		return
	}
	_, _ = s.DB.Exec(`DELETE FROM chunk_fts WHERE kb_id = ?`, kbID)
}

// FTSQuote 将用户查询转为 FTS5 短语字面量（防 MATCH 语法注入；trigram 下即子串匹配）。
func FTSQuote(query string) string {
	return `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
}

// FTSMatchQuery 自然语言问句 → FTS5 MATCH 表达式（KB-10①：trigram 下整句短语=逐字子串匹配，
// 问句几乎必失配；改为 OR 连接的关键词短语组——ASCII/数字词取整词（≥2 字符），CJK 连续段取
// 3 字滑窗（步长 2，每段 ≤6 窗）；任一命中即出榜，bm25 按命中数兜排序。上限 16 短语防表达式爆炸）。
func FTSMatchQuery(question string) string {
	// 按空白与常用标点切分
	splitFn := func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '，', '。', '？', '！', '、', '：', '；', ',', '.', '?', '!', ':', ';', '（', '）', '(', ')', '\u201c', '\u201d', '\u2018', '\u2019':
			return true
		}
		return false
	}
	terms := []string{}
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] && len(terms) < 16 {
			seen[s] = true
			terms = append(terms, s)
		}
	}
	for _, field := range strings.FieldsFunc(question, splitFn) {
		rs := []rune(field)
		if len(rs) == 0 {
			continue
		}
		isASCII := true
		for _, r := range rs {
			if r > 127 {
				isASCII = false
				break
			}
		}
		if isASCII {
			if len(rs) >= 2 { // 编号/术语类：BUG-1024、HNSW
				add(field)
			}
			continue
		}
		// CJK 段：3 字滑窗步长 2
		if len(rs) < 3 {
			continue
		}
		windows := 0
		for i := 0; i+3 <= len(rs) && windows < 6; i, windows = i+2, windows+1 {
			add(string(rs[i : i+3]))
		}
	}
	if len(terms) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		quoted = append(quoted, FTSQuote(t))
	}
	return strings.Join(quoted, " OR ")
}
