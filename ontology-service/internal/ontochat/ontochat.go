// Package ontochat OntoChat 多轮引导会话存储（REQ-103 模式 A）。
// 会话状态机：cq（列能力问题）→ domain（逐轮补全领域信息）→ draft（生成 spec 草稿）
// → refine（校验错误回喂修正）→ done（已入库）。消息与累积上下文整体存 JSON 列，
// 无新服务、无新依赖——与 REQ-103 "复用 /api/ontology-llm/generate" 口径一致。
package ontochat

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/repo"
)

var ErrNotFound = repo.ErrNotFound

// Message 单条对话消息（全量留痕，前端直接渲染）。
type Message struct {
	Role    string `json:"role"` // user | assistant | system
	Content string `json:"content"`
	TS      string `json:"ts"`
}

// Context 累积上下文（每轮追加，draft 阶段整体喂给生成器）。
type Context struct {
	Description string      `json:"description"`            // 领域描述（cq 阶段录入）
	CQs         []string    `json:"cqs,omitempty"`          // 能力问题列表
	Hints       []string    `json:"hints,omitempty"`        // 逐轮补全的领域信息
	DraftSpec   *json.RawMessage `json:"draft_spec,omitempty"` // 最近一次草稿（refine 阶段回喂）
}

// Session 会话聚合。
type Session struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Stage      string    `json:"stage"`
	Round      int       `json:"round"`
	Messages   []Message `json:"messages"`
	Context    Context   `json:"context"`
	OntologyID string    `json:"ontology_id,omitempty"`
	CreatedAt  string    `json:"created_at"`
	UpdatedAt  string    `json:"updated_at"`
}

// Store SQLite 存储（复用 repo.Store 的连接）。
type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store { return &Store{db: db} }

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// Create 新建会话（stage=cq）。
func (s *Store) Create(id, title string) (*Session, error) {
	ts := now()
	_, err := s.db.Exec(
		`INSERT INTO ontochat_session (id, title, stage, round, messages, context_json, created_at, updated_at)
		 VALUES (?, ?, 'cq', 0, '[]', '{}', ?, ?)`, id, title, ts, ts)
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Get 读取会话；不存在 → repo.ErrNotFound。
func (s *Store) Get(id string) (*Session, error) {
	row := s.db.QueryRow(
		`SELECT id, title, stage, round, messages, context_json, ontology_id, created_at, updated_at
		 FROM ontochat_session WHERE id = ?`, id)
	var sess Session
	var rawMsgs, rawCtx, ontoID sql.NullString
	if err := row.Scan(&sess.ID, &sess.Title, &sess.Stage, &sess.Round, &rawMsgs, &rawCtx, &ontoID, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(rawMsgs.String), &sess.Messages); err != nil {
		sess.Messages = nil
	}
	if err := json.Unmarshal([]byte(rawCtx.String), &sess.Context); err != nil {
		sess.Context = Context{}
	}
	sess.OntologyID = ontoID.String
	return &sess, nil
}

// List 会话列表（按更新时间倒序；不含消息体，列表页轻量）。
func (s *Store) List() ([]Session, error) {
	rows, err := s.db.Query(
		`SELECT id, title, stage, round, ontology_id, created_at, updated_at
		 FROM ontochat_session ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var sess Session
		var ontoID sql.NullString
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.Stage, &sess.Round, &ontoID, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, err
		}
		sess.OntologyID = ontoID.String
		out = append(out, sess)
	}
	return out, rows.Err()
}

// Append 追加消息并更新阶段/轮数/上下文（整体覆写 messages/context_json，量级小无需增量）。
func (s *Store) Append(id string, msg Message, stage *string, round *int, ctx *Context) error {
	sess, err := s.Get(id)
	if err != nil {
		return err
	}
	if msg.TS == "" {
		msg.TS = now()
	}
	sess.Messages = append(sess.Messages, msg)
	if len(sess.Messages) > 200 { // 留痕上限：超出截断最旧（防长会话膨胀）
		sess.Messages = sess.Messages[len(sess.Messages)-200:]
	}
	if stage != nil {
		sess.Stage = *stage
	}
	if round != nil {
		sess.Round = *round
	}
	if ctx != nil {
		sess.Context = *ctx
	}
	rawMsgs, _ := json.Marshal(sess.Messages)
	rawCtx, _ := json.Marshal(sess.Context)
	ts := now()
	_, err = s.db.Exec(
		`UPDATE ontochat_session SET messages = ?, context_json = ?, stage = ?, round = ?, updated_at = ? WHERE id = ?`,
		string(rawMsgs), string(rawCtx), sess.Stage, sess.Round, ts, id)
	return err
}

// BindOntology 入库后回填产物 id 并置 done。
func (s *Store) BindOntology(id, ontologyID string) error {
	ts := now()
	_, err := s.db.Exec(
		`UPDATE ontochat_session SET ontology_id = ?, stage = 'done', updated_at = ? WHERE id = ?`,
		ontologyID, ts, id)
	return err
}

// Delete 删除会话。
func (s *Store) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM ontochat_session WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
