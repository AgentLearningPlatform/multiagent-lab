package store

import (
	"database/sql"
	"errors"
)

// ---------------------------------------------------------------------------
// REQ-170/M28 伴生本体 P1：候选表 + 会话游标的存储访问（旁路数据面，不进对话主链路）。
// 候选 = LLM 抽取产物（pending）；人工确认（confirmed）后由伴生模块写入 Oxigraph 会话图。
// ---------------------------------------------------------------------------

// CompanionCandidate 伴生抽取候选（薄本体三元组原料）。
type CompanionCandidate struct {
	ID              string  `json:"id"`
	ConversationID  string  `json:"conversation_id"`
	AgentID         string  `json:"agent_id"`
	Kind            string  `json:"kind"` // concept | relation | event
	Name            string  `json:"name"` // 概念/事件标签；relation 时为关系动名词名
	RelName         string  `json:"rel_name,omitempty"`
	RelTarget       string  `json:"rel_target,omitempty"`
	Definition      string  `json:"definition,omitempty"`
	Confidence      float64 `json:"confidence"`
	SourceMessageID string  `json:"source_message_id,omitempty"`
	SourceExcerpt   string  `json:"source_excerpt,omitempty"`
	Status          string  `json:"status"` // pending | confirmed | rejected
	CreatedAt       string  `json:"created_at"`
	DecidedAt       string  `json:"decided_at,omitempty"`
}

// CompanionCursor 会话抽取游标：已处理到的最后一条 message id（只读续抽）。
type CompanionCursor struct {
	ConversationID string `json:"conversation_id"`
	LastMessageID  string `json:"last_message_id"`
	UpdatedAt      string `json:"updated_at"`
}

const companionCandidateCols = `id,conversation_id,agent_id,kind,name,rel_name,rel_target,definition,confidence,source_message_id,source_excerpt,status,created_at,decided_at`

func scanCandidate(row interface{ Scan(...any) error }) (*CompanionCandidate, error) {
	var c CompanionCandidate
	var relName, relTarget, def, excerpt, decided sql.NullString
	var conf sql.NullFloat64
	err := row.Scan(&c.ID, &c.ConversationID, &c.AgentID, &c.Kind, &c.Name, &relName, &relTarget, &def, &conf, &c.SourceMessageID, &excerpt, &c.Status, &c.CreatedAt, &decided)
	if err != nil {
		return nil, err
	}
	c.RelName, c.RelTarget, c.Definition, c.SourceExcerpt = relName.String, relTarget.String, def.String, excerpt.String
	if conf.Valid {
		c.Confidence = conf.Float64
	}
	if decided.Valid {
		c.DecidedAt = decided.String
	}
	return &c, nil
}

// CreateCompanionCandidates 批量落 pending 候选（一次抽取一批）。
func (s *Store) CreateCompanionCandidates(cands []*CompanionCandidate) error {
	if len(cands) == 0 {
		return nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range cands {
		if c.ID == "" {
			c.ID = NewID()
		}
		if c.Status == "" {
			c.Status = "pending"
		}
		if _, err := tx.Exec(`INSERT INTO companion_candidate (`+companionCandidateCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			c.ID, c.ConversationID, c.AgentID, c.Kind, c.Name, c.RelName, c.RelTarget, c.Definition, c.Confidence, c.SourceMessageID, c.SourceExcerpt, c.Status, now(), nil); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListCompanionCandidates 候选列表（可按会话与状态过滤；时间升序）。
func (s *Store) ListCompanionCandidates(convID, status string) ([]*CompanionCandidate, error) {
	q := `SELECT ` + companionCandidateCols + ` FROM companion_candidate WHERE 1=1`
	var args []any
	if convID != "" {
		q += ` AND conversation_id = ?`
		args = append(args, convID)
	}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY created_at, id`
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CompanionCandidate
	for rows.Next() {
		c, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCompanionCandidate 按 ID 查询。
func (s *Store) GetCompanionCandidate(id string) (*CompanionCandidate, error) {
	row := s.DB.QueryRow(`SELECT `+companionCandidateCols+` FROM companion_candidate WHERE id = ?`, id)
	c, err := scanCandidate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// DecideCompanionCandidate 候选裁决（confirm/reject），记 decided_at；仅 pending 可裁决。
func (s *Store) DecideCompanionCandidate(id, status string) (*CompanionCandidate, error) {
	if status != "confirmed" && status != "rejected" {
		return nil, ErrConflict
	}
	res, err := s.DB.Exec(`UPDATE companion_candidate SET status=?, decided_at=? WHERE id=? AND status='pending'`, status, now(), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetCompanionCandidate(id)
}

// DeleteConversationCompanionData 会话级整体摘除（低侵入三原则③：清候选 + 清游标；图面 DROP 由伴生模块执行）。
func (s *Store) DeleteConversationCompanionData(convID string) error {
	if _, err := s.DB.Exec(`DELETE FROM companion_candidate WHERE conversation_id = ?`, convID); err != nil {
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM companion_cursor WHERE conversation_id = ?`, convID)
	return err
}

// GetCompanionCursor 读会话游标（无记录返回空游标）。
func (s *Store) GetCompanionCursor(convID string) (*CompanionCursor, error) {
	row := s.DB.QueryRow(`SELECT conversation_id,last_message_id,updated_at FROM companion_cursor WHERE conversation_id = ?`, convID)
	var c CompanionCursor
	var last sql.NullString
	if err := row.Scan(&c.ConversationID, &last, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &CompanionCursor{ConversationID: convID}, nil
		}
		return nil, err
	}
	c.LastMessageID = last.String
	return &c, nil
}

// AdvanceCompanionCursor 游标推进（幂等 upsert）。
func (s *Store) AdvanceCompanionCursor(convID, lastMessageID string) error {
	_, err := s.DB.Exec(`INSERT INTO companion_cursor (conversation_id,last_message_id,updated_at) VALUES (?,?,?)
		ON CONFLICT(conversation_id) DO UPDATE SET last_message_id=excluded.last_message_id, updated_at=excluded.updated_at`,
		convID, lastMessageID, now())
	return err
}
