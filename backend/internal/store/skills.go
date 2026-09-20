package store

import (
	"database/sql"
	"encoding/json"
	"strings"
)

// Skill 技能包（方案 §6.12）：配置级能力包（提示词 + 工具白名单），不是代码执行环境。
type Skill struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Instruction string          `json:"instruction"`
	Tools       []string        `json:"tools"`
	Resources   []SkillResource `json:"resources"`
	Builtin     bool            `json:"builtin"`
	Enabled     bool            `json:"enabled"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

// SkillResource 技能附加资源（P2 激活注入，本表先存）。
type SkillResource struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

const skillCols = `id,name,description,instruction,tools,resources,builtin,enabled,created_at,updated_at`

func scanSkill(row interface{ Scan(...any) error }) (*Skill, error) {
	var s Skill
	var tools, resources string
	var builtin, enabled int
	if err := row.Scan(&s.ID, &s.Name, &s.Description, &s.Instruction, &tools, &resources, &builtin, &enabled, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	s.Builtin = builtin == 1
	s.Enabled = enabled == 1
	_ = json.Unmarshal([]byte(tools), &s.Tools)
	_ = json.Unmarshal([]byte(resources), &s.Resources)
	if s.Tools == nil {
		s.Tools = []string{}
	}
	if s.Resources == nil {
		s.Resources = []SkillResource{}
	}
	return &s, nil
}

func marshalSkillArrays(s *Skill) (tools, resources string) {
	tools = "[]"
	resources = "[]"
	if b, err := json.Marshal(s.Tools); err == nil {
		tools = string(b)
	}
	if b, err := json.Marshal(s.Resources); err == nil {
		resources = string(b)
	}
	return
}

// ListSkills 返回技能列表。
func (s *Store) ListSkills() ([]*Skill, error) {
	rows, err := s.DB.Query(`SELECT ` + skillCols + ` FROM skill ORDER BY builtin DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Skill{}
	for rows.Next() {
		sk, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

// GetSkill 按 id 取技能。
func (s *Store) GetSkill(id string) (*Skill, error) {
	row := s.DB.QueryRow(`SELECT `+skillCols+` FROM skill WHERE id = ?`, id)
	sk, err := scanSkill(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return sk, err
}

// CreateSkill 新建技能（内置技能不可由此创建）。
func (s *Store) CreateSkill(sk *Skill) (*Skill, error) {
	if strings.TrimSpace(sk.ID) == "" {
		sk.ID = NewID()
	}
	if strings.TrimSpace(sk.Name) == "" {
		return nil, &HTTPError{Status: 400, Msg: "name is required"}
	}
	if strings.TrimSpace(sk.Instruction) == "" {
		return nil, &HTTPError{Status: 400, Msg: "instruction is required"}
	}
	tools, resources := marshalSkillArrays(sk)
	_, err := s.DB.Exec(`INSERT INTO skill (id,name,description,instruction,tools,resources,builtin,enabled,created_at,updated_at)
		VALUES (?,?,?,?,?,?,0,?,?,?)`,
		sk.ID, sk.Name, sk.Description, sk.Instruction, tools, resources, boolInt(sk.Enabled), now(), now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrConflict
		}
		return nil, err
	}
	return s.GetSkill(sk.ID)
}

// UpdateSkill 更新技能；name 唯一冲突返回 ErrConflict。
func (s *Store) UpdateSkill(sk *Skill) (*Skill, error) {
	tools, resources := marshalSkillArrays(sk)
	res, err := s.DB.Exec(`UPDATE skill SET name=?,description=?,instruction=?,tools=?,resources=?,enabled=?,updated_at=? WHERE id=?`,
		sk.Name, sk.Description, sk.Instruction, tools, resources, boolInt(sk.Enabled), now(), sk.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrConflict
		}
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetSkill(sk.ID)
}

// DeleteSkill 删除技能（builtin 由 handler 层拦截）。
func (s *Store) DeleteSkill(id string) error {
	res, err := s.DB.Exec(`DELETE FROM skill WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
