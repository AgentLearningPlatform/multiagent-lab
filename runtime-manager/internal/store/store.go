// Package store 运行方案持久化（方案 04 §4.1）。
package store

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
	// REQ-155/M-O15 阶段二：启动/重载成功时的加载版本快照（JSON {ontology_id: version}），生命周期 drift 检测数据源
	LoadedVersions string `json:"loaded_versions,omitempty"`
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
	entries, err := os.ReadDir(dir)
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
		bts, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f, err)
		}
		for _, stmt := range splitSQL(string(bts)) {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := s.db.Exec(stmt); err != nil {
				// 幂等容忍：重放式迁移下 ALTER ADD COLUMN 无法 IF NOT EXISTS（REQ-155/M-O15 003 起）
				if strings.Contains(err.Error(), "duplicate column name") {
					continue
				}
				return fmt.Errorf("apply migration %s: %w", f, err)
			}
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

const profileCols = `id,name,engine,ontology_ids,config,port,status,pid,last_error,created_at,updated_at,IFNULL(loaded_versions,'')`

func scanProfile(row interface{ Scan(...any) error }) (*Profile, error) {
	var p Profile
	var oids, cfg, pid, lastErr, loadedVersions string
	if err := row.Scan(&p.ID, &p.Name, &p.Engine, &oids, &cfg, &p.Port, &p.Status, &pid, &lastErr, &p.CreatedAt, &p.UpdatedAt, &loadedVersions); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(oids), &p.OntologyIDs)
	p.Config, p.PID, p.LastError, p.LoadedVersions = cfg, pid, lastErr, loadedVersions
	return &p, nil
}

// RuntimeConfig 全局运行配置（REQ-179/M-O16：执行方式为系统级配置而非方案级——2026-09-27 开发者指示变更；
// REQ-236④/M63 D-O20 v0.73 变更拍板：默认执行方式改 docker，未安装 docker 时启动期降级进程内 native）。
type RuntimeConfig struct {
	ExecutionMethod string `json:"execution_method"` // docker | native | k8s（默认 docker）
}

// GetConfig 读全局运行配置（无行/空值 = 默认 docker——D-O20 v0.73 变更；docker 不可用的
// 运行期降级在 manager.startEngine 探测兜底，配置值不写回）。
func (s *Store) GetConfig() RuntimeConfig {
	var method string
	_ = s.db.QueryRow(`SELECT execution_method FROM runtime_config WHERE id=1`).Scan(&method)
	method = strings.TrimSpace(method)
	if method == "" {
		method = "docker"
	}
	return RuntimeConfig{ExecutionMethod: method}
}

// SetConfig 写全局运行配置（单行 upsert）。
func (s *Store) SetConfig(cfg RuntimeConfig) error {
	_, err := s.db.Exec(`INSERT INTO runtime_config(id, execution_method) VALUES(1, ?)
		ON CONFLICT(id) DO UPDATE SET execution_method=excluded.execution_method`, cfg.ExecutionMethod)
	return err
}

// SetLoadedVersions REQ-155/M-O15 阶段二：记录启动/重载成功时的加载版本快照。
func (s *Store) SetLoadedVersions(id string, versionsJSON string) error {
	_, err := s.db.Exec(`UPDATE runtime_profile SET loaded_versions=? WHERE id=?`, versionsJSON, id)
	return err
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
// REQ-236②/M63：同本体多方案时取**最早创建**（created_at 升序，并列按 id 字典序）——
// 消除「取第一个匹配」的任意性，路由结果确定可复现（显式绑定语义随需求推进）。
func (s *Store) RunningByOntology(ontologyID string) (*Profile, error) {
	rows, err := s.db.Query(`SELECT ` + profileCols + ` FROM runtime_profile WHERE status='running' ORDER BY created_at ASC, id ASC`)
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

// ---- 翻译透视（REQ-94，docs/04 v0.8 §4.8.2）----

// Trace 一条工具→SPARQL 翻译透视记录。
type Trace struct {
	ID          int64  `json:"id,omitempty"`
	TS          string `json:"ts"`
	Tool        string `json:"tool"`
	ProfileID   string `json:"profile_id"`
	OntologyID  string `json:"ontology_id"`
	Sparql      string `json:"sparql"`
	TookMS      int64  `json:"took_ms"`
	ResultCount int    `json:"result_count"`
	Ok          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
}

// SaveTrace 追加一条透视记录。
func (s *Store) SaveTrace(t *Trace) error {
	ok := 0
	if t.Ok {
		ok = 1
	}
	_, err := s.db.Exec(`INSERT INTO trace_log(tool,profile_id,ontology_id,sparql,took_ms,result_count,ok,error)
		VALUES(?,?,?,?,?,?,?,?)`, t.Tool, t.ProfileID, t.OntologyID, t.Sparql, t.TookMS, t.ResultCount, ok, t.Error)
	return err
}

// ListTraces 按方案倒序列出透视记录（limit<=0 或 >200 时取 50）。
func (s *Store) ListTraces(profileID string, limit int) ([]*Trace, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id,ts,tool,profile_id,ontology_id,sparql,took_ms,result_count,ok,error
		FROM trace_log WHERE profile_id=? ORDER BY id DESC LIMIT ?`, profileID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Trace{}
	for rows.Next() {
		var t Trace
		var errText *string
		var ok int
		if err := rows.Scan(&t.ID, &t.TS, &t.Tool, &t.ProfileID, &t.OntologyID, &t.Sparql, &t.TookMS, &t.ResultCount, &ok, &errText); err != nil {
			return nil, err
		}
		t.Ok = ok == 1
		if errText != nil {
			t.Error = *errText
		}
		out = append(out, &t)
	}
	return out, rows.Err()
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
