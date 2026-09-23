package store

import (
	"database/sql"
	"errors"
)

func scanConversation(row interface{ Scan(...any) error }) (*Conversation, error) {
	var c Conversation
	var agentID, projectID, kbID, profileID sql.NullString
	var enableKB, ontoEnabled, enableSkills int
	err := row.Scan(&c.ID, &c.Scope, &agentID, &projectID, &c.Title, &kbID, &enableKB, &profileID, &ontoEnabled, &c.TopK, &c.MinScore, &c.CreatedAt, &c.UpdatedAt, &enableSkills, &c.InterruptState)
	if err != nil {
		return nil, err
	}
	if agentID.Valid {
		c.AgentID = &agentID.String
	}
	if projectID.Valid {
		c.ProjectID = &projectID.String
	}
	if kbID.Valid {
		c.KBID = &kbID.String
	}
	if profileID.Valid {
		c.RuntimeProfileID = &profileID.String
	}
	c.EnableKB = enableKB == 1
	c.OntologyEnabled = ontoEnabled == 1
	es := enableSkills == 1
	c.EnableSkills = &es
	return &c, nil
}

const convCols = `id,scope,agent_id,project_id,title,kb_id,enable_kb,runtime_profile_id,ontology_enabled,top_k,min_score,created_at,updated_at,enable_skills,interrupt_state`

// ConversationFilter 会话列表过滤。
type ConversationFilter struct {
	Scope     string
	AgentID   string
	ProjectID string
}

// ListConversations 按过滤条件列出对话。
func (s *Store) ListConversations(f ConversationFilter) ([]*Conversation, error) {
	q := `SELECT ` + convCols + ` FROM conversation WHERE 1=1`
	var args []any
	if f.Scope != "" {
		q += ` AND scope = ?`
		args = append(args, f.Scope)
	}
	if f.AgentID != "" {
		q += ` AND agent_id = ?`
		args = append(args, f.AgentID)
	}
	if f.ProjectID != "" {
		q += ` AND project_id = ?`
		args = append(args, f.ProjectID)
	}
	q += ` ORDER BY updated_at DESC, id`
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetConversation 按 ID 查询。
func (s *Store) GetConversation(id string) (*Conversation, error) {
	row := s.DB.QueryRow(`SELECT `+convCols+` FROM conversation WHERE id = ?`, id)
	c, err := scanConversation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// CreateConversation 新建对话；校验 scope 与归属。
func (s *Store) CreateConversation(c *Conversation) (*Conversation, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	if c.Scope != "agent" && c.Scope != "project" {
		return nil, &HTTPError{Status: 400, Msg: "scope must be agent|project"}
	}
	if c.Scope == "agent" && (c.AgentID == nil || *c.AgentID == "") {
		return nil, &HTTPError{Status: 400, Msg: "agent_id required for scope=agent"}
	}
	if c.Scope == "project" && (c.ProjectID == nil || *c.ProjectID == "") {
		return nil, &HTTPError{Status: 400, Msg: "project_id required for scope=project"}
	}
	if c.Title == "" {
		c.Title = "新对话"
	}
	if c.TopK == 0 {
		c.TopK = 4
	}
	// enable_skills：未指定时默认开启（前端 full-replace PUT 会显式携带）
	enableSkills := true
	if c.EnableSkills != nil {
		enableSkills = *c.EnableSkills
	}
	_, err := s.DB.Exec(`INSERT INTO conversation (`+convCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Scope, c.AgentID, c.ProjectID, c.Title, c.KBID, boolInt(c.EnableKB), c.RuntimeProfileID, boolInt(c.OntologyEnabled), c.TopK, c.MinScore, now(), now(), boolInt(enableSkills), c.InterruptState)
	if err != nil {
		return nil, err
	}
	return s.GetConversation(c.ID)
}

// UpdateConversation 更新标题与对话级配置（kb / 本体运行方案 / 技能开关）。
func (s *Store) UpdateConversation(c *Conversation) (*Conversation, error) {
	// enable_skills 为 nil 时保留原值（COALESCE），非 nil 时覆盖
	var enableArg any
	if c.EnableSkills != nil {
		enableArg = boolInt(*c.EnableSkills)
	}
	res, err := s.DB.Exec(`UPDATE conversation SET title=?,kb_id=?,enable_kb=?,runtime_profile_id=?,ontology_enabled=?,top_k=?,min_score=?,enable_skills=COALESCE(?,enable_skills),updated_at=? WHERE id=?`,
		c.Title, c.KBID, boolInt(c.EnableKB), c.RuntimeProfileID, boolInt(c.OntologyEnabled), c.TopK, c.MinScore, enableArg, now(), c.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetConversation(c.ID)
}

// DeleteConversation 删除对话（级联消息与事件）。
func (s *Store) DeleteConversation(id string) error {
	res, err := s.DB.Exec(`DELETE FROM conversation WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetConversationInterruptState 更新会话中断挂起状态（"" = 清除；M11 收尾中断恢复）。
func (s *Store) SetConversationInterruptState(id, stateJSON string) error {
	res, err := s.DB.Exec(`UPDATE conversation SET interrupt_state=?,updated_at=? WHERE id=?`, stateJSON, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
