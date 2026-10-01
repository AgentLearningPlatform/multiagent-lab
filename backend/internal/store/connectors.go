package store

// REQ-214/M46：外部连接器（Connector）存储层。
// 连接器 = 连接对象（kind）× 交付驱动（mcp 直通 / 平台托管插件服务）的统一抽象；
// 凭据服务端绑定（credentials_encrypted），agent 侧仅存实例 id 引用（连接白名单）。

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// 连接器类型（阶段一 mcp；阶段二 kubernetes/ssh）。
const (
	ConnectorKindMCP        = "mcp"
	ConnectorKindKubernetes = "kubernetes"
	ConnectorKindSSH        = "ssh"
)

// ConnectorKinds 合法类型白名单。
var ConnectorKinds = []string{ConnectorKindMCP, ConnectorKindKubernetes, ConnectorKindSSH}

// BuiltinConnectorOpenOntologies 内置 open-ontologies 连接器实例名（oo 预设迁移，REQ-214 批次一）。
const BuiltinConnectorOpenOntologies = "open-ontologies"

func scanConnector(row interface{ Scan(...any) error }) (*Connector, error) {
	var c Connector
	var configJSON, toolsJSON string
	var creds []byte
	var isBuiltin int
	err := row.Scan(&c.ID, &c.Kind, &c.Name, &c.Description, &configJSON, &creds, &c.Status, &c.StatusDetail, &isBuiltin, &c.CreatedAt, &c.UpdatedAt, &toolsJSON, &c.TestedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(configJSON), &c.Config)
	_ = json.Unmarshal([]byte(toolsJSON), &c.Tools)
	c.CredentialsEncrypted = creds
	c.HasCredentials = len(creds) > 0
	c.IsBuiltin = isBuiltin == 1
	if c.Config == nil {
		c.Config = map[string]any{}
	}
	if c.Tools == nil {
		c.Tools = []string{}
	}
	return &c, nil
}

const connectorCols = `id,kind,name,description,config_json,credentials_encrypted,status,status_detail,is_builtin,created_at,updated_at,tools_json,tested_at`

// ListConnectors 全量连接器（按创建时间升序）。
func (s *Store) ListConnectors() ([]*Connector, error) {
	rows, err := s.DB.Query(`SELECT ` + connectorCols + ` FROM connector ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Connector
	for rows.Next() {
		c, err := scanConnector(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetConnector 按 ID 查询。
func (s *Store) GetConnector(id string) (*Connector, error) {
	row := s.DB.QueryRow(`SELECT `+connectorCols+` FROM connector WHERE id = ?`, id)
	c, err := scanConnector(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// GetConnectorByName 按实例名查询（内置引导与存量迁移的幂等锚点）。
func (s *Store) GetConnectorByName(name string) (*Connector, error) {
	row := s.DB.QueryRow(`SELECT `+connectorCols+` FROM connector WHERE name = ?`, name)
	c, err := scanConnector(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// CreateConnector 新建；实例名唯一冲突返回 ErrConflict。
func (s *Store) CreateConnector(c *Connector) (*Connector, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	configJSON, _ := json.Marshal(c.Config)
	if c.Status == "" {
		c.Status = "unknown"
	}
	toolsJSON, _ := json.Marshal(c.Tools)
	_, err := s.DB.Exec(`INSERT INTO connector (id,kind,name,description,config_json,credentials_encrypted,status,status_detail,is_builtin,tools_json,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Kind, c.Name, c.Description, string(configJSON), c.CredentialsEncrypted, c.Status, c.StatusDetail, boolToInt(c.IsBuiltin), string(toolsJSON), now(), now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrConflict
		}
		return nil, err
	}
	return s.GetConnector(c.ID)
}

