package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// ---------------------------------------------------------------------------
// REQ-226/M54 配置治理：Agent 配置版本面。PUT agent 成功后自动快照（保存即版本），
// 一键回滚=读历史版本整包写回当前配置（回滚动作本身也先快照「回滚前」状态，可再滚回）。
// 保留最近 maxConfigVersionsPerAgent 版，写入时惰性裁剪。
// ---------------------------------------------------------------------------

const maxConfigVersionsPerAgent = 50

// AgentConfigVersion 一条配置版本快照。
type AgentConfigVersion struct {
	ID         string `json:"id"`
	AgentID    string `json:"agent_id"`
	Version    int    `json:"version"`
	ConfigJSON string `json:"config_json"`
	Note       string `json:"note"`
	CreatedAt  string `json:"created_at"`
}

// InsertAgentConfigVersion PUT 成功后落快照（版本号=当前最大+1；并裁剪超限旧版）。
// 同一事务内完成：取号→插入→裁剪。
func (s *Store) InsertAgentConfigVersion(agentID, configJSON, note string) (*AgentConfigVersion, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	var maxVer int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(version),0) FROM agent_config_version WHERE agent_id=?`, agentID).Scan(&maxVer); err != nil {
		return nil, err
	}
	v := &AgentConfigVersion{ID: NewID(), AgentID: agentID, Version: maxVer + 1, ConfigJSON: configJSON, Note: note}
	if _, err := tx.Exec(`INSERT INTO agent_config_version (id,agent_id,version,config_json,note) VALUES (?,?,?,?,?)`,
		v.ID, v.AgentID, v.Version, v.ConfigJSON, v.Note); err != nil {
		return nil, err
	}
	// 惰性裁剪：仅保留最近 N 版
	if _, err := tx.Exec(`DELETE FROM agent_config_version WHERE agent_id=? AND version<=?`, agentID, maxVer+1-maxConfigVersionsPerAgent); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return v, nil
}

// ListAgentConfigVersions 版本列表（新→旧；不含 config_json 大字段）。
func (s *Store) ListAgentConfigVersions(agentID string) ([]*AgentConfigVersion, error) {
	rows, err := s.DB.Query(`SELECT id,agent_id,version,config_json,note,created_at FROM agent_config_version WHERE agent_id=? ORDER BY version DESC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AgentConfigVersion
	for rows.Next() {
		v := &AgentConfigVersion{}
		if err := rows.Scan(&v.ID, &v.AgentID, &v.Version, &v.ConfigJSON, &v.Note, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetAgentConfigVersion 单版本（含 config_json）。
func (s *Store) GetAgentConfigVersion(agentID string, version int) (*AgentConfigVersion, error) {
	row := s.DB.QueryRow(`SELECT id,agent_id,version,config_json,note,created_at FROM agent_config_version WHERE agent_id=? AND version=?`, agentID, version)
	v := &AgentConfigVersion{}
	if err := row.Scan(&v.ID, &v.AgentID, &v.Version, &v.ConfigJSON, &v.Note, &v.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return v, nil
}

// AgentConfigDiff 两版配置的结构化差异（字段级；简单平坦比较——顶层标量直接比，
// 数组/对象序列化后比；返回 [字段, 旧值, 新值] 行）。纯函数便于单测。
func AgentConfigDiff(oldJSON, newJSON string) ([]map[string]string, error) {
	var oldMap, newMap map[string]any
	if err := json.Unmarshal([]byte(oldJSON), &oldMap); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(newJSON), &newMap); err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for k := range oldMap {
		keys[k] = true
	}
	for k := range newMap {
		keys[k] = true
	}
	out := []map[string]string{}
	for k := range keys {
		if k == "updated_at" {
			continue
		}
		ov, nv := flatVal(oldMap[k]), flatVal(newMap[k])
		if ov != nv {
			out = append(out, map[string]string{"field": k, "old": ov, "new": nv})
		}
	}
	// 字段名排序保证输出稳定
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j]["field"] < out[j-1]["field"]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

func flatVal(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		b, _ := json.Marshal(t)
		return string(b)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
