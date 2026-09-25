package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

func scanAgent(row interface{ Scan(...any) error }) (*Agent, error) {
	var a Agent
	var tools, skills, mcp, mcpServe string
	var modelConn sql.NullString
	var temp sql.NullFloat64
	var maxTok sql.NullInt64
	var sandboxCPUs sql.NullFloat64
	var companionOntology int
	err := row.Scan(&a.ID, &a.Name, &a.Description, &a.Instruction, &modelConn, &temp, &maxTok,
		&a.MaxIteration, &tools, &skills, &mcp, &a.RuntimeBackend, &a.InferenceBackend, &a.LogoURL, &a.ToolApproval, &mcpServe, &a.SandboxMemory, &sandboxCPUs, &companionOntology, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if modelConn.Valid {
		a.ModelConnID = &modelConn.String
	}
	if temp.Valid {
		a.Temperature = &temp.Float64
	}
	if maxTok.Valid {
		v := int(maxTok.Int64)
		a.MaxTokens = &v
	}
	_ = json.Unmarshal([]byte(tools), &a.Tools)
	_ = json.Unmarshal([]byte(skills), &a.Skills)
	_ = json.Unmarshal([]byte(mcp), &a.MCPServers)
	_ = json.Unmarshal([]byte(mcpServe), &a.McpServe)
	if sandboxCPUs.Valid {
		a.SandboxCPUs = sandboxCPUs.Float64
	}
	a.CompanionOntology = companionOntology != 0
	if a.Tools == nil {
		a.Tools = []string{}
	}
	if a.Skills == nil {
		a.Skills = []string{}
	}
	if a.MCPServers == nil {
		a.MCPServers = []MCPServer{}
	}
	return &a, nil
}

const agentCols = `id,name,description,instruction,model_conn_id,temperature,max_tokens,max_iteration,tools,skills,mcp_servers,runtime_backend,inference_backend,logo_url,tool_approval,mcp_serve,sandbox_memory,sandbox_cpus,companion_ontology,created_at,updated_at`

// ListAgents 返回全部 Agent（按创建时间升序）。
func (s *Store) ListAgents() ([]*Agent, error) {
	rows, err := s.DB.Query(`SELECT ` + agentCols + ` FROM agent ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAgent 按 ID 查询。
func (s *Store) GetAgent(id string) (*Agent, error) {
	row := s.DB.QueryRow(`SELECT `+agentCols+` FROM agent WHERE id = ?`, id)
	a, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// CreateAgent 新建；名称唯一冲突返回 ErrConflict。
func (s *Store) CreateAgent(a *Agent) (*Agent, error) {
	if a.ID == "" {
		a.ID = NewID()
	}
	tools, _ := json.Marshal(a.Tools)
	skills, _ := json.Marshal(a.Skills)
	mcp, _ := json.Marshal(a.MCPServers)
	mcpServeJSON, _ := json.Marshal(a.McpServe)
	_, err := s.DB.Exec(`INSERT INTO agent (`+agentCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.Description, a.Instruction, a.ModelConnID, a.Temperature, a.MaxTokens,
		a.MaxIteration, string(tools), string(skills), string(mcp), a.RuntimeBackend, a.InferenceBackend, a.LogoURL, a.ToolApproval, string(mcpServeJSON), a.SandboxMemory, a.SandboxCPUs, boolToInt(a.CompanionOntology), now(), now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrConflict
		}
		return nil, err
	}
	return s.GetAgent(a.ID)
}

// UpdateAgent 全量更新（字段以传入值为准）。
func (s *Store) UpdateAgent(a *Agent) (*Agent, error) {
	tools, _ := json.Marshal(a.Tools)
	skills, _ := json.Marshal(a.Skills)
	mcp, _ := json.Marshal(a.MCPServers)
	mcpServeJSON, _ := json.Marshal(a.McpServe)
	res, err := s.DB.Exec(`UPDATE agent SET name=?,description=?,instruction=?,model_conn_id=?,temperature=?,max_tokens=?,max_iteration=?,tools=?,skills=?,mcp_servers=?,runtime_backend=?,inference_backend=?,logo_url=?,tool_approval=?,mcp_serve=?,sandbox_memory=?,sandbox_cpus=?,companion_ontology=?,updated_at=? WHERE id=?`,
		a.Name, a.Description, a.Instruction, a.ModelConnID, a.Temperature, a.MaxTokens,
		a.MaxIteration, string(tools), string(skills), string(mcp), a.RuntimeBackend, a.InferenceBackend, a.LogoURL, a.ToolApproval, string(mcpServeJSON), a.SandboxMemory, a.SandboxCPUs, boolToInt(a.CompanionOntology), now(), a.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrConflict
		}
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetAgent(a.ID)
}

// DeleteAgent 删除（级联 conversation/project_agent）。
func (s *Store) DeleteAgent(id string) error {
	res, err := s.DB.Exec(`DELETE FROM agent WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- project_agent ----

// SetProjectAgents 全量重设项目成员（role: coordinator/member）。
func (s *Store) SetProjectAgents(projectID string, members []ProjectMember) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM project_agent WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	for _, m := range members {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO project_agent (project_id, agent_id, role) VALUES (?,?,?)`,
			projectID, m.AgentID, m.Role); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ProjectMember 项目成员。
type ProjectMember struct {
	AgentID string `json:"agent_id"`
	Role    string `json:"role"` // coordinator | member
}

// ListProjectAgents 返回项目成员。
func (s *Store) ListProjectAgents(projectID string) ([]ProjectMember, error) {
	rows, err := s.DB.Query(`SELECT agent_id, role FROM project_agent WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectMember
	for rows.Next() {
		var m ProjectMember
		if err := rows.Scan(&m.AgentID, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountAgentsUsingConn 统计引用某模型连接的 Agent 数（引用保护提示用）。
func (s *Store) CountAgentsUsingConn(connID string) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM agent WHERE model_conn_id = ?`, connID).Scan(&n)
	return n, err
}

// boolToInt SQLite 无原生 bool，用 0/1 整型承载。
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
