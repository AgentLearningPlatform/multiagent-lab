package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---- 知识库（M6，方案 §5.2/§6.9）----

// KnowledgeBase 知识库。
type KnowledgeBase struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	TopK        int     `json:"top_k"`
	MinScore    float64 `json:"min_score"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// KnowledgeDoc 知识文档（索引状态机：pending→indexing→success|failed）。
type KnowledgeDoc struct {
	ID         string `json:"id"`
	KBID       string `json:"kb_id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	ChunkCount int    `json:"chunk_count"`
	Error      string `json:"error"`
	Source     string `json:"source"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// KnowledgeChunk 知识片段。
type KnowledgeChunk struct {
	ID      string `json:"id"`
	KBID    string `json:"kb_id"`
	DocID   string `json:"doc_id"`
	Seq     int    `json:"seq"`
	Content string `json:"content"`
	// Vector 仅 SQLite fallback 路径有值（float32 小端 BLOB）
	Vector       []byte `json:"-"`
	VectorRef    string `json:"vector_ref,omitempty"` // Qdrant point id
	StoreBackend string `json:"store_backend"`
	CreatedAt    string `json:"created_at"`
}

const kbCols = `id,name,description,top_k,min_score,created_at,updated_at`
const kdocCols = `id,kb_id,title,status,chunk_count,error,source,created_at,updated_at`
const kchunkCols = `id,kb_id,doc_id,seq,content,vector,vector_ref,store_backend,created_at`

func scanKB(row interface{ Scan(...any) error }) (*KnowledgeBase, error) {
	var k KnowledgeBase
	if err := row.Scan(&k.ID, &k.Name, &k.Description, &k.TopK, &k.MinScore, &k.CreatedAt, &k.UpdatedAt); err != nil {
		return nil, err
	}
	return &k, nil
}

func scanKDoc(row interface{ Scan(...any) error }) (*KnowledgeDoc, error) {
	var d KnowledgeDoc
	if err := row.Scan(&d.ID, &d.KBID, &d.Title, &d.Status, &d.ChunkCount, &d.Error, &d.Source, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

func scanKChunk(row interface{ Scan(...any) error }) (*KnowledgeChunk, error) {
	var c KnowledgeChunk
	if err := row.Scan(&c.ID, &c.KBID, &c.DocID, &c.Seq, &c.Content, &c.Vector, &c.VectorRef, &c.StoreBackend, &c.CreatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

// ListKnowledgeBases 全部知识库（按创建时间升序）。
func (s *Store) ListKnowledgeBases() ([]*KnowledgeBase, error) {
	rows, err := s.DB.Query(`SELECT ` + kbCols + ` FROM knowledge_base ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*KnowledgeBase
	for rows.Next() {
		k, err := scanKB(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// GetKnowledgeBase 按 ID 查询。
func (s *Store) GetKnowledgeBase(id string) (*KnowledgeBase, error) {
	k, err := scanKB(s.DB.QueryRow(`SELECT `+kbCols+` FROM knowledge_base WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return k, err
}

// CreateKnowledgeBase 新建（name 唯一冲突 ErrConflict）。
func (s *Store) CreateKnowledgeBase(k *KnowledgeBase) (*KnowledgeBase, error) {
	if strings.TrimSpace(k.Name) == "" {
		return nil, &HTTPError{Status: 400, Msg: "name is required"}
	}
	if k.ID == "" {
		k.ID = NewID()
	}
	if k.TopK <= 0 {
		k.TopK = 4
	}
	_, err := s.DB.Exec(`INSERT INTO knowledge_base (`+kbCols+`) VALUES (?,?,?,?,?,?,?)`,
		k.ID, k.Name, k.Description, k.TopK, k.MinScore, now(), now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrConflict
		}
		return nil, err
	}
	return s.GetKnowledgeBase(k.ID)
}

// UpdateKnowledgeBase 全量更新。
func (s *Store) UpdateKnowledgeBase(k *KnowledgeBase) (*KnowledgeBase, error) {
	res, err := s.DB.Exec(`UPDATE knowledge_base SET name=?,description=?,top_k=?,min_score=?,updated_at=? WHERE id=?`,
		k.Name, k.Description, k.TopK, k.MinScore, now(), k.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrConflict
		}
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetKnowledgeBase(k.ID)
}

// DeleteKnowledgeBase 级联删除（chunks/docs 由调用方先清理向量库后调用）。
func (s *Store) DeleteKnowledgeBase(id string) error {
	res, err := s.DB.Exec(`DELETE FROM knowledge_base WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.DB.Exec(`DELETE FROM knowledge_chunk WHERE kb_id = ?`, id)
	s.DB.Exec(`DELETE FROM knowledge_doc WHERE kb_id = ?`, id)
	return nil
}

// ---- Docs ----

// ListKnowledgeDocs 库内文档（按创建时间升序）。
func (s *Store) ListKnowledgeDocs(kbID string) ([]*KnowledgeDoc, error) {
	rows, err := s.DB.Query(`SELECT `+kdocCols+` FROM knowledge_doc WHERE kb_id = ? ORDER BY created_at, id`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*KnowledgeDoc
	for rows.Next() {
		d, err := scanKDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetKnowledgeDoc 单文档。
func (s *Store) GetKnowledgeDoc(id string) (*KnowledgeDoc, error) {
	d, err := scanKDoc(s.DB.QueryRow(`SELECT `+kdocCols+` FROM knowledge_doc WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

// CreateKnowledgeDoc 新建文档（初始 pending）。
func (s *Store) CreateKnowledgeDoc(d *KnowledgeDoc) (*KnowledgeDoc, error) {
	if d.ID == "" {
		d.ID = NewID()
	}
	if d.Status == "" {
		d.Status = "pending"
	}
	if d.Source == "" {
		d.Source = "paste"
	}
	_, err := s.DB.Exec(`INSERT INTO knowledge_doc (`+kdocCols+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		d.ID, d.KBID, d.Title, d.Status, d.ChunkCount, d.Error, d.Source, now(), now())
	if err != nil {
		return nil, err
	}
	return s.GetKnowledgeDoc(d.ID)
}

// UpdateKnowledgeDocStatus 索引状态回写（indexing/success/failed + chunk_count/error）。
func (s *Store) UpdateKnowledgeDocStatus(id, status string, chunkCount int, errMsg string) error {
	_, err := s.DB.Exec(`UPDATE knowledge_doc SET status=?,chunk_count=?,error=?,updated_at=? WHERE id=?`,
		status, chunkCount, errMsg, now(), id)
	return err
}

// DeleteKnowledgeDoc 删除文档及其 chunks（向量库清理由调用方先行）。
func (s *Store) DeleteKnowledgeDoc(kbID, docID string) error {
	res, err := s.DB.Exec(`DELETE FROM knowledge_doc WHERE id = ? AND kb_id = ?`, docID, kbID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.DB.Exec(`DELETE FROM knowledge_chunk WHERE doc_id = ?`, docID)
	return nil
}

// ---- Chunks ----

// InsertKnowledgeChunks 批量写入 chunks（事务）。
// Qdrant 路径：vector 为 nil、vectorRef=point id、backend=qdrant；
// SQLite 路径：vector=BLOB、backend=sqlite。
func (s *Store) InsertKnowledgeChunks(chunks []*KnowledgeChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO knowledge_chunk (` + kchunkCols + `) VALUES (?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, c := range chunks {
		if c.ID == "" {
			c.ID = NewID()
		}
		if c.CreatedAt == "" {
			c.CreatedAt = now().UTC().Format(time.RFC3339)
		}
		if _, err := stmt.Exec(c.ID, c.KBID, c.DocID, c.Seq, c.Content, c.Vector, c.VectorRef, c.StoreBackend, c.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListKnowledgeChunksByDoc 按 doc 取 chunks（重索引前清理/调试用）。
func (s *Store) ListKnowledgeChunksByDoc(docID string) ([]*KnowledgeChunk, error) {
	rows, err := s.DB.Query(`SELECT `+kchunkCols+` FROM knowledge_chunk WHERE doc_id = ? ORDER BY seq`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*KnowledgeChunk
	for rows.Next() {
		c, err := scanKChunk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteKnowledgeChunksByDoc 删除文档全部 chunks。
func (s *Store) DeleteKnowledgeChunksByDoc(docID string) error {
	_, err := s.DB.Exec(`DELETE FROM knowledge_chunk WHERE doc_id = ?`, docID)
	return err
}

// DocTitles 批量取文档标题（retrieval 事件 hits[].doc 需要）。
func (s *Store) DocTitles(ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	q := `SELECT id,title FROM knowledge_doc WHERE id IN (` + strings.TrimRight(strings.Repeat("?,", len(ids)), ",") + `)`
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		out[id] = title
	}
	return out, rows.Err()
}

// SearchChunksSQLite SQLite fallback 余弦 TopK（§6.9：无外部依赖对照实现）。
// 返回带 Content/DocID/Seq/Vector 的 chunks，由 kb 层计算相似度。
func (s *Store) SearchChunksSQLite(kbID string) ([]*KnowledgeChunk, error) {
	rows, err := s.DB.Query(`SELECT `+kchunkCols+` FROM knowledge_chunk WHERE kb_id = ? AND vector IS NOT NULL ORDER BY doc_id, seq`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*KnowledgeChunk
	for rows.Next() {
		c, err := scanKChunk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarshalJSON 调试辅助（避免 chunk 内容转 JSON 时带二进制）。
func (c *KnowledgeChunk) MarshalChunkMeta() string {
	b, _ := json.Marshal(map[string]any{"id": c.ID, "doc_id": c.DocID, "seq": c.Seq, "backend": c.StoreBackend})
	return string(b)
}

var _ = fmt.Sprintf
