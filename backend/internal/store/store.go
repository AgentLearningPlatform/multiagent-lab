// Package store 提供 SQLite 存储层：打开连接、执行迁移与各实体 CRUD。
package store

import (
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store 封装主平台 SQLite。
type Store struct {
	DB *sql.DB
}

// Open 打开（必要时创建）SQLite 数据库并执行迁移。
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// 学习尺度：单写连接即可，避免 SQLITE_BUSY
	db.SetMaxOpenConns(1)
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	// 迁移记录表：SQLite 不支持 ADD COLUMN IF NOT EXISTS，通过记录已应用迁移
	// 保证 006 等 ALTER 型迁移在重复启动时幂等（001~005 均为 IF NOT EXISTS/OR IGNORE，重放安全）。
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_migration (
		name TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create schema_migration: %w", err)
	}
	applied := map[string]bool{}
	rows, err := s.DB.Query(`SELECT name FROM schema_migration`)
	if err != nil {
		return fmt.Errorf("read schema_migration: %w", err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		applied[n] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		if applied[name] {
			continue
		}
		b, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := s.DB.Exec(string(b)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := s.DB.Exec(`INSERT OR IGNORE INTO schema_migration (name) VALUES (?)`, name); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	return nil
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.DB.Close() }

// NewID 生成 32 位十六进制随机 ID（本地学习尺度，无需外键可读性）。
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// HTTPError 带 HTTP 状态码的错误，用于 handler 返回。
type HTTPError struct {
	Status int
	Msg    string
}

func (e *HTTPError) Error() string { return e.Msg }

// ErrNotFound 通用 404。
var ErrNotFound = &HTTPError{Status: http.StatusNotFound, Msg: "not found"}

// ErrConflict 通用 409（如唯一名冲突）。
var ErrConflict = &HTTPError{Status: http.StatusConflict, Msg: "conflict"}

func now() time.Time { return time.Now().UTC() }
