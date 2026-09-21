// Package repo 本体仓库：元数据 + 多形态资产（original 不可变 + spec_json 归一化工作形态）。
package repo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
)

var ErrNotFound = errors.New("not found")

type Ontology struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     int    `json:"version"`
	ForkedFrom  string `json:"forked_from,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	// 统计（从 spec_json 计算，仅列表/详情返回时填充）
	NConcepts  int `json:"n_concepts,omitempty"`
	NRelations int `json:"n_relations,omitempty"`
	NInstances int `json:"n_instances,omitempty"`
}

type Store struct {
	db            *sql.DB
	migrationsDir string
}

func Open(path, migrationsDir string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, migrationsDir: migrationsDir}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	entries, err := os.ReadDir(s.migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, f := range files {
		bts, err := os.ReadFile(filepath.Join(s.migrationsDir, f))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f, err)
		}
		for _, stmt := range splitSQL(string(bts)) {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := s.db.Exec(stmt); err != nil {
				return fmt.Errorf("apply migration %s: %w", f, err)
			}
		}
	}
	return nil
}

// splitSQL 将多语句迁移脚本按分号拆分为独立语句（modernc sqlite 的 Exec 只执行第一条）。
func splitSQL(script string) []string {
	var stmts []string
	var cur strings.Builder
	inStr := false
	for _, r := range script {
		switch {
		case r == '\'':
			inStr = !inStr
		case r == ';' && !inStr:
			stmts = append(stmts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	if strings.TrimSpace(cur.String()) != "" {
		stmts = append(stmts, cur.String())
	}
	return stmts
}

func (s *Store) Close() error { return s.db.Close() }

// ---- 元数据 CRUD ----

func (s *Store) ListOntologies() ([]Ontology, error) {
	rows, err := s.db.Query(`SELECT id,name,description,version,IFNULL(forked_from,''),created_at,updated_at FROM ontology ORDER BY updated_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ontology{}
	for rows.Next() {
		var o Ontology
		if err := rows.Scan(&o.ID, &o.Name, &o.Description, &o.Version, &o.ForkedFrom, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		if err := s.fillStats(&o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) GetOntology(id string) (*Ontology, error) {
	var o Ontology
	err := s.db.QueryRow(`SELECT id,name,description,version,IFNULL(forked_from,''),created_at,updated_at FROM ontology WHERE id=?`, id).
		Scan(&o.ID, &o.Name, &o.Description, &o.Version, &o.ForkedFrom, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.fillStats(&o); err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *Store) fillStats(o *Ontology) error {
	raw, _, err := s.GetArtifact(o.ID, "spec_json")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil // 无归一化形态（如仅有 original 的外部本体尚未归一化）
		}
		return err
	}
	var sp pkgspec.Spec
	if json.Unmarshal([]byte(raw), &sp) == nil {
		o.NConcepts, o.NRelations, o.NInstances = sp.Stats()
	}
	return nil
}

func (s *Store) CreateOntology(id, name, description string) (*Ontology, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`INSERT INTO ontology(id,name,description,version,created_at,updated_at) VALUES(?,?,?,1,?,?)`, id, name, description, now, now)
	if err != nil {
		return nil, err
	}
	return s.GetOntology(id)
}

