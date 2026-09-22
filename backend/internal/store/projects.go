package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

func scanProject(row interface{ Scan(...any) error }) (*Project, error) {
	var p Project
	var constraints string
	var agentIDs string
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.CollabMode, &p.WorkflowMode, &constraints, &agentIDs, &p.Coordinator, &p.CreatedAt, &p.UpdatedAt, &p.LocalDir)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(agentIDs), &p.AgentIDs)
	if p.AgentIDs == nil {
		p.AgentIDs = []string{}
	}
	p.Constraints = constraints
	return &p, nil
}

const projectCols = `p.id, p.name, p.description, p.collab_mode, p.workflow_mode, p.constraints,
 COALESCE((SELECT json_group_array(agent_id) FROM project_agent pa WHERE pa.project_id = p.id), '[]') AS agent_ids,
 COALESCE((SELECT agent_id FROM project_agent pa WHERE pa.project_id = p.id AND pa.role = 'coordinator' LIMIT 1), '') AS coordinator,
 p.created_at, p.updated_at, p.local_dir`

// ListProjects 返回全部项目。
func (s *Store) ListProjects() ([]*Project, error) {
	rows, err := s.DB.Query(`SELECT ` + projectCols + ` FROM project p ORDER BY p.created_at, p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProject 按 ID 查询。
func (s *Store) GetProject(id string) (*Project, error) {
	row := s.DB.QueryRow(`SELECT `+projectCols+` FROM project p WHERE p.id = ?`, id)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// CreateProject 新建项目（成员经 SetProjectAgents 单独设置）。
func (s *Store) CreateProject(p *Project) (*Project, error) {
	if p.ID == "" {
		p.ID = NewID()
	}
	if p.CollabMode == "" {
		p.CollabMode = "agent_as_tool"
	}
	if p.WorkflowMode == "" {
		p.WorkflowMode = "free"
	}
	_, err := s.DB.Exec(`INSERT INTO project (id,name,description,collab_mode,workflow_mode,constraints,local_dir,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.Description, p.CollabMode, p.WorkflowMode, p.Constraints, p.LocalDir, now(), now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrConflict
		}
		return nil, err
	}
	return s.GetProject(p.ID)
}

// UpdateProject 更新项目基础字段。
func (s *Store) UpdateProject(p *Project) (*Project, error) {
	res, err := s.DB.Exec(`UPDATE project SET name=?,description=?,collab_mode=?,workflow_mode=?,constraints=?,local_dir=?,updated_at=? WHERE id=?`,
		p.Name, p.Description, p.CollabMode, p.WorkflowMode, p.Constraints, p.LocalDir, now(), p.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrConflict
		}
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetProject(p.ID)
}

// DeleteProject 删除项目（级联对话）。
func (s *Store) DeleteProject(id string) error {
	res, err := s.DB.Exec(`DELETE FROM project WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- 项目文件（M11 §5.2 project_file）----

// InsertProjectFile 写入文件元数据，返回 ID。
func (s *Store) InsertProjectFile(pf *ProjectFile) (string, error) {
	if pf.ID == "" {
		pf.ID = NewID()
	}
	_, err := s.DB.Exec(`INSERT INTO project_file (id,project_id,conversation_id,name,path,size,mime,source,created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		pf.ID, pf.ProjectID, pf.ConversationID, pf.Name, pf.Path, pf.Size, pf.Mime, pf.Source, now())
	return pf.ID, err
}

// ListProjectFiles 列出项目文件（产物 + 上传）。
func (s *Store) ListProjectFiles(projectID string) ([]*ProjectFile, error) {
	rows, err := s.DB.Query(`SELECT id,project_id,IFNULL(conversation_id,''),name,path,IFNULL(size,0),IFNULL(mime,''),IFNULL(source,'upload'),IFNULL(created_at,'') FROM project_file WHERE project_id = ? ORDER BY created_at, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProjectFile
	for rows.Next() {
		pf := &ProjectFile{}
		if err := rows.Scan(&pf.ID, &pf.ProjectID, &pf.ConversationID, &pf.Name, &pf.Path, &pf.Size, &pf.Mime, &pf.Source, &pf.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, pf)
	}
	return out, rows.Err()
}

// GetProjectFile 按 ID 取文件元数据。
func (s *Store) GetProjectFile(id string) (*ProjectFile, error) {
	row := s.DB.QueryRow(`SELECT id,project_id,IFNULL(conversation_id,''),name,path,IFNULL(size,0),IFNULL(mime,''),IFNULL(source,'upload'),IFNULL(created_at,'') FROM project_file WHERE id = ?`, id)
	pf := &ProjectFile{}
	if err := row.Scan(&pf.ID, &pf.ProjectID, &pf.ConversationID, &pf.Name, &pf.Path, &pf.Size, &pf.Mime, &pf.Source, &pf.CreatedAt); err != nil {
		return nil, ErrNotFound
	}
	return pf, nil
}
