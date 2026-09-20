package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// 模型连接 CRUD。API Key 密文（api_key_enc）经 secrets 包加密后由上层写入。

func scanConn(row interface{ Scan(...any) error }) (*ModelConnection, error) {
	var c ModelConnection
	var enc []byte
	var enabled, isDefault int
	err := row.Scan(&c.ID, &c.Name, &c.ConnType, &c.Protocol, &c.BaseURL, &c.ModelName, &enc, &c.APIKeyHint, &enabled, &isDefault, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.HasKey = len(enc) > 0
	c.Enabled = enabled == 1
	c.IsDefault = isDefault == 1
	return &c, nil
}

const connCols = `id,name,conn_type,protocol,base_url,model_name,api_key_enc,api_key_hint,enabled,is_default,created_at,updated_at`

// ListConnections 返回全部连接。
func (s *Store) ListConnections() ([]*ModelConnection, error) {
	rows, err := s.DB.Query(`SELECT ` + connCols + ` FROM model_connection ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ModelConnection
	for rows.Next() {
		c, err := scanConn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetConnection 按 ID 查询。
func (s *Store) GetConnection(id string) (*ModelConnection, error) {
	row := s.DB.QueryRow(`SELECT `+connCols+` FROM model_connection WHERE id = ?`, id)
	c, err := scanConn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// ConnectionRecord 含密文的完整记录（内部使用）。
type ConnectionRecord struct {
	Conn      *ModelConnection
	Encrypted []byte
}

// GetConnectionRecord 取含密文记录（工厂解密用）。
func (s *Store) GetConnectionRecord(id string) (*ConnectionRecord, error) {
	var c ModelConnection
	var enc []byte
	var enabled, isDefault int
	row := s.DB.QueryRow(`SELECT `+connCols+` FROM model_connection WHERE id = ?`, id)
	err := row.Scan(&c.ID, &c.Name, &c.ConnType, &c.Protocol, &c.BaseURL, &c.ModelName, &enc, &c.APIKeyHint, &enabled, &isDefault, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Enabled = enabled == 1
	c.IsDefault = isDefault == 1
	return &ConnectionRecord{Conn: &c, Encrypted: enc}, nil
}

// CreateConnection 新建连接。
func (s *Store) CreateConnection(c *ModelConnection, enc []byte) (*ModelConnection, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	if c.ConnType != "chat" && c.ConnType != "embedding" {
		return nil, &HTTPError{Status: 400, Msg: "conn_type must be chat|embedding"}
	}
	if c.Protocol == "" {
		c.Protocol = "openai_compat"
	}
	_, err := s.DB.Exec(`INSERT INTO model_connection (`+connCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Name, c.ConnType, c.Protocol, c.BaseURL, c.ModelName, enc, c.APIKeyHint, boolInt(c.Enabled), boolInt(c.IsDefault), now(), now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrConflict
		}
		return nil, err
	}
	return s.GetConnection(c.ID)
}

// UpdateConnection 更新连接；apiKeyEnc 为 nil 表示不改 key。
func (s *Store) UpdateConnection(c *ModelConnection, apiKeyEnc []byte) (*ModelConnection, error) {
	var (
		res sql.Result
		err error
	)
	if apiKeyEnc != nil {
		res, err = s.DB.Exec(`UPDATE model_connection SET name=?,conn_type=?,protocol=?,base_url=?,model_name=?,api_key_enc=?,api_key_hint=?,enabled=?,is_default=?,updated_at=? WHERE id=?`,
			c.Name, c.ConnType, c.Protocol, c.BaseURL, c.ModelName, apiKeyEnc, c.APIKeyHint, boolInt(c.Enabled), boolInt(c.IsDefault), now(), c.ID)
	} else {
		res, err = s.DB.Exec(`UPDATE model_connection SET name=?,conn_type=?,protocol=?,base_url=?,model_name=?,enabled=?,is_default=?,updated_at=? WHERE id=?`,
			c.Name, c.ConnType, c.Protocol, c.BaseURL, c.ModelName, boolInt(c.Enabled), boolInt(c.IsDefault), now(), c.ID)
	}
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrConflict
		}
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetConnection(c.ID)
}

// DeleteConnection 删除连接。
func (s *Store) DeleteConnection(id string) error {
	res, err := s.DB.Exec(`DELETE FROM model_connection WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetDefaultConnection 设置某类型默认连接（先清后设，每类型至多一条）。
func (s *Store) SetDefaultConnection(id, connType string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`UPDATE model_connection SET is_default = 0 WHERE conn_type = ?`, connType); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE model_connection SET is_default = 1 WHERE id = ? AND conn_type = ?`, id, connType)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// GetDefaultConnection 取某类型默认连接。
func (s *Store) GetDefaultConnection(connType string) (*ModelConnection, error) {
	row := s.DB.QueryRow(`SELECT `+connCols+` FROM model_connection WHERE conn_type = ? AND is_default = 1 AND enabled = 1 LIMIT 1`, connType)
	c, err := scanConn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // 无默认连接不是错误，由上层决定提示
	}
	if err != nil {
		return nil, fmt.Errorf("get default connection: %w", err)
	}
	return c, nil
}

// GetConnectionKey 取某连接解密后的 key（由上层注入解密函数，避免 store 依赖 crypto）。
func (s *Store) CountConnsUsingType(connType string) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM model_connection WHERE conn_type = ?`, connType).Scan(&n)
	return n, err
}