// UpdateConnector 更新（CredentialsEncrypted 以传入值为准：nil=保留原值，空切片=清除）。
func (s *Store) UpdateConnector(c *Connector) (*Connector, error) {
	if c.CredentialsEncrypted == nil {
		prev, err := s.GetConnector(c.ID)
		if err != nil {
			return nil, err
		}
		c.CredentialsEncrypted = prev.CredentialsEncrypted
	}
	configJSON, _ := json.Marshal(c.Config)
	toolsJSON, _ := json.Marshal(c.Tools)
	res, err := s.DB.Exec(`UPDATE connector SET kind=?,name=?,description=?,config_json=?,credentials_encrypted=?,status=?,status_detail=?,is_builtin=?,tools_json=?,tested_at=?,updated_at=? WHERE id=?`,
		c.Kind, c.Name, c.Description, string(configJSON), c.CredentialsEncrypted, c.Status, c.StatusDetail, boolToInt(c.IsBuiltin), string(toolsJSON), c.TestedAt, now(), c.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrConflict
		}
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetConnector(c.ID)
}

// DeleteConnector 删除（内置实例由 API 层拦截）。
func (s *Store) DeleteConnector(id string) error {
	res, err := s.DB.Exec(`DELETE FROM connector WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ConnectorRefs 连接器被哪些 agent 引用（删除保护与列表引用计数；REQ-214 批次一）。
func (s *Store) ConnectorRefs(connectorID string) ([]string, error) {
	agents, err := s.ListAgents()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, a := range agents {
		for _, id := range a.Connectors {
			if id == connectorID {
				out = append(out, a.Name)
				break
			}
		}
	}
	return out, nil
}

// MigrateAgentMCPToConnectors 启动幂等迁移（REQ-214 批次一）：存量 agent.mcp_servers 挂载
// 自动生成 kind=mcp 连接器实例（实例名沿用原 name——装配前缀槽位不变，工具名零破坏），
// agent.connectors 追加引用后 mcp_servers 置空。幂等：mcp_servers 为空即跳过。
func (s *Store) MigrateAgentMCPToConnectors() (int, error) {
	agents, err := s.ListAgents()
	if err != nil {
		return 0, err
	}
	migrated := 0
	for _, a := range agents {
		if len(a.MCPServers) == 0 {
			continue
		}
		changed := false
		for _, ms := range a.MCPServers {
			if ms.Name == "" || ms.URL == "" {
				continue
			}
			conn, err := s.GetConnectorByName(ms.Name)
			if errors.Is(err, ErrNotFound) {
				conn, err = s.CreateConnector(&Connector{
					Kind:        ConnectorKindMCP,
					Name:        ms.Name,
					Description: "存量 MCP 挂载自动迁移（REQ-214）",
					Config:      map[string]any{"url": ms.URL},
					Status:      "unknown",
				})
				if err != nil {
					return migrated, fmt.Errorf("migrate mcp server %q: %w", ms.Name, err)
				}
				changed = true
			} else if err != nil {
				return migrated, err
			}
			if !containsID(a.Connectors, conn.ID) {
				a.Connectors = append(a.Connectors, conn.ID)
				changed = true
			}
		}
		if changed {
			a.MCPServers = []MCPServer{}
			if _, err := s.UpdateAgent(a); err != nil {
				return migrated, fmt.Errorf("migrate agent %q: %w", a.Name, err)
			}
			migrated++
		}
	}
	return migrated, nil
}

// EnsureBuiltinOpenOntologiesConnector 内置 open-ontologies 连接器（oo 预设迁移，幂等）。
// 沿用原预设 URL（oo 原生 :8092/mcp；平台同源等价地址 /api/oo/mcp）。
func (s *Store) EnsureBuiltinOpenOntologiesConnector() error {
	_, err := s.GetConnectorByName(BuiltinConnectorOpenOntologies)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err = s.CreateConnector(&Connector{
		Kind:        ConnectorKindMCP,
		Name:        BuiltinConnectorOpenOntologies,
		Description: "内置：open-ontologies 双轨 MCP（119 onto_* 工具，Streamable HTTP；v2.0.1）——平台同源等价地址 /api/oo/mcp",
		Config:      map[string]any{"url": "http://127.0.0.1:8092/mcp"},
		Status:      "unknown",
		IsBuiltin:   true,
	})
	return err
}

func containsID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
