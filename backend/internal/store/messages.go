package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// 消息与运行事件持久化（历史还原 / 事件时间线）。

// InsertMessage 保存消息。
func (s *Store) InsertMessage(m *Message) (*Message, error) {
	if m.ID == "" {
		m.ID = NewID()
	}
	_, err := s.DB.Exec(`INSERT INTO message (id,conversation_id,role,content,meta,created_at) VALUES (?,?,?,?,?,?)`,
		m.ID, m.ConversationID, m.Role, m.Content, m.Meta, now())
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ListMessages 按时间升序返回对话全部消息（历史还原）。
func (s *Store) ListMessages(convID string) ([]*Message, error) {
	rows, err := s.DB.Query(`SELECT id,conversation_id,role,content,meta,created_at FROM message WHERE conversation_id = ? ORDER BY created_at, id`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		var m Message
		var meta sql.NullString
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &meta, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Meta = meta.String
		out = append(out, &m)
	}
	return out, rows.Err()
}

// ListToolEvents 返回对话的工具调用/结果事件（REQ-201 A1：tool 轮次由 run_event 派生重建，
// 零 schema 变更——历史重建恢复 assistant ToolCalls 与 tool 结果消息）。
func (s *Store) ListToolEvents(convID string) ([]*RunEvent, error) {
	rows, err := s.DB.Query(`SELECT id,conversation_id,run_id,type,data,created_at,COALESCE(schema_version,1) FROM run_event WHERE conversation_id = ? AND type IN ('tool.call','tool.result') ORDER BY created_at, id`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RunEvent
	for rows.Next() {
		var e RunEvent
		var data sql.NullString
		if err := rows.Scan(&e.ID, &e.ConversationID, &e.RunID, &e.Type, &data, &e.CreatedAt, &e.SchemaVersion); err != nil {
			return nil, err
		}
		e.Data = data.String
		out = append(out, &e)
	}
	return out, rows.Err()
}

// CountMessages 统计对话消息数。
func (s *Store) CountMessages(convID string) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM message WHERE conversation_id = ?`, convID).Scan(&n)
	return n, err
}

// InsertEvent 保存运行事件。
func (s *Store) InsertEvent(e *RunEvent) (*RunEvent, error) {
	if e.ID == "" {
		e.ID = NewID()
	}
	sv := e.SchemaVersion
	if sv == 0 {
		sv = 1 // 存量兼容：调用方未显式盖版本按旧契约计
	}
	_, err := s.DB.Exec(`INSERT INTO run_event (id,conversation_id,run_id,type,data,created_at,schema_version) VALUES (?,?,?,?,?,?,?)`,
		e.ID, e.ConversationID, e.RunID, e.Type, e.Data, now(), sv)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// ListEvents 按时间升序返回对话全部运行事件（历史还原事件时间线）。
func (s *Store) ListEvents(convID string) ([]*RunEvent, error) {
	rows, err := s.DB.Query(`SELECT id,conversation_id,run_id,type,data,created_at,COALESCE(schema_version,1) FROM run_event WHERE conversation_id = ? ORDER BY created_at, id`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RunEvent
	for rows.Next() {
		var e RunEvent
		var data sql.NullString
		if err := rows.Scan(&e.ID, &e.ConversationID, &e.RunID, &e.Type, &data, &e.CreatedAt, &e.SchemaVersion); err != nil {
			return nil, err
		}
		e.Data = data.String
		out = append(out, &e)
	}
	return out, rows.Err()
}

// EventQuery 事件查询过滤（REQ-217③：run_id/type/limit/offset；零值字段不参与过滤）。
type EventQuery struct {
	RunID  string // 按运行过滤（轨迹面板「重放此运行」/单运行视图）
	Type   string // 精确类型过滤（tool.call/tool.result/…）
	Limit  int    // ≤0 = 不限
	Offset int    // ≥0，配 Limit 分页
}

// ListEventsQ 过滤版事件查询（升序不变；返回命中总数供分页——limit 生效时 out 可能是总数的前窗）。
func (s *Store) ListEventsQ(convID string, q EventQuery) ([]*RunEvent, int, error) {
	where := []string{"conversation_id = ?"}
	args := []any{convID}
	if q.RunID != "" {
		where = append(where, "run_id = ?") // 走 idx_run_event_run（迁移 035）
		args = append(args, q.RunID)
	}
	if q.Type != "" {
		where = append(where, "type = ?")
		args = append(args, q.Type)
	}
	total := 0
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM run_event WHERE `+strings.Join(where, " AND "), args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	sqlq := `SELECT id,conversation_id,run_id,type,data,created_at,COALESCE(schema_version,1) FROM run_event WHERE ` + strings.Join(where, " AND ") + ` ORDER BY created_at, id`
	if q.Limit > 0 {
		sqlq += fmt.Sprintf(" LIMIT %d OFFSET %d", q.Limit, max(q.Offset, 0))
	}
	rows, err := s.DB.Query(sqlq, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*RunEvent
	for rows.Next() {
		var e RunEvent
		var data sql.NullString
		if err := rows.Scan(&e.ID, &e.ConversationID, &e.RunID, &e.Type, &data, &e.CreatedAt, &e.SchemaVersion); err != nil {
			return nil, 0, err
		}
		e.Data = data.String
		out = append(out, &e)
	}
	return out, total, rows.Err()
}
func (s *Store) TouchConversation(convID string) error {
	_, err := s.DB.Exec(`UPDATE conversation SET updated_at = ? WHERE id = ?`, now(), convID)
	return err
}

// ---- REQ-204/M39 C1：中断检查点持久化 ----

// SetCheckpoint 保存中断检查点（gob blob，按 checkpointID 幂等覆盖）。
func (s *Store) SetCheckpoint(id string, blob []byte) error {
	_, err := s.DB.Exec(`INSERT INTO checkpoint (id,blob,created_at) VALUES (?,?,?)
		ON CONFLICT(id) DO UPDATE SET blob=excluded.blob, created_at=excluded.created_at`, id, blob, now())
	return err
}

// GetCheckpoint 取检查点（不存在返回 ok=false）。
func (s *Store) GetCheckpoint(id string) ([]byte, bool, error) {
	var b []byte
	err := s.DB.QueryRow(`SELECT blob FROM checkpoint WHERE id = ?`, id).Scan(&b)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

// DeleteCheckpoint 删除检查点（恢复完成/放弃挂起时清理）。
func (s *Store) DeleteCheckpoint(id string) error {
	_, err := s.DB.Exec(`DELETE FROM checkpoint WHERE id = ?`, id)
	return err
}
