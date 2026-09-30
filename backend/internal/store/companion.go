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
	// REQ-194/M34：抽取时实体对齐标记（aligned=对齐已有实体沿用原名 / new=新造；空=存量未标）
	Aligned string `json:"aligned,omitempty"`
	// REQ-194/M34：审计注记（语义矛盾检测「疑似矛盾待人工」等）
	Note string `json:"note,omitempty"`
}

// CompanionCursor 抽取游标（REQ-211/M44 复合键：会话 × agent——多 agent 共用项目会话
// 时各自追踪抽取位；只读续抽）。
type CompanionCursor struct {
	ConversationID string `json:"conversation_id"`
	AgentID        string `json:"agent_id"`
	LastMessageID  string `json:"last_message_id"`
	UpdatedAt      string `json:"updated_at"`
}

// CompanionGraphSource 图迁移数据源（REQ-211：agent × 会话对——历史候选/游标涉及的图）。
type CompanionGraphSource struct {
	AgentID        string `json:"agent_id"`
	ConversationID string `json:"conversation_id"`
}

const companionCandidateCols = `id,conversation_id,agent_id,kind,name,rel_name,rel_target,definition,confidence,source_message_id,source_excerpt,status,created_at,decided_at,aligned,note`

func scanCandidate(row interface{ Scan(...any) error }) (*CompanionCandidate, error) {
	var c CompanionCandidate
	var relName, relTarget, def, excerpt, decided, aligned, note sql.NullString
	var conf sql.NullFloat64
	err := row.Scan(&c.ID, &c.ConversationID, &c.AgentID, &c.Kind, &c.Name, &relName, &relTarget, &def, &conf, &c.SourceMessageID, &excerpt, &c.Status, &c.CreatedAt, &decided, &aligned, &note)
	if err != nil {
		return nil, err
	}
	c.RelName, c.RelTarget, c.Definition, c.SourceExcerpt = relName.String, relTarget.String, def.String, excerpt.String
	c.Aligned, c.Note = aligned.String, note.String
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
		if _, err := tx.Exec(`INSERT INTO companion_candidate (`+companionCandidateCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			c.ID, c.ConversationID, c.AgentID, c.Kind, c.Name, c.RelName, c.RelTarget, c.Definition, c.Confidence, c.SourceMessageID, c.SourceExcerpt, c.Status, now(), nil, c.Aligned, c.Note); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListCompanionCandidates 候选列表（可按会话/智能体/状态过滤；时间升序）。
// REQ-193/M33：增 agent 维度可选过滤（表已有 agent_id 列）——伴生管理铺平视图跨会话
// 单列表按 agent 拉取，会话归属在行内标注。
func (s *Store) ListCompanionCandidates(convID, agentID, status string) ([]*CompanionCandidate, error) {
	q := `SELECT ` + companionCandidateCols + ` FROM companion_candidate WHERE 1=1`
	var args []any
	if convID != "" {
		q += ` AND conversation_id = ?`
		args = append(args, convID)
	}
	if agentID != "" {
		q += ` AND agent_id = ?`
		args = append(args, agentID)
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

// SetCompanionCandidateNote 审计注记回写（REQ-194⑤语义矛盾检测：疑似矛盾待人工等；仅 pending）。
func (s *Store) SetCompanionCandidateNote(id, note string) error {
	res, err := s.DB.Exec(`UPDATE companion_candidate SET note=? WHERE id=? AND status='pending'`, truncateNote(note), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// truncateNote note 落库截断（审计摘要非全文，500 字符足够）。
func truncateNote(s string) string {
	r := []rune(s)
	if len(r) > 500 {
		return string(r[:500])
	}
	return s
}

// DeleteConversationCompanionData 会话级清理（清该会话全部候选与游标；图面 DROP 由伴生模块执行）。
func (s *Store) DeleteConversationCompanionData(convID string) error {
	if _, err := s.DB.Exec(`DELETE FROM companion_candidate WHERE conversation_id = ?`, convID); err != nil {
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM companion_cursor WHERE conversation_id = ?`, convID)
	return err
}

// DeleteAgentCompanionData agent 级整体摘除数据面（REQ-211：清该 agent 全部候选与游标；
// agent 图 DROP 由伴生模块执行）。
func (s *Store) DeleteAgentCompanionData(agentID string) error {
	if _, err := s.DB.Exec(`DELETE FROM companion_candidate WHERE agent_id = ?`, agentID); err != nil {
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM companion_cursor WHERE agent_id = ?`, agentID)
	return err
}

// ListCompanionGraphSources 图迁移数据源（REQ-211 启动迁移）：历史候选/游标涉及的
// agent × 会话对（agent 图聚合其全部会话图的迁移清单）。
func (s *Store) ListCompanionGraphSources() ([]CompanionGraphSource, error) {
	rows, err := s.DB.Query(`
		SELECT DISTINCT agent_id, conversation_id FROM companion_candidate WHERE agent_id != ''
		UNION
		SELECT DISTINCT agent_id, conversation_id FROM companion_cursor WHERE agent_id != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CompanionGraphSource
	for rows.Next() {
		var g CompanionGraphSource
		if err := rows.Scan(&g.AgentID, &g.ConversationID); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// CountCompanionCursors agent 在抽会话数（agent 视角状态卡呈现）。
func (s *Store) CountCompanionCursors(agentID string) (int, error) {
	row := s.DB.QueryRow(`SELECT count(*) FROM companion_cursor WHERE agent_id = ? AND last_message_id != ''`, agentID)
	var n int
	err := row.Scan(&n)
	return n, err
}

// GetCompanionMeta 伴生模块元数据（迁移完成标记等；无记录返回空）。
func (s *Store) GetCompanionMeta(key string) (string, error) {
	var v string
	err := s.DB.QueryRow(`SELECT value FROM companion_meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetCompanionMeta 写伴生模块元数据（幂等 upsert）。
func (s *Store) SetCompanionMeta(key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO companion_meta (key,value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// GetCompanionCursor 读抽取游标（复合键 会话×agent；无记录返回空游标）。
func (s *Store) GetCompanionCursor(convID, agentID string) (*CompanionCursor, error) {
	row := s.DB.QueryRow(`SELECT conversation_id,agent_id,last_message_id,updated_at FROM companion_cursor WHERE conversation_id = ? AND agent_id = ?`, convID, agentID)
	var c CompanionCursor
	var last sql.NullString
	if err := row.Scan(&c.ConversationID, &c.AgentID, &last, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &CompanionCursor{ConversationID: convID, AgentID: agentID}, nil
		}
		return nil, err
	}
	c.LastMessageID = last.String
	return &c, nil
}

// AdvanceCompanionCursor 游标推进（复合键幂等 upsert）。
func (s *Store) AdvanceCompanionCursor(convID, agentID, lastMessageID string) error {
	_, err := s.DB.Exec(`INSERT INTO companion_cursor (conversation_id,agent_id,last_message_id,updated_at) VALUES (?,?,?,?)
		ON CONFLICT(conversation_id,agent_id) DO UPDATE SET last_message_id=excluded.last_message_id, updated_at=excluded.updated_at`,
		convID, agentID, lastMessageID, now())
	return err
}