func (s *Store) UpdateOntology(id, name, description string) error {
	res, err := s.db.Exec(`UPDATE ontology SET name=?,description=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, name, description, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteOntology(id string) error {
	res, err := s.db.Exec(`DELETE FROM ontology WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- 形态资产 ----

// PutArtifact 写入形态。spec_json 版本演进时由调用方 bump version。
func (s *Store) PutArtifact(ontologyID, format, content string, normalized bool) error {
	_, err := s.db.Exec(`INSERT INTO ontology_artifact(ontology_id,format,content,is_normalized,imported_at)
		VALUES(?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(ontology_id,format) DO UPDATE SET content=excluded.content, is_normalized=excluded.is_normalized, imported_at=excluded.imported_at`,
		ontologyID, format, content, b2i(normalized))
	return err
}

func (s *Store) GetArtifact(ontologyID, format string) (content string, importedAt string, err error) {
	err = s.db.QueryRow(`SELECT content,imported_at FROM ontology_artifact WHERE ontology_id=? AND format=?`, ontologyID, format).
		Scan(&content, &importedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return
}

type ArtifactMeta struct {
	Format     string `json:"format"`
	Size       int    `json:"size"`
	Normalized bool   `json:"is_normalized"`
	ImportedAt string `json:"imported_at"`
}

func (s *Store) ListArtifacts(ontologyID string) ([]ArtifactMeta, error) {
	rows, err := s.db.Query(`SELECT format,length(content),is_normalized,imported_at FROM ontology_artifact WHERE ontology_id=? ORDER BY format`, ontologyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ArtifactMeta{}
	for rows.Next() {
		var m ArtifactMeta
		var n int
		if err := rows.Scan(&m.Format, &m.Size, &n, &m.ImportedAt); err != nil {
			return nil, err
		}
		m.Normalized = n != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

// BumpVersion 仓库内容变化时递增版本（REQ-87 显式重载语义的版本锚点）。
func (s *Store) BumpVersion(ontologyID string) (int, error) {
	_, err := s.db.Exec(`UPDATE ontology SET version=version+1, updated_at=CURRENT_TIMESTAMP WHERE id=?`, ontologyID)
	if err != nil {
		return 0, err
	}
	var v int
	err = s.db.QueryRow(`SELECT version FROM ontology WHERE id=?`, ontologyID).Scan(&v)
	return v, err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- 版本历史（docs/04 v0.8 §4.8 / REQ-93）----

// VersionMeta 版本历史条目元信息。
type VersionMeta struct {
	Version        int    `json:"version"`
	CreatedAt      string `json:"created_at"`
	HasOriginal    bool   `json:"has_original"`
	OriginalFormat string `json:"original_format,omitempty"`
	OriginalSize   int    `json:"original_size,omitempty"`
}

// SaveVersion 写入一条版本快照（spec_json 必有；original_format/original_content 可空）。幂等：同 (ontology_id, version) 覆盖。
func (s *Store) SaveVersion(ontologyID string, version int, specJSON, origFormat, origContent string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO ontology_version
		(ontology_id, version, spec_json, original_format, original_content, created_at)
		VALUES (?,?,?,?,?,CURRENT_TIMESTAMP)`,
		ontologyID, version, specJSON, origFormat, origContent)
	return err
}

// ListVersions 列出某本体的版本历史（version 正序）。
func (s *Store) ListVersions(ontologyID string) ([]VersionMeta, error) {
	rows, err := s.db.Query(`SELECT version, created_at, original_format, length(original_content)
		FROM ontology_version WHERE ontology_id=? ORDER BY version`, ontologyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VersionMeta
	for rows.Next() {
		var m VersionMeta
		var origFormat *string
		var origLen *int
		if err := rows.Scan(&m.Version, &m.CreatedAt, &origFormat, &origLen); err != nil {
			return nil, err
		}
		m.HasOriginal = origFormat != nil && *origFormat != "" && origLen != nil && *origLen > 0
		if m.HasOriginal {
			m.OriginalFormat = *origFormat
			m.OriginalSize = *origLen
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetVersionOriginal 读取指定版本的原始源文件内容与格式。
func (s *Store) GetVersionOriginal(ontologyID string, version int) (content, format string, err error) {
	var f, c *string
	err = s.db.QueryRow(`SELECT original_format, original_content FROM ontology_version
		WHERE ontology_id=? AND version=?`, ontologyID, version).Scan(&f, &c)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	if f == nil || *f == "" || c == nil {
		return "", "", ErrNotFound // 该版本由编辑产生，无原始源文件
	}
	return *c, *f, nil
}

// GetVersionSpec 读取指定版本的 spec_json 快照（版本回看/diff 的数据基础，REQ-95）。
func (s *Store) GetVersionSpec(ontologyID string, version int) (string, error) {
	var specJSON *string
	err := s.db.QueryRow(`SELECT spec_json FROM ontology_version
		WHERE ontology_id=? AND version=?`, ontologyID, version).Scan(&specJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if specJSON == nil {
		return "", ErrNotFound
	}
	return *specJSON, nil
}
