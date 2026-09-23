// 数据量概览与级联统计（REQ-113② 数据与安全最小版）：设置页展示各表行数，
// 对话/项目列表附级联规模（消息数 / 会话数），删除时据此提示级联范围。
package store

// StorageStats 各表行数概览。
type StorageStats struct {
	Conversations  int `json:"conversations"`
	Messages       int `json:"messages"`
	RunEvents      int `json:"run_events"`
	Agents         int `json:"agents"`
	Projects       int `json:"projects"`
	Skills         int `json:"skills"`
	KnowledgeBases int `json:"knowledge_bases"`
	ProjectFiles   int `json:"project_files"`
	ModelConns     int `json:"model_conns"`
}

// StorageStats 统计各表行数（学习尺度全表 COUNT；表名白名单固定，无注入面）。
func (s *Store) StorageStats() (*StorageStats, error) {
	st := &StorageStats{}
	type item struct {
		dst   *int
		table string
	}
	for _, it := range []item{
		{&st.Conversations, "conversation"},
		{&st.Messages, "message"},
		{&st.RunEvents, "run_event"},
		{&st.Agents, "agent"},
		{&st.Projects, "project"},
		{&st.Skills, "skill"},
		{&st.KnowledgeBases, "knowledge_base"},
		{&st.ProjectFiles, "project_file"},
		{&st.ModelConns, "model_connection"},
	} {
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM ` + it.table).Scan(it.dst); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// ConversationMessageCounts 各对话消息数（conversation_id → count）。
func (s *Store) ConversationMessageCounts() (map[string]int, error) {
	rows, err := s.DB.Query(`SELECT conversation_id, COUNT(*) FROM message GROUP BY conversation_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// ProjectConversationCounts 各项目会话数（project_id → count）。
func (s *Store) ProjectConversationCounts() (map[string]int, error) {
	rows, err := s.DB.Query(`SELECT project_id, COUNT(*) FROM conversation WHERE project_id IS NOT NULL GROUP BY project_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
