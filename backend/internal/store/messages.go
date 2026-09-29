package store

import "database/sql"

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
	rows, err := s.DB.Query(`SELECT id,conversation_id,run_id,type,data,created_at FROM run_event WHERE conversation_id = ? AND type IN ('tool.call','tool.result') ORDER BY created_at, id`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RunEvent
	for rows.Next() {
		var e RunEvent
		var data sql.NullString
		if err := rows.Scan(&e.ID, &e.ConversationID, &e.RunID, &e.Type, &data, &e.CreatedAt); err != nil {
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
	_, err := s.DB.Exec(`INSERT INTO run_event (id,conversation_id,run_id,type,data,created_at) VALUES (?,?,?,?,?,?)`,
		e.ID, e.ConversationID, e.RunID, e.Type, e.Data, now())
	if err != nil {
		return nil, err
	}
	return e, nil
}

// ListEvents 按时间升序返回对话全部运行事件（历史还原事件时间线）。
func (s *Store) ListEvents(convID string) ([]*RunEvent, error) {
	rows, err := s.DB.Query(`SELECT id,conversation_id,run_id,type,data,created_at FROM run_event WHERE conversation_id = ? ORDER BY created_at, id`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RunEvent
	for rows.Next() {
		var e RunEvent
		var data sql.NullString
		if err := rows.Scan(&e.ID, &e.ConversationID, &e.RunID, &e.Type, &data, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Data = data.String
		out = append(out, &e)
	}
	return out, rows.Err()
}

// TouchConversation 刷新对话更新时间。
func (s *Store) TouchConversation(convID string) error {
	_, err := s.DB.Exec(`UPDATE conversation SET updated_at = ? WHERE id = ?`, now(), convID)
	return err
}
