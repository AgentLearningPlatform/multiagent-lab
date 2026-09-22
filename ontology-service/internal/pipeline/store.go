package pipeline

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ErrNotFound 配置不存在。
var ErrNotFound = errors.New("pipeline profile 不存在")

// Profile 工具链配置（pipeline_profile 行）。
type Profile struct {
	ID               string                    `json:"id"`
	Name             string                    `json:"name"`
	OntologyID       string                    `json:"ontology_id,omitempty"`
	RuntimeProfileID string                    `json:"runtime_profile_id,omitempty"`
	Stages           map[string]StageSelection `json:"stages"`
	Checklist        map[string]any            `json:"checklist"`
	CreatedAt        string                    `json:"created_at,omitempty"`
	UpdatedAt        string                    `json:"updated_at,omitempty"`
}

// Store pipeline_profile 存取（复用构建平面 SQLite；schema 走 004_pipeline.sql）。
type Store struct {
	db *sql.DB
}

// NewStore 构造（db 传 repo.Store.DB()）。
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func scanProfile(row interface{ Scan(...any) error }) (*Profile, error) {
	var p Profile
	var stages, checklist string
	var ontoID, rtID sql.NullString
	if err := row.Scan(&p.ID, &p.Name, &ontoID, &rtID, &stages, &checklist, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.OntologyID, p.RuntimeProfileID = ontoID.String, rtID.String
	if err := json.Unmarshal([]byte(stages), &p.Stages); err != nil {
		p.Stages = map[string]StageSelection{}
	}
	if err := json.Unmarshal([]byte(checklist), &p.Checklist); err != nil {
		p.Checklist = map[string]any{}
	}
	return &p, nil
}

// Create 新建配置（stages 空时落默认工具链）。
func (s *Store) Create(id, name, ontologyID string, stages map[string]StageSelection) (*Profile, error) {
	if stages == nil {
		stages = DefaultStages()
	}
	sb, _ := json.Marshal(stages)
	_, err := s.db.Exec(`INSERT INTO pipeline_profile(id,name,ontology_id,stages) VALUES(?,?,?,?)`,
		id, name, ontologyID, string(sb))
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Get 按 id 读取。
func (s *Store) Get(id string) (*Profile, error) {
	return scanProfile(s.db.QueryRow(
		`SELECT id,name,ontology_id,runtime_profile_id,stages,checklist,created_at,updated_at FROM pipeline_profile WHERE id=?`, id))
}

// List 全部配置（按更新时间倒序）。
func (s *Store) List() ([]Profile, error) {
	rows, err := s.db.Query(
		`SELECT id,name,ontology_id,runtime_profile_id,stages,checklist,created_at,updated_at FROM pipeline_profile ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Profile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// UpdateStages 覆盖阶段选择（PUT 全量语义）。
func (s *Store) UpdateStages(id string, stages map[string]StageSelection) error {
	sb, _ := json.Marshal(stages)
	res, err := s.db.Exec(`UPDATE pipeline_profile SET stages=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, string(sb), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateMeta 更新名称/绑定本体/S5 运行方案引用。
func (s *Store) UpdateMeta(id, name string, ontologyID, runtimeProfileID *string) error {
	res, err := s.db.Exec(`UPDATE pipeline_profile SET name=?, ontology_id=?, runtime_profile_id=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		name, ontologyID, runtimeProfileID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Check 打卡：checklist[key] = {done_at}；再点取消（toggle 语义由调用方传 done）。
func (s *Store) Check(id, key string, done bool) error {
	p, err := s.Get(id)
	if err != nil {
		return err
	}
	if done {
		p.Checklist[key] = map[string]any{"done_at": time.Now().UTC().Format(time.RFC3339)}
	} else {
		delete(p.Checklist, key)
	}
	cb, _ := json.Marshal(p.Checklist)
	_, err = s.db.Exec(`UPDATE pipeline_profile SET checklist=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, string(cb), id)
	return err
}

// Delete 删除配置。
func (s *Store) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM pipeline_profile WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
