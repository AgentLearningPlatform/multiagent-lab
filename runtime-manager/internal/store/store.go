// Package store 运行方案持久化（方案 04 §4.1）。
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

// Profile 运行方案记录。Status 状态机：created→starting→running⇄stopped；任意态可进 error。
type Profile struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Engine      string   `json:"engine"`
	OntologyIDs []string `json:"ontology_ids"`
	Config      string   `json:"config"` // 原样 JSON 文本
	Port        int      `json:"port"`
	Status      string   `json:"status"`
	PID         string   `json:"pid,omitempty"`
	LastError   string   `json:"last_error,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type Store struct{ db *sql.DB }

func Open(path, migrationsDir string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(migrationsDir); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(dir string) error {
	bts, err := os.ReadFile(filepath.Join(dir, "001_profile.sql"))
	if err != nil {
		return fmt.Errorf("read migration: %w", err)
	}
	_, err = s.db.Exec(string(bts))
	return err
}

func (s *Store) Close() error { return s.db.Close() }

const profileCols = `id,name,engine,ontology_ids,config,port,status,pid,last_error,created_at,updated_at`

func scanProfile(row interface{ Scan(...any) error }) (*Profile, error) {
	var p Profile
	var oids, cfg, pid, lastErr string
	if err := row.Scan(&p.ID, &p.Name, &p.Engine, &oids, &cfg, &p.Port, &p.Status, &pid, &lastErr, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(oids), &p.OntologyIDs)
	p.Config, p.PID, p.LastError = cfg, pid, lastErr
	return &p, nil
}

func (s *Store) List() ([]*Profile, error) {
	rows, err := s.db.Query(`SELECT ` + profileCols + ` FROM runtime_profile ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Profile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Get(id string) (*Profile, error) {
	p, err := scanProfile(s.db.QueryRow(`SELECT `+profileCols+` FROM runtime_profile WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Store) Create(p *Profile) error {
	oids, _ := json.Marshal(p.OntologyIDs)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`INSERT INTO runtime_profile(id,name,engine,ontology_ids,config,port,status,pid,last_error,created_at,updated_at)
		VALUES(?,?,?,?,?,?,'created','','',?,?)`, p.ID, p.Name, p.Engine, string(oids), p.Config, p.Port, now, now)
	return err
}

func (s *Store) UpdateMeta(id, name, config string, ontologyIDs []string, port int) error {
	oids, _ := json.Marshal(ontologyIDs)
	res, err := s.db.Exec(`UPDATE runtime_profile SET name=?,config=?,ontology_ids=?,port=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		name, config, string(oids), port, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPort 分配端口落库（启动时动态分配）。
func (s *Store) SetPort(id string, port int) error {
	_, err := s.db.Exec(`UPDATE runtime_profile SET port=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, port, id)
	return err
}

// SetStatus 更新状态/错误；pid 仅在启动时传入（"" 表示保持）。
func (s *Store) SetStatus(id, status, lastErr, pid string) error {
	q := `UPDATE runtime_profile SET status=?,last_error=?,updated_at=CURRENT_TIMESTAMP`
	args := []any{status, lastErr}
	if pid != "" {
		q += `,pid=?`
		args = append(args, pid)
	}
	q += ` WHERE id=?`
	args = append(args, id)
	res, err := s.db.Exec(q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM runtime_profile WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RunningByOntology 返回加载了指定本体且处于 running 状态的方案（facade 路由用）。
func (s *Store) RunningByOntology(ontologyID string) (*Profile, error) {
	rows, err := s.db.Query(`SELECT ` + profileCols + ` FROM runtime_profile WHERE status='running'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		for _, oid := range p.OntologyIDs {
			if oid == ontologyID {
				return p, nil
			}
		}
	}
	return nil, rows.Err()
}
