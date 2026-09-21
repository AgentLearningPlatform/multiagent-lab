package store

import (
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
)

// ---- 使用统计（GET /api/stats/usage，前端契约字段名固定）----

// UsageRow 使用统计聚合行。
type UsageRow struct {
	Key              string `json:"key"`
	Label            string `json:"label"`
	Calls            int    `json:"calls"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
}

// UsageRange 使用统计时间范围（YYYY-MM-DD，闭区间；空串=该侧不限）。
// 由 handler 校验格式与先后顺序后传入。
type UsageRange struct {
	From string
	To   string
}

// runStartedMeta run.started 事件解析结果（维度归属）。
type runStartedMeta struct {
	model     string
	agentName string
	convID    string
}

// runUsage run.finished 事件解析出的 token 用量（缺省为 0）。
type runUsage struct {
	prompt     int
	completion int
	total      int
}

// UsageStats 按 model|agent|project 维度聚合运行统计。
// 数据源：run_event 的 run.started（model/agent_name/conversation_id）与
// run.finished（usage），按 run_id 在内存关联聚合（学习尺度，避免 SQL JSON 处理）。
// project 维度经 conversation_id → project.name 归属；无项目会话的运行被排除。
// rng 为空（From/To 均为空）时不做时间过滤。
func (s *Store) UsageStats(groupBy string, rng UsageRange) ([]UsageRow, error) {
	if groupBy != "model" && groupBy != "agent" && groupBy != "project" {
		return nil, &HTTPError{Status: 400, Msg: "group_by must be model|agent|project"}
	}
	started, err := s.loadRunStarted(rng)
	if err != nil {
		return nil, err
	}
	finished, err := s.loadRunFinished(rng)
	if err != nil {
		return nil, err
	}

	// project 维度：conversation_id -> 项目名（仅该维度需要）
	var convProject map[string]string
	if groupBy == "project" {
		convProject, err = s.loadConversationProjects()
		if err != nil {
			return nil, err
		}
	}

	agg := map[string]*UsageRow{}
	for runID, u := range finished {
		st, ok := started[runID]
		if !ok {
			continue // 无 run.started 无法归属维度
		}
		var key, label string
		switch groupBy {
		case "model":
			key, label = st.model, st.model
		case "agent":
			key, label = st.agentName, st.agentName
		case "project":
			name, ok := convProject[st.convID]
			if !ok {
				continue // 会话无项目 → project 维度排除
			}
			key, label = name, name
		}
		row := agg[key]
		if row == nil {
			row = &UsageRow{Key: key, Label: label}
			agg[key] = row
		}
		row.Calls++ // calls = 已完成运行的条数
		row.PromptTokens += u.prompt
		row.CompletionTokens += u.completion
		row.TotalTokens += u.total
	}

	out := make([]UsageRow, 0, len(agg))
	for _, r := range agg {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// usageDateClause 生成 run_event.created_at 的日期范围条件（闭区间）。
// created_at 由驱动以 RFC3339 文本写入，SQLite 的 date() 无法解析其纳秒+Z 形式，
// 故取前 10 位（YYYY-MM-DD）做字典序比较；参数化拼装，避免注入。
// 返回以 " AND " 开头的片段（无过滤条件时返回空串）。
func usageDateClause(rng UsageRange) (string, []any) {
	var conds []string
	var args []any
	if rng.From != "" {
		conds = append(conds, "substr(created_at,1,10) >= ?")
		args = append(args, rng.From)
	}
	if rng.To != "" {
		conds = append(conds, "substr(created_at,1,10) <= ?")
		args = append(args, rng.To)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(conds, " AND "), args
}

// loadRunStarted 读取全部 run.started 事件，解析 model / agent_name / 会话归属。
func (s *Store) loadRunStarted(rng UsageRange) (map[string]runStartedMeta, error) {
	clause, args := usageDateClause(rng)
	rows, err := s.DB.Query(`SELECT run_id, conversation_id, data FROM run_event WHERE type = 'run.started'`+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]runStartedMeta{}
	for rows.Next() {
		var runID, convID string
		var data sql.NullString
		if err := rows.Scan(&runID, &convID, &data); err != nil {
			return nil, err
		}
		var d struct {
			Model     string `json:"model"`
			AgentName string `json:"agent_name"`
		}
		if data.Valid && data.String != "" {
			_ = json.Unmarshal([]byte(data.String), &d)
		}
		out[runID] = runStartedMeta{model: d.Model, agentName: d.AgentName, convID: convID}
	}
	return out, rows.Err()
}

// loadRunFinished 读取全部 run.finished 事件，解析 usage（缺失字段计 0）。
func (s *Store) loadRunFinished(rng UsageRange) (map[string]runUsage, error) {
	clause, args := usageDateClause(rng)
	rows, err := s.DB.Query(`SELECT run_id, data FROM run_event WHERE type = 'run.finished'`+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]runUsage{}
	for rows.Next() {
		var runID string
		var data sql.NullString
		if err := rows.Scan(&runID, &data); err != nil {
			return nil, err
		}
		var d struct {
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		if data.Valid && data.String != "" {
			_ = json.Unmarshal([]byte(data.String), &d)
		}
		u := runUsage{}
		if d.Usage != nil {
			u.prompt = d.Usage.PromptTokens
			u.completion = d.Usage.CompletionTokens
			u.total = d.Usage.TotalTokens
		}
		out[runID] = u
	}
	return out, rows.Err()
}

// loadConversationProjects 会话 → 项目名映射（project 维度归属）。
func (s *Store) loadConversationProjects() (map[string]string, error) {
	rows, err := s.DB.Query(`SELECT c.id, p.name FROM conversation c JOIN project p ON p.id = c.project_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var convID, name string
		if err := rows.Scan(&convID, &name); err != nil {
			return nil, err
		}
		out[convID] = name
	}
	return out, rows.Err()
}
