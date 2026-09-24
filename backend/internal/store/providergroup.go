package store

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// ProviderGroup 供应商分组（REQ-148）：分组标识与 BaseURL 解耦——同一供应商（同 BaseURL）
// 可创建多个独立实例（不同账号/Key/用途）。alias 为显示层别名（仅展示，不改连接真名）。
type ProviderGroup struct {
	ID        string `json:"id"`
	Alias     string `json:"alias"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListProviderGroups 返回全部分组。
func (s *Store) ListProviderGroups() ([]*ProviderGroup, error) {
	rows, err := s.DB.Query(`SELECT id,alias,created_at,updated_at FROM provider_group ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProviderGroup
	for rows.Next() {
		var g ProviderGroup
		if err := rows.Scan(&g.ID, &g.Alias, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &g)
	}
	return out, rows.Err()
}

// CreateProviderGroup 新建分组（alias 可空，空 = 展示名从锚点连接名派生）。
func (s *Store) CreateProviderGroup(alias string) (*ProviderGroup, error) {
	g := &ProviderGroup{ID: "pg_" + NewID(), Alias: alias}
	if _, err := s.DB.Exec(`INSERT INTO provider_group (id,alias,created_at,updated_at) VALUES (?,?,?,?)`,
		g.ID, g.Alias, now(), now()); err != nil {
		return nil, err
	}
	return g, nil
}

// UpdateProviderGroup 更新分组别名。
func (s *Store) UpdateProviderGroup(id, alias string) error {
	res, err := s.DB.Exec(`UPDATE provider_group SET alias=?,updated_at=? WHERE id=?`, alias, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetProviderGroup 按组 ID 查询（不存在返回 ErrNotFound）。
func (s *Store) GetProviderGroup(id string) (*ProviderGroup, error) {
	row := s.DB.QueryRow(`SELECT id,alias,created_at,updated_at FROM provider_group WHERE id=?`, id)
	var g ProviderGroup
	err := row.Scan(&g.ID, &g.Alias, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// backfillGroupID 由 (protocol, base_url) 派生的确定性分组 ID（跨启动幂等，同键同 ID）。
func backfillGroupID(protocol, baseURL string) string {
	sum := sha1.Sum([]byte(protocol + "::" + baseURL))
	return "pgbg_" + hex.EncodeToString(sum[:8])
}

// BackfillProviderGroups 老数据幂等回填（REQ-148）：为没有 provider_group_id 的连接按
// (protocol, base_url) 建组并归属——分组键从 BaseURL 迁移到独立标识，存量行为不变
// （同 BaseURL 仍聚为一组），此后新建连接可自由拆分多实例。可在每次启动时安全重放。
func (s *Store) BackfillProviderGroups() error {
	rows, err := s.DB.Query(`SELECT DISTINCT protocol, base_url FROM model_connection WHERE provider_group_id = ''`)
	if err != nil {
		return fmt.Errorf("scan ungrouped connections: %w", err)
	}
	type key struct{ protocol, baseURL string }
	var pending []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.protocol, &k.baseURL); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, k)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, k := range pending {
		id := backfillGroupID(k.protocol, k.baseURL)
		if _, err := s.DB.Exec(`INSERT OR IGNORE INTO provider_group (id,alias,created_at,updated_at) VALUES (?,'',?,?)`, id, now(), now()); err != nil {
			return fmt.Errorf("ensure provider group: %w", err)
		}
		if _, err := s.DB.Exec(`UPDATE model_connection SET provider_group_id=? WHERE provider_group_id='' AND protocol=? AND base_url=?`, id, k.protocol, k.baseURL); err != nil {
			return fmt.Errorf("assign provider group: %w", err)
		}
	}
	return nil
}
